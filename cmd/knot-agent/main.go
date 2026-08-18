package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	"github.com/knot-infra/knot/internal/agent/buildrunner"
	"github.com/knot-infra/knot/internal/agent/deployrunner"
	"github.com/knot-infra/knot/internal/agent/edgeproxy"
	"github.com/knot-infra/knot/internal/agent/inventory"
	"github.com/knot-infra/knot/internal/agent/jobrunner"
	"github.com/knot-infra/knot/internal/agent/keystore"
	"github.com/knot-infra/knot/internal/agent/offline"
	"github.com/knot-infra/knot/internal/agent/storfs"
	"github.com/knot-infra/knot/internal/agent/updater"
	"github.com/knot-infra/knot/internal/agent/xfer"
	"github.com/knot-infra/knot/pkg/protocol"
)

type agentState struct {
	DeviceID         string `json:"device_id"`
	DeviceToken      string `json:"device_token"`
	Name             string `json:"name"`
	PublicKey        string `json:"public_key"`
	PrivateKeySealed string `json:"private_key_sealed"`
	LegacyPrivateKey string `json:"private_key,omitempty"`
	ControlURL       string `json:"control_url"`
}

func main() {
	controlURL := flag.String("control-url", envOr("KNOT_CONTROL_URL", "http://127.0.0.1:8787"), "Control Plane base URL")
	regToken := flag.String("registration-token", os.Getenv("KNOT_REGISTRATION_TOKEN"), "one-time registration token")
	name := flag.String("name", os.Getenv("KNOT_DEVICE_NAME"), "device display name")
	dataDir := flag.String("data-dir", defaultDataDir(), "agent state directory")
	shareDir := flag.String("share-dir", xfer.DefaultShareDir(), "inbox/outbox root (allowlisted)")
	storageDir := flag.String("storage-dir", storfs.DefaultDir(), "knot-storage root (allowlisted)")
	flag.Parse()
	*controlURL = strings.TrimSpace(*controlURL)
	*regToken = strings.TrimSpace(*regToken)
	*name = strings.TrimSpace(*name)

	if err := os.MkdirAll(*dataDir, 0o700); err != nil {
		log.Fatal(err)
	}
	statePath := filepath.Join(*dataDir, "state.json")

	state, err := loadState(statePath)
	if err != nil && !os.IsNotExist(err) {
		log.Fatal(err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if state == nil || state.DeviceToken == "" {
		if *regToken == "" {
			log.Fatal("registration required: pass -registration-token or KNOT_REGISTRATION_TOKEN")
		}
		state, err = register(*controlURL, *regToken, *name, *dataDir, statePath)
		if err != nil {
			log.Fatalf("register: %v", err)
		}
		log.Printf("registered as %s (%s)", state.Name, state.DeviceID)
	} else {
		if err := migrateLegacyKey(state, *dataDir, statePath); err != nil {
			log.Fatalf("keystore: %v", err)
		}
		state.ControlURL = *controlURL
		_ = saveState(statePath, state)
		log.Printf("loaded device %s (%s)", state.Name, state.DeviceID)
	}

	priv, err := loadPrivateKey(state, *dataDir)
	if err != nil {
		log.Fatalf("private key: %v", err)
	}

	log.Printf("share dir: %s (outbox for send, inbox for receive)", *shareDir)
	log.Printf("storage dir: %s", *storageDir)

	q, err := offline.Open(offline.Config{
		DBPath:   offline.DefaultDBPath(*dataDir),
		MaxBytes: offline.MaxBytesFromEnv(),
	})
	if err != nil {
		log.Fatalf("offline queue: %v", err)
	}
	defer q.Close()
	scanner := offline.NewScanner(*storageDir, q)
	var onlineMu sync.Mutex
	online := false
	setOnline := func(v bool) {
		onlineMu.Lock()
		online = v
		onlineMu.Unlock()
	}
	isOnline := func() bool {
		onlineMu.Lock()
		defer onlineMu.Unlock()
		return online
	}
	// Capture local FS edits while disconnected (and right after crash before reconnect).
	go scanner.Loop(ctx, 2*time.Second, func() bool { return !isOnline() })

	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		err := runSession(ctx, state, priv, *dataDir, *shareDir, *storageDir, q, scanner, setOnline)
		if ctx.Err() != nil {
			return
		}
		log.Printf("disconnected: %v; reconnecting in %s", err, backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

func register(controlURL, regToken, name, dataDir, statePath string) (*agentState, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	sealed, err := keystore.Seal(dataDir, priv)
	if err != nil {
		return nil, err
	}
	host, _ := os.Hostname()
	reqBody := protocol.RegisterRequest{
		RegistrationToken: regToken,
		PublicKey:         base64.RawURLEncoding.EncodeToString(pub),
		Name:              name,
		Hostname:          host,
		OS:                runtime.GOOS,
		Arch:              runtime.GOARCH,
	}
	body, _ := json.Marshal(reqBody)
	req, err := http.NewRequest(http.MethodPost, controlURL+"/v1/agent/register", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	hc, err := outboundHTTP()
	if err != nil {
		return nil, err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		var er map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&er)
		return nil, fmt.Errorf("register status %d: %v", resp.StatusCode, er)
	}
	var out protocol.RegisterResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	st := &agentState{
		DeviceID:         out.DeviceID,
		DeviceToken:      out.DeviceToken,
		Name:             out.Name,
		PublicKey:        base64.RawURLEncoding.EncodeToString(pub),
		PrivateKeySealed: sealed,
		ControlURL:       controlURL,
	}
	if err := saveState(statePath, st); err != nil {
		return nil, err
	}
	return st, nil
}

func runSession(ctx context.Context, st *agentState, priv ed25519.PrivateKey, dataDir, shareDir, storageDir string, q *offline.Queue, scanner *offline.Scanner, setOnline func(bool)) error {
	u, err := url.Parse(st.ControlURL)
	if err != nil {
		return err
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	default:
		u.Scheme = "ws"
	}
	u.Path = "/v1/agent/connect"
	qv := u.Query()
	qv.Set("token", st.DeviceToken)
	u.RawQuery = qv.Encode()

	hdr := http.Header{}
	hdr.Set("Authorization", "Bearer "+st.DeviceToken)
	tlsCfg, err := outboundTLS()
	if err != nil {
		return err
	}
	dialer := websocket.Dialer{HandshakeTimeout: 15 * time.Second, TLSClientConfig: tlsCfg}
	conn, _, err := dialer.DialContext(ctx, u.String(), hdr)
	if err != nil {
		return err
	}
	defer conn.Close()

	_, data, err := conn.ReadMessage()
	if err != nil {
		return err
	}
	var challenge protocol.ChallengeMessage
	if err := json.Unmarshal(data, &challenge); err != nil || challenge.Type != protocol.TypeChallenge {
		return fmt.Errorf("expected challenge, got %s", string(data))
	}
	sig := ed25519.Sign(priv, []byte(challenge.Nonce))
	if err := conn.WriteJSON(protocol.ChallengeResponse{
		Type:      protocol.TypeChallengeR,
		Nonce:     challenge.Nonce,
		Signature: base64.RawURLEncoding.EncodeToString(sig),
	}); err != nil {
		return err
	}

	_, data, err = conn.ReadMessage()
	if err != nil {
		return err
	}
	var session protocol.SessionMessage
	if err := json.Unmarshal(data, &session); err != nil || session.Type != protocol.TypeSession {
		var sm protocol.ServerMessage
		_ = json.Unmarshal(data, &sm)
		return fmt.Errorf("challenge failed: %s %s", sm.Code, sm.Message)
	}

	writeMu := &sync.Mutex{}
	pub, _ := base64.RawURLEncoding.DecodeString(st.PublicKey)
	xf, err := xfer.NewManager(st.DeviceID, shareDir, storageDir, pub, priv, conn, writeMu)
	if err != nil {
		return err
	}
	sf, err := storfs.New(storageDir, func(v any) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return conn.WriteJSON(v)
	})
	if err != nil {
		return err
	}
	ep := &edgeproxy.Manager{Send: func(v any) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return conn.WriteJSON(v)
	}}
	dr := deployrunner.NewManager(func(v any) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return conn.WriteJSON(v)
	}, deployrunner.NewComposite())
	jr := jobrunner.NewManager(func(v any) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return conn.WriteJSON(v)
	}, storageDir)
	br := buildrunner.NewManager(func(v any) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return conn.WriteJSON(v)
	}, filepath.Join(storageDir, "builds"))
	ur := updater.NewManager(dataDir, func(v any) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return conn.WriteJSON(v)
	})
	log.Printf("job policy: cpu=%.2f ram=%dMB gpu=%d disk=%dMB pids=%d concurrent=%d",
		jr.Policy.MaxCPU, jr.Policy.MaxMemoryMB, jr.Policy.MaxGPU, jr.Policy.MaxDiskMB, jr.Policy.MaxPids, jr.Policy.MaxConcurrent)

	log.Printf("connected to control plane (device session issued)")
	if setOnline != nil {
		setOnline(true)
		defer setOnline(false)
	}
	flushCh := make(chan protocol.OfflineFlushResult, 1)
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	sendHeartbeat := func() error {
		host, _ := os.Hostname()
		inv := inventory.Collect()
		ramMB := inv.Memory.TotalBytes / (1024 * 1024)
		msg := protocol.Heartbeat{
			Type: protocol.TypeHeartbeat,
			Telemetry: protocol.Telemetry{
				Hostname: host,
				OS:       runtime.GOOS,
				Arch:     runtime.GOARCH,
				CPUs:     inv.CPU.Cores,
				RAMMB:    ramMB,
				Version:  protocol.AgentVersion,
				Compute:  &inv,
			},
		}
		writeMu.Lock()
		defer writeMu.Unlock()
		return conn.WriteJSON(msg)
	}

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, welcomeRaw, welcomeErr := conn.ReadMessage()
	_ = conn.SetReadDeadline(time.Time{})
	if welcomeErr == nil {
		var welcome protocol.ServerMessage
		if json.Unmarshal(welcomeRaw, &welcome) == nil && welcome.Type == protocol.TypeWelcome {
			if protocol.VersionOlder(protocol.AgentVersion, welcome.MinAgentVersion) {
				log.Printf("agent version %s is older than control plane minimum %s — replace the knot-agent binary", protocol.AgentVersion, welcome.MinAgentVersion)
			}
		}
	}
	if err := sendHeartbeat(); err != nil {
		return err
	}

	go drainOfflineQueue(ctx, q, scanner, func(v any) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return conn.WriteJSON(v)
	}, flushCh)

	errCh := make(chan error, 1)
	go func() {
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				errCh <- err
				return
			}
			var env struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(msg, &env) != nil {
				continue
			}
			switch env.Type {
			case protocol.TypeWelcome:
				continue
			case protocol.TypeTransferOffer, protocol.TypeTransferStart, protocol.TypeTransferChunk,
				protocol.TypeTransferAck, protocol.TypeTransferComplete, protocol.TypeTransferAbort,
				protocol.TypePathNegotiate, protocol.TypePathCandidate, protocol.TypePathSelected:
				xf.Handle(msg)
			case protocol.TypeStorageOp:
				sf.Handle(msg)
			case protocol.TypeEdgeHTTPBegin, protocol.TypeEdgeHTTPBody, protocol.TypeEdgeHTTPAck, protocol.TypeEdgeHTTPFail,
				protocol.TypeEdgeProbe:
				ep.Handle(msg)
			case protocol.TypeEdgeStreamOpen, protocol.TypeEdgeStreamData, protocol.TypeEdgeStreamAck,
				protocol.TypeEdgeStreamClose, protocol.TypeEdgeStreamFail:
				ep.Handle(msg)
			case protocol.TypeDeployApply:
				dr.Handle(msg)
			case protocol.TypeJobRun, protocol.TypeJobCancel:
				jr.Handle(msg)
			case protocol.TypeBuildRun, protocol.TypeBuildCancel:
				br.Handle(msg)
			case protocol.TypeUpdateCheck, protocol.TypeUpdateApply:
				ur.Handle(msg)
			case protocol.TypeOfflineFlushResult:
				var fr protocol.OfflineFlushResult
				if json.Unmarshal(msg, &fr) == nil {
					select {
					case flushCh <- fr:
					default:
					}
				}
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-errCh:
			return err
		case <-ticker.C:
			if err := sendHeartbeat(); err != nil {
				return err
			}
		}
	}
}

func drainOfflineQueue(ctx context.Context, q *offline.Queue, scanner *offline.Scanner, send func(any) error, flushCh <-chan protocol.OfflineFlushResult) {
	if q == nil || scanner == nil {
		return
	}
	// Catch edits made while disconnected / during reconnect race.
	if _, err := scanner.ScanOnce(ctx); err != nil {
		log.Printf("offline scan: %v", err)
	}
	base, _ := scanner.LoadBaseline(ctx)
	if len(base) == 0 {
		if err := scanner.SeedBaseline(ctx); err != nil {
			log.Printf("offline seed baseline: %v", err)
		}
	}
	backoff := time.Duration(0)
	for {
		if ctx.Err() != nil {
			return
		}
		pending, err := q.ListPending(ctx)
		if err != nil {
			log.Printf("offline list: %v", err)
			return
		}
		if len(pending) == 0 {
			_ = q.CompactDone(ctx, 24*time.Hour)
			return
		}
		ids := make([]string, len(pending))
		paths := make([]string, 0, len(pending))
		for i, e := range pending {
			ids[i] = e.ID
			paths = append(paths, e.Path)
		}
		if err := q.MarkSyncing(ctx, ids); err != nil {
			log.Printf("offline mark syncing: %v", err)
			return
		}
		msg := protocol.OfflinePending{Type: protocol.TypeOfflinePending, Pending: len(pending), Paths: paths}
		if err := send(msg); err != nil {
			_ = q.MarkPendingRetry(ctx, ids, offline.NextBackoff(backoff))
			backoff = offline.NextBackoff(backoff)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			continue
		}
		select {
		case <-ctx.Done():
			return
		case fr := <-flushCh:
			if len(fr.JobIDs) == 0 {
				_ = q.MarkPendingRetry(ctx, ids, offline.NextBackoff(backoff))
				backoff = offline.NextBackoff(backoff)
				log.Printf("offline flush: no jobs (%s); retry in %s", fr.Error, backoff)
				select {
				case <-ctx.Done():
					return
				case <-time.After(backoff):
				}
				continue
			}
			conflict := map[string]struct{}{}
			for _, p := range fr.ConflictPaths {
				conflict[p] = struct{}{}
			}
			if err := q.FinishFlush(ctx, conflict); err != nil {
				log.Printf("offline finish: %v", err)
			}
			if fr.Error != "" {
				log.Printf("offline flush partial: %s", fr.Error)
			}
			log.Printf("offline flush ok jobs=%d conflicts=%d", len(fr.JobIDs), len(fr.ConflictPaths))
			_ = scanner.SeedBaseline(ctx)
			_ = q.CompactDone(ctx, 0)
			backoff = 0
			return
		case <-time.After(2 * time.Minute):
			_ = q.MarkPendingRetry(ctx, ids, offline.NextBackoff(backoff))
			backoff = offline.NextBackoff(backoff)
			log.Printf("offline flush timeout; retry in %s", backoff)
		}
	}
}

func loadPrivateKey(st *agentState, dataDir string) (ed25519.PrivateKey, error) {
	if st.PrivateKeySealed != "" {
		raw, err := keystore.Open(dataDir, st.PrivateKeySealed)
		if err != nil {
			return nil, err
		}
		if len(raw) != ed25519.PrivateKeySize {
			return nil, fmt.Errorf("invalid private key size")
		}
		return ed25519.PrivateKey(raw), nil
	}
	if st.LegacyPrivateKey != "" {
		raw, err := base64.RawURLEncoding.DecodeString(st.LegacyPrivateKey)
		if err != nil {
			return nil, err
		}
		return ed25519.PrivateKey(raw), nil
	}
	return nil, fmt.Errorf("no private key in state")
}

func migrateLegacyKey(st *agentState, dataDir, statePath string) error {
	if st.PrivateKeySealed != "" || st.LegacyPrivateKey == "" {
		return nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(st.LegacyPrivateKey)
	if err != nil {
		return err
	}
	sealed, err := keystore.Seal(dataDir, raw)
	if err != nil {
		return err
	}
	st.PrivateKeySealed = sealed
	st.LegacyPrivateKey = ""
	return saveState(statePath, st)
}

func loadState(path string) (*agentState, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var st agentState
	if err := json.Unmarshal(b, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

func saveState(path string, st *agentState) error {
	cp := *st
	cp.LegacyPrivateKey = ""
	b, err := json.MarshalIndent(&cp, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

func defaultDataDir() string {
	if v := os.Getenv("KNOT_AGENT_DATA"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "./knot-agent-data"
	}
	return filepath.Join(home, ".knot", "agent")
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
