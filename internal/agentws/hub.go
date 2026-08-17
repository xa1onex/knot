package agentws

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/knot-infra/knot/internal/audit"
	"github.com/knot-infra/knot/internal/auth"
	"github.com/knot-infra/knot/internal/devices"
	"github.com/knot-infra/knot/internal/oplogs"
	"github.com/knot-infra/knot/internal/store"
	"github.com/knot-infra/knot/internal/transfers"
	"github.com/knot-infra/knot/pkg/protocol"
)

var errDeviceOffline = errors.New("device offline")

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type Hub struct {
	Auth      *auth.Service
	Devices   *devices.Service
	Store     *store.Store
	Audit     *audit.Logger
	Transfers *transfers.Service
	Storage   StorageHandler
	Offline   OfflineFlusher
	Indexer   FileIndexer
	Edge      EdgeHandler
	Deploy    DeployHandler
	Jobs      JobsHandler
	Builds    BuildsHandler
	Logs      *oplogs.Service
	mu        sync.Mutex
	conns     map[string]*websocket.Conn
	writeMu   map[string]*sync.Mutex
}

// StorageHandler receives storage_op_result frames from agents.
type StorageHandler interface {
	HandleAgentMessage(ctx context.Context, fromDeviceID string, envelopeType string, raw []byte) error
}

// OfflineFlusher runs sync jobs for a device after reconnect (Stage 6.2.2).
type OfflineFlusher interface {
	FlushDevice(ctx context.Context, userID, deviceID string) (jobIDs, conflictPaths []string, errs []string, err error)
}

func NewHub(a *auth.Service, d *devices.Service, st *store.Store, al *audit.Logger) *Hub {
	return &Hub{
		Auth:    a,
		Devices: d,
		Store:   st,
		Audit:   al,
		conns:   make(map[string]*websocket.Conn),
		writeMu: make(map[string]*sync.Mutex),
	}
}

func (h *Hub) SetTransfers(t *transfers.Service) {
	h.Transfers = t
}

func (h *Hub) SetStorage(s StorageHandler) {
	h.Storage = s
}

func (h *Hub) SetOffline(f OfflineFlusher) {
	h.Offline = f
}

// FileIndexer refreshes the global metadata index when a node comes online (Stage 6.5).
type FileIndexer interface {
	ScheduleReindex(userID, deviceID string)
}

func (h *Hub) SetIndexer(i FileIndexer) {
	h.Indexer = i
}

// EdgeHandler receives edge stream frames from agents (Stage 7.3).
type EdgeHandler interface {
	HandleAgentMessage(ctx context.Context, fromDeviceID string, envelopeType string, raw []byte) error
	OnDeviceDisconnect(deviceID string)
}

func (h *Hub) SetEdge(e EdgeHandler) {
	h.Edge = e
}

// DeployHandler receives deploy frames from agents (Stage 7.4).
type DeployHandler interface {
	HandleAgentMessage(ctx context.Context, fromDeviceID string, envelopeType string, raw []byte) error
	OnDeviceDisconnect(deviceID string)
}

func (h *Hub) SetDeploy(d DeployHandler) {
	h.Deploy = d
}

// JobsHandler receives compute job frames from agents (Stage 8.2).
type JobsHandler interface {
	HandleAgentMessage(ctx context.Context, fromDeviceID string, envelopeType string, raw []byte) error
	OnDeviceDisconnect(deviceID string)
	OnComputeUpdated(userID string)
}

func (h *Hub) SetJobs(j JobsHandler) {
	h.Jobs = j
}

// BuildsHandler receives Git→image build frames from agents (Stage 9.2).
type BuildsHandler interface {
	HandleAgentMessage(ctx context.Context, fromDeviceID string, envelopeType string, raw []byte) error
	OnDeviceDisconnect(deviceID string)
}

func (h *Hub) SetBuilds(b BuildsHandler) {
	h.Builds = b
}

func (h *Hub) IsOnline(deviceID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	_, ok := h.conns[deviceID]
	return ok
}

func (h *Hub) SendJSON(deviceID string, v any) error {
	h.mu.Lock()
	conn := h.conns[deviceID]
	wmu := h.writeMu[deviceID]
	h.mu.Unlock()
	if conn == nil || wmu == nil {
		return errDeviceOffline
	}
	wmu.Lock()
	defer wmu.Unlock()
	_ = conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
	return conn.WriteJSON(v)
}

func (h *Hub) HandleConnect(w http.ResponseWriter, r *http.Request) {
	raw := bearer(r)
	id, err := h.Auth.ResolveBearer(r.Context(), raw)
	if err != nil || id.Kind != auth.KindDevice {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	dev, err := h.Store.GetDeviceByID(r.Context(), id.DeviceID)
	if err != nil || dev.RevokedAt != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws upgrade: %v", err)
		return
	}
	defer conn.Close()

	nonceBytes := make([]byte, 32)
	if _, err := rand.Read(nonceBytes); err != nil {
		return
	}
	nonce := base64.RawURLEncoding.EncodeToString(nonceBytes)
	if err := conn.WriteJSON(protocol.ChallengeMessage{Type: protocol.TypeChallenge, Nonce: nonce}); err != nil {
		return
	}

	_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		h.Audit.Log(r.Context(), id.UserID, id.Actor, "agent.challenge", id.DeviceID, "read_failed", "FAILURE")
		return
	}
	var resp protocol.ChallengeResponse
	if err := json.Unmarshal(data, &resp); err != nil || resp.Type != protocol.TypeChallengeR {
		h.Audit.Log(r.Context(), id.UserID, id.Actor, "agent.challenge", id.DeviceID, "bad_message", "FAILURE")
		_ = conn.WriteJSON(protocol.ServerMessage{Type: protocol.TypeError, Code: "validation_error", Message: "expected challenge_response"})
		return
	}
	if resp.Nonce != nonce {
		h.Audit.Log(r.Context(), id.UserID, id.Actor, "agent.challenge", id.DeviceID, "nonce_mismatch", "FAILURE")
		_ = conn.WriteJSON(protocol.ServerMessage{Type: protocol.TypeError, Code: "unauthorized", Message: "nonce mismatch"})
		return
	}

	pub, err := base64.RawURLEncoding.DecodeString(dev.PublicKey)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		h.Audit.Log(r.Context(), id.UserID, id.Actor, "agent.challenge", id.DeviceID, "bad_pubkey", "FAILURE")
		return
	}
	sig, err := base64.RawURLEncoding.DecodeString(resp.Signature)
	if err != nil || !ed25519.Verify(ed25519.PublicKey(pub), []byte(nonce), sig) {
		h.Audit.Log(r.Context(), id.UserID, id.Actor, "agent.challenge", id.DeviceID, "bad_signature", "FAILURE")
		_ = conn.WriteJSON(protocol.ServerMessage{Type: protocol.TypeError, Code: "unauthorized", Message: "invalid signature"})
		return
	}

	sessionTok, err := h.Auth.IssueDeviceSession(r.Context(), dev)
	if err != nil {
		h.Audit.Log(r.Context(), id.UserID, id.Actor, "agent.challenge", id.DeviceID, "session_issue", "FAILURE")
		return
	}
	ttl := h.Auth.DeviceSessionTTL
	if ttl == 0 {
		ttl = 24 * time.Hour
	}
	if err := conn.WriteJSON(protocol.SessionMessage{
		Type:          protocol.TypeSession,
		DeviceSession: sessionTok,
		ExpiresIn:     int64(ttl.Seconds()),
	}); err != nil {
		return
	}

	_ = conn.SetReadDeadline(time.Time{})
	h.mu.Lock()
	if old, ok := h.conns[id.DeviceID]; ok {
		_ = old.Close()
	}
	h.conns[id.DeviceID] = conn
	h.writeMu[id.DeviceID] = &sync.Mutex{}
	h.mu.Unlock()

	_ = h.Devices.Touch(r.Context(), id.DeviceID, true, protocol.Telemetry{})
	h.Audit.Log(r.Context(), id.UserID, id.Actor, "agent.connect", id.DeviceID, "", "SUCCESS")
	h.Logs.Emit(r.Context(), oplogs.Event{
		UserID: id.UserID, Source: oplogs.SourceAgent, Level: "info",
		DeviceID: id.DeviceID, Message: "agent connected",
	})
	if h.Jobs != nil {
		h.Jobs.OnComputeUpdated(id.UserID)
	}

	defer func() {
		h.mu.Lock()
		if h.conns[id.DeviceID] == conn {
			delete(h.conns, id.DeviceID)
			delete(h.writeMu, id.DeviceID)
		}
		h.mu.Unlock()
		if h.Edge != nil {
			h.Edge.OnDeviceDisconnect(id.DeviceID)
		}
		if h.Deploy != nil {
			h.Deploy.OnDeviceDisconnect(id.DeviceID)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = h.Devices.Touch(ctx, id.DeviceID, false, protocol.Telemetry{})
		if h.Jobs != nil {
			h.Jobs.OnDeviceDisconnect(id.DeviceID)
		}
		if h.Builds != nil {
			h.Builds.OnDeviceDisconnect(id.DeviceID)
		}
		h.Audit.Log(ctx, id.UserID, id.Actor, "agent.disconnect", id.DeviceID, "", "SUCCESS")
		h.Logs.Emit(ctx, oplogs.Event{
			UserID: id.UserID, Source: oplogs.SourceAgent, Level: "info",
			DeviceID: id.DeviceID, Message: "agent disconnected",
		})
	}()

	_ = conn.WriteJSON(protocol.ServerMessage{
		Type:            protocol.TypeWelcome,
		Message:         "connected",
		AgentVersion:    protocol.AgentVersion,
		MinAgentVersion: protocol.AgentVersion,
	})
	if h.Indexer != nil {
		h.Indexer.ScheduleReindex(id.UserID, id.DeviceID)
	}

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var envelope struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(data, &envelope); err != nil {
			continue
		}
		switch envelope.Type {
		case protocol.TypeHeartbeat, protocol.TypeHello:
			var hb protocol.Heartbeat
			if err := json.Unmarshal(data, &hb); err != nil {
				continue
			}
			_ = h.Devices.Touch(r.Context(), id.DeviceID, true, hb.Telemetry)
			if h.Jobs != nil && hb.Telemetry.Compute != nil {
				h.Jobs.OnComputeUpdated(id.UserID)
			}
		case protocol.TypeTransferAccept, protocol.TypeTransferReject,
			protocol.TypeTransferChunk, protocol.TypeTransferAck,
			protocol.TypeTransferComplete, protocol.TypeTransferAbort,
			protocol.TypePathCandidate, protocol.TypePathSelected:
			if h.Transfers == nil {
				continue
			}
			if err := h.Transfers.HandleAgentMessage(r.Context(), id.DeviceID, envelope.Type, data); err != nil {
				log.Printf("transfer frame from %s: %v", id.DeviceID, err)
			}
		case protocol.TypeStorageOpResult:
			if h.Storage == nil {
				continue
			}
			if err := h.Storage.HandleAgentMessage(r.Context(), id.DeviceID, envelope.Type, data); err != nil {
				log.Printf("storage frame from %s: %v", id.DeviceID, err)
			}
		case protocol.TypeEdgeHTTPRespHead, protocol.TypeEdgeHTTPRespBody,
			protocol.TypeEdgeHTTPAck, protocol.TypeEdgeHTTPFail,
			protocol.TypeEdgeProbeResult,
			protocol.TypeEdgeStreamReady, protocol.TypeEdgeStreamData,
			protocol.TypeEdgeStreamAck, protocol.TypeEdgeStreamFail, protocol.TypeEdgeStreamClose:
			if h.Edge == nil {
				continue
			}
			if err := h.Edge.HandleAgentMessage(r.Context(), id.DeviceID, envelope.Type, data); err != nil {
				log.Printf("edge frame from %s: %v", id.DeviceID, err)
			}
		case protocol.TypeDeployApplyResult, protocol.TypeDeployLogLine:
			if h.Deploy == nil {
				continue
			}
			if err := h.Deploy.HandleAgentMessage(r.Context(), id.DeviceID, envelope.Type, data); err != nil {
				log.Printf("deploy frame from %s: %v", id.DeviceID, err)
			}
		case protocol.TypeJobResult, protocol.TypeJobLogLine:
			if h.Jobs == nil {
				continue
			}
			if err := h.Jobs.HandleAgentMessage(r.Context(), id.DeviceID, envelope.Type, data); err != nil {
				log.Printf("job frame from %s: %v", id.DeviceID, err)
			}
		case protocol.TypeBuildResult, protocol.TypeBuildLogLine, protocol.TypeBuildProgress:
			if h.Builds == nil {
				continue
			}
			if err := h.Builds.HandleAgentMessage(r.Context(), id.DeviceID, envelope.Type, data); err != nil {
				log.Printf("build frame from %s: %v", id.DeviceID, err)
			}
		case protocol.TypeOfflinePending:
			h.handleOfflinePending(r.Context(), id.UserID, id.DeviceID, data)
		}
	}
}

func (h *Hub) handleOfflinePending(ctx context.Context, userID, deviceID string, data []byte) {
	var msg protocol.OfflinePending
	_ = json.Unmarshal(data, &msg)
	res := protocol.OfflineFlushResult{Type: protocol.TypeOfflineFlushResult}
	if h.Offline == nil {
		res.OK = false
		res.Error = "offline flush not configured"
		_ = h.SendJSON(deviceID, res)
		return
	}
	flushCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	jobIDs, conflicts, errs, err := h.Offline.FlushDevice(flushCtx, userID, deviceID)
	res.JobIDs = jobIDs
	res.ConflictPaths = conflicts
	if err != nil {
		res.OK = false
		res.Error = err.Error()
	} else if len(errs) > 0 {
		res.OK = false
		res.Error = strings.Join(errs, "; ")
	} else {
		res.OK = true
	}
	if sendErr := h.SendJSON(deviceID, res); sendErr != nil {
		log.Printf("offline flush result to %s: %v", deviceID, sendErr)
	}
}

func (h *Hub) StartPresenceSweeper(ctx context.Context, timeout time.Duration) {
	t := time.NewTicker(15 * time.Second)
	go func() {
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				_ = h.Devices.MarkStaleOffline(ctx, timeout)
			}
		}
	}()
}

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(strings.ToLower(h), "bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return r.URL.Query().Get("token")
}
