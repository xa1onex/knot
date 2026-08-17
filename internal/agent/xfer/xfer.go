package xfer

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/knot-infra/knot/internal/network/direct"
	"github.com/knot-infra/knot/internal/network/transport"
	"github.com/knot-infra/knot/pkg/protocol"
)

type Manager struct {
	DeviceID   string
	PubKey     []byte
	PrivKey    ed25519.PrivateKey
	InboxDir   string
	OutboxDir  string
	StorageDir string
	writeMu    *sync.Mutex
	conn       *websocket.Conn
	Dialer     *direct.Dialer

	mu            sync.Mutex
	recv          map[string]*recvState
	pendingSource map[string]protocol.TransferOffer
	pendingDest   map[string]protocol.TransferOffer
	candCh        map[string]chan transport.Candidate
	directSess    map[string]transport.Session
	ackWait       map[string]chan int // transferID -> next expected ack index
	pathMu        sync.Map            // final path -> *sync.Mutex
}

type recvState struct {
	file      *os.File
	hasher    hash.Hash
	offer     protocol.TransferOffer
	finalPath string
	partPath  string
	received  int64
}

func NewManager(deviceID, shareDir, storageDir string, pub []byte, priv ed25519.PrivateKey, conn *websocket.Conn, writeMu *sync.Mutex) (*Manager, error) {
	inbox := filepath.Join(shareDir, "inbox")
	outbox := filepath.Join(shareDir, "outbox")
	if err := os.MkdirAll(inbox, 0o700); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(outbox, 0o700); err != nil {
		return nil, err
	}
	if storageDir == "" {
		storageDir = filepath.Join(shareDir, "..", "knot-storage")
	}
	if err := os.MkdirAll(storageDir, 0o700); err != nil {
		return nil, err
	}
	return &Manager{
		DeviceID:      deviceID,
		PubKey:        pub,
		PrivKey:       priv,
		InboxDir:      inbox,
		OutboxDir:     outbox,
		StorageDir:    storageDir,
		conn:          conn,
		writeMu:       writeMu,
		Dialer:        &direct.Dialer{Timeout: 3 * time.Second},
		recv:          make(map[string]*recvState),
		pendingSource: make(map[string]protocol.TransferOffer),
		pendingDest:   make(map[string]protocol.TransferOffer),
		candCh:        make(map[string]chan transport.Candidate),
		directSess:    make(map[string]transport.Session),
		ackWait:       make(map[string]chan int),
	}, nil
}

func (m *Manager) Handle(data []byte) {
	var env struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return
	}
	switch env.Type {
	case protocol.TypeTransferOffer:
		var offer protocol.TransferOffer
		if err := json.Unmarshal(data, &offer); err != nil {
			return
		}
		m.onOffer(offer)
	case protocol.TypePathNegotiate:
		var neg protocol.PathNegotiate
		if err := json.Unmarshal(data, &neg); err != nil {
			return
		}
		go m.onPathNegotiate(neg)
	case protocol.TypePathCandidate:
		var c protocol.PathCandidateMsg
		if err := json.Unmarshal(data, &c); err != nil {
			return
		}
		m.mu.Lock()
		ch := m.candCh[c.TransferID]
		m.mu.Unlock()
		if ch != nil {
			select {
			case ch <- transport.Candidate{TransferID: c.TransferID, DeviceID: c.DeviceID, Addr: c.Addr, Kind: c.Kind}:
			default:
			}
		}
	case protocol.TypeTransferStart:
		var start protocol.TransferStart
		if err := json.Unmarshal(data, &start); err != nil {
			return
		}
		m.onStart(start)
	case protocol.TypeTransferChunk:
		var chunk protocol.TransferChunk
		if err := json.Unmarshal(data, &chunk); err != nil {
			return
		}
		m.onChunk(chunk)
	case protocol.TypeTransferAck:
		var ack protocol.TransferAck
		if err := json.Unmarshal(data, &ack); err != nil {
			return
		}
		m.mu.Lock()
		ch := m.ackWait[ack.TransferID]
		m.mu.Unlock()
		if ch != nil {
			select {
			case ch <- ack.Index:
			default:
			}
		}
	case protocol.TypeTransferComplete:
		log.Printf("transfer complete ack: %s", extractID(data))
	case protocol.TypeTransferAbort:
		var ab protocol.TransferAbort
		_ = json.Unmarshal(data, &ab)
		log.Printf("transfer %s aborted: %s", ab.TransferID, ab.Reason)
		m.cleanupRecv(ab.TransferID)
		m.closeDirect(ab.TransferID)
	}
}

func (m *Manager) onOffer(offer protocol.TransferOffer) {
	max := int64(protocol.MaxTransferBytes)
	if offer.DestStoragePath != "" || offer.SourceFromStorage {
		max = protocol.MaxStorageTransferBytes
	}
	if offer.Size <= 0 || offer.Size > max {
		m.send(protocol.TransferReject{
			Type: protocol.TypeTransferReject, TransferID: offer.TransferID,
			DeviceID: m.DeviceID, Reason: "size limit",
		})
		return
	}
	m.send(protocol.TransferAccept{
		Type: protocol.TypeTransferAccept, TransferID: offer.TransferID, DeviceID: m.DeviceID,
	})
	m.mu.Lock()
	if offer.Role == "source" {
		m.pendingSource[offer.TransferID] = offer
	} else {
		m.pendingDest[offer.TransferID] = offer
	}
	m.mu.Unlock()
}

func (m *Manager) onPathNegotiate(neg protocol.PathNegotiate) {
	peerPub, err := base64.RawURLEncoding.DecodeString(neg.PeerPublicKey)
	if err != nil || len(peerPub) != ed25519.PublicKeySize {
		m.reportPath(neg.TransferID, transport.PathRelay)
		return
	}
	ch := make(chan transport.Candidate, 32)
	m.mu.Lock()
	m.candCh[neg.TransferID] = ch
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.candCh, neg.TransferID)
		m.mu.Unlock()
		close(ch)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	sess, err := m.Dialer.TryDirect(ctx, transport.DialParams{
		TransferID:    neg.TransferID,
		LocalDeviceID: m.DeviceID,
		PeerDeviceID:  neg.PeerDeviceID,
		LocalPrivKey:  m.PrivKey,
		LocalPubKey:   m.PubKey,
		PeerPubKey:    peerPub,
		Role:          neg.Role,
		ForceRelay:    neg.ForceRelay,
		STUNURLs:      neg.STUNURLs,
		CandidateInbox: ch,
		SignalOutbound: func(msg any) error {
			c, ok := msg.(transport.Candidate)
			if !ok {
				return nil
			}
			return m.send(protocol.PathCandidateMsg{
				Type: protocol.TypePathCandidate, TransferID: c.TransferID,
				DeviceID: m.DeviceID, Addr: c.Addr, Kind: c.Kind,
			})
		},
	})
	if err != nil {
		log.Printf("direct negotiate error: %v", err)
	}
	if sess != nil {
		m.mu.Lock()
		m.directSess[neg.TransferID] = sess
		m.mu.Unlock()
		log.Printf("direct path ready for %s", neg.TransferID)
		if neg.Role == "source" {
			m.reportPath(neg.TransferID, transport.PathDirect)
		}
		return
	}
	if neg.Role == "source" {
		m.reportPath(neg.TransferID, transport.PathRelay)
	}
}

func (m *Manager) reportPath(transferID, path string) {
	_ = m.send(protocol.PathSelected{
		Type: protocol.TypePathSelected, TransferID: transferID,
		DeviceID: m.DeviceID, Path: path,
	})
}

func (m *Manager) onStart(start protocol.TransferStart) {
	path := start.Path
	if path == "" {
		path = transport.PathRelay
	}
	log.Printf("transfer %s start via %s", start.TransferID, path)

	m.mu.Lock()
	offerSrc, isSrc := m.pendingSource[start.TransferID]
	offerDst, isDst := m.pendingDest[start.TransferID]
	delete(m.pendingSource, start.TransferID)
	delete(m.pendingDest, start.TransferID)
	sess := m.directSess[start.TransferID]
	m.mu.Unlock()

	if path == transport.PathDirect {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			m.mu.Lock()
			sess = m.directSess[start.TransferID]
			m.mu.Unlock()
			if sess != nil {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		if sess != nil {
			if isSrc {
				go m.sendFileDirect(offerSrc, sess)
			} else if isDst {
				go m.recvFileDirect(offerDst, sess)
			}
			return
		}
		log.Printf("direct session missing for %s; cannot complete direct path", start.TransferID)
		m.send(protocol.TransferAbort{Type: protocol.TypeTransferAbort, TransferID: start.TransferID, DeviceID: m.DeviceID, Reason: "direct session missing"})
		return
	}
	// Relay path
	m.closeDirect(start.TransferID)
	if isSrc {
		go m.sendFileRelay(offerSrc)
		return
	}
	if isDst {
		m.prepareRecv(offerDst)
	}
}

func (m *Manager) sourceFile(offer protocol.TransferOffer) (string, error) {
	if offer.SourceFromStorage {
		return resolveUnder(m.StorageDir, offer.SourcePath)
	}
	src := filepath.Join(m.OutboxDir, filepath.Clean(filepath.FromSlash(offer.SourcePath)))
	rel, err := filepath.Rel(m.OutboxDir, src)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("path outside outbox")
	}
	return src, nil
}

func (m *Manager) destPaths(offer protocol.TransferOffer) (finalPath, partPath string, err error) {
	if offer.DestStoragePath != "" {
		full, err := resolveUnder(m.StorageDir, offer.DestStoragePath)
		if err != nil {
			return "", "", err
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			return "", "", err
		}
		// Resume continues the stable file_id part; fresh uploads use transfer-scoped parts (no mix).
		if offer.ResumeOffset > 0 && offer.FileID != "" {
			return full, protocol.PartPath(full, offer.FileID), nil
		}
		return full, protocol.PartPath(full, offer.TransferID), nil
	}
	final := filepath.Join(m.InboxDir, filepath.Base(offer.Filename))
	return final, final, nil
}

func resolveUnder(root, rel string) (string, error) {
	rel = filepath.Clean(filepath.FromSlash(rel))
	full := filepath.Join(root, rel)
	r, err := filepath.Rel(root, full)
	if err != nil || strings.HasPrefix(r, "..") {
		return "", fmt.Errorf("path outside storage root")
	}
	return full, nil
}

func (m *Manager) lockFinal(path string) func() {
	v, _ := m.pathMu.LoadOrStore(path, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

func (m *Manager) finalizeRecv(offer protocol.TransferOffer, finalPath, partPath string) error {
	unlock := m.lockFinal(finalPath)
	defer unlock()
	sum, size, err := hashFile(partPath)
	if err != nil {
		return err
	}
	if size != offer.Size {
		return fmt.Errorf("size mismatch after recv: got %d want %d", size, offer.Size)
	}
	if !strings.EqualFold(sum, offer.SHA256) {
		return fmt.Errorf("sha256 mismatch")
	}
	if partPath != finalPath {
		_ = os.Remove(finalPath)
		if err := os.Rename(partPath, finalPath); err != nil {
			return err
		}
	}
	return nil
}

func hashFile(path string) (sum string, size int64, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

func (m *Manager) sendFileDirect(offer protocol.TransferOffer, sess transport.Session) {
	defer m.closeDirect(offer.TransferID)
	src, err := m.sourceFile(offer)
	if err != nil {
		m.send(protocol.TransferAbort{Type: protocol.TypeTransferAbort, TransferID: offer.TransferID, DeviceID: m.DeviceID, Reason: err.Error()})
		return
	}
	f, err := os.Open(src)
	if err != nil {
		m.send(protocol.TransferAbort{Type: protocol.TypeTransferAbort, TransferID: offer.TransferID, DeviceID: m.DeviceID, Reason: err.Error()})
		return
	}
	defer f.Close()
	if offer.ResumeOffset > 0 {
		if _, err := f.Seek(offer.ResumeOffset, io.SeekStart); err != nil {
			m.send(protocol.TransferAbort{Type: protocol.TypeTransferAbort, TransferID: offer.TransferID, DeviceID: m.DeviceID, Reason: err.Error()})
			return
		}
	}
	remain := offer.Size - offer.ResumeOffset
	limited := io.LimitReader(f, remain)
	if err := direct.WriteFilePayload(sess, offer.TransferID, remain, offer.SHA256, limited); err != nil {
		m.send(protocol.TransferAbort{Type: protocol.TypeTransferAbort, TransferID: offer.TransferID, DeviceID: m.DeviceID, Reason: err.Error()})
		return
	}
	log.Printf("direct send complete %s offset=%d", offer.TransferID, offer.ResumeOffset)
}

func (m *Manager) recvFileDirect(offer protocol.TransferOffer, sess transport.Session) {
	defer m.closeDirect(offer.TransferID)
	_, size, _, body, err := direct.ReadFilePayload(sess)
	if err != nil {
		m.send(protocol.TransferAbort{Type: protocol.TypeTransferAbort, TransferID: offer.TransferID, DeviceID: m.DeviceID, Reason: err.Error()})
		return
	}
	finalPath, partPath, err := m.destPaths(offer)
	if err != nil {
		m.send(protocol.TransferAbort{Type: protocol.TypeTransferAbort, TransferID: offer.TransferID, DeviceID: m.DeviceID, Reason: err.Error()})
		return
	}
	flags := os.O_CREATE | os.O_WRONLY
	if offer.ResumeOffset > 0 && partPath != finalPath {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	f, err := os.OpenFile(partPath, flags, 0o600)
	if err != nil {
		m.send(protocol.TransferAbort{Type: protocol.TypeTransferAbort, TransferID: offer.TransferID, DeviceID: m.DeviceID, Reason: err.Error()})
		return
	}
	n, err := io.Copy(f, body)
	_ = f.Close()
	if err != nil || n != size {
		m.send(protocol.TransferAbort{Type: protocol.TypeTransferAbort, TransferID: offer.TransferID, DeviceID: m.DeviceID, Reason: "direct read failed"})
		return
	}
	if err := m.finalizeRecv(offer, finalPath, partPath); err != nil {
		m.send(protocol.TransferAbort{Type: protocol.TypeTransferAbort, TransferID: offer.TransferID, DeviceID: m.DeviceID, Reason: err.Error()})
		return
	}
	m.send(protocol.TransferComplete{
		Type: protocol.TypeTransferComplete, TransferID: offer.TransferID, DeviceID: m.DeviceID, SHA256: offer.SHA256,
	})
	log.Printf("direct recv complete %s -> %s", offer.TransferID, finalPath)
}

func (m *Manager) prepareRecv(offer protocol.TransferOffer) {
	finalPath, partPath, err := m.destPaths(offer)
	if err != nil {
		m.send(protocol.TransferAbort{Type: protocol.TypeTransferAbort, TransferID: offer.TransferID, DeviceID: m.DeviceID, Reason: err.Error()})
		return
	}
	if offer.ResumeOffset > 0 && offer.FileID != "" && partPath != finalPath {
		if err := adoptPartial(finalPath, offer.FileID, offer.ResumeOffset); err != nil {
			m.send(protocol.TransferAbort{Type: protocol.TypeTransferAbort, TransferID: offer.TransferID, DeviceID: m.DeviceID, Reason: err.Error()})
			return
		}
	}
	flags := os.O_CREATE | os.O_WRONLY
	if offer.ResumeOffset > 0 && partPath != finalPath {
		st, err := os.Stat(partPath)
		if err != nil || st.Size() != offer.ResumeOffset {
			m.send(protocol.TransferAbort{Type: protocol.TypeTransferAbort, TransferID: offer.TransferID, DeviceID: m.DeviceID, Reason: "partial size mismatch"})
			return
		}
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	f, err := os.OpenFile(partPath, flags, 0o600)
	if err != nil {
		m.send(protocol.TransferAbort{Type: protocol.TypeTransferAbort, TransferID: offer.TransferID, DeviceID: m.DeviceID, Reason: err.Error()})
		return
	}
	m.mu.Lock()
	m.recv[offer.TransferID] = &recvState{
		file: f, hasher: sha256.New(), offer: offer,
		finalPath: finalPath, partPath: partPath, received: offer.ResumeOffset,
	}
	m.mu.Unlock()
}

func adoptPartial(finalPath, fileID string, want int64) error {
	stable := protocol.PartPath(finalPath, fileID)
	if st, err := os.Stat(stable); err == nil {
		if st.Size() == want {
			return nil
		}
		return fmt.Errorf("stable partial size %d want %d", st.Size(), want)
	}
	dir := filepath.Dir(finalPath)
	base := filepath.Base(finalPath)
	ents, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	prefix := base + protocol.PartSuffixPrefix
	var best string
	var bestSize int64
	for _, e := range ents {
		n := e.Name()
		if !strings.HasPrefix(n, prefix) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.Size() > bestSize {
			bestSize = info.Size()
			best = filepath.Join(dir, n)
		}
	}
	if best == "" || bestSize != want {
		return fmt.Errorf("no partial matching offset %d", want)
	}
	return os.Rename(best, stable)
}

func (m *Manager) sendFileRelay(offer protocol.TransferOffer) {
	src, err := m.sourceFile(offer)
	if err != nil {
		m.send(protocol.TransferAbort{Type: protocol.TypeTransferAbort, TransferID: offer.TransferID, DeviceID: m.DeviceID, Reason: err.Error()})
		return
	}
	f, err := os.Open(src)
	if err != nil {
		m.send(protocol.TransferAbort{Type: protocol.TypeTransferAbort, TransferID: offer.TransferID, DeviceID: m.DeviceID, Reason: err.Error()})
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.Size() != offer.Size {
		m.send(protocol.TransferAbort{Type: protocol.TypeTransferAbort, TransferID: offer.TransferID, DeviceID: m.DeviceID, Reason: "size mismatch on disk"})
		return
	}
	if offer.ResumeOffset > 0 {
		if _, err := f.Seek(offer.ResumeOffset, io.SeekStart); err != nil {
			m.send(protocol.TransferAbort{Type: protocol.TypeTransferAbort, TransferID: offer.TransferID, DeviceID: m.DeviceID, Reason: err.Error()})
			return
		}
	}
	chunkSize := offer.ChunkBytes
	if chunkSize <= 0 {
		chunkSize = protocol.DefaultChunkBytes
	}

	ackCh := make(chan int, 2)
	m.mu.Lock()
	m.ackWait[offer.TransferID] = ackCh
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.ackWait, offer.TransferID)
		m.mu.Unlock()
	}()

	buf := make([]byte, chunkSize)
	idx := 0
	sent := offer.ResumeOffset
	for {
		n, err := f.Read(buf)
		if n > 0 {
			sent += int64(n)
			last := err == io.EOF || sent >= offer.Size
			if sendErr := m.send(protocol.TransferChunk{
				Type: protocol.TypeTransferChunk, TransferID: offer.TransferID,
				Index: idx, DataB64: base64.StdEncoding.EncodeToString(buf[:n]), Last: last,
			}); sendErr != nil {
				return
			}
			// Backpressure: wait for dest ACK before next chunk (window=1).
			select {
			case <-ackCh:
			case <-time.After(60 * time.Second):
				m.send(protocol.TransferAbort{Type: protocol.TypeTransferAbort, TransferID: offer.TransferID, DeviceID: m.DeviceID, Reason: "ack timeout"})
				return
			}
			idx++
			if last {
				log.Printf("relay sent all chunks for %s (from offset %d)", offer.TransferID, offer.ResumeOffset)
				return
			}
		}
		if err == io.EOF {
			return
		}
		if err != nil {
			m.send(protocol.TransferAbort{Type: protocol.TypeTransferAbort, TransferID: offer.TransferID, DeviceID: m.DeviceID, Reason: err.Error()})
			return
		}
	}
}

func (m *Manager) onChunk(chunk protocol.TransferChunk) {
	m.mu.Lock()
	st := m.recv[chunk.TransferID]
	m.mu.Unlock()
	if st == nil {
		return
	}
	raw, err := base64.StdEncoding.DecodeString(chunk.DataB64)
	if err != nil {
		m.abortRecv(chunk.TransferID, "bad chunk encoding")
		return
	}
	if _, err := st.file.Write(raw); err != nil {
		m.abortRecv(chunk.TransferID, err.Error())
		return
	}
	st.received += int64(len(raw))
	_, _ = st.hasher.Write(raw)
	m.send(protocol.TransferAck{
		Type: protocol.TypeTransferAck, TransferID: chunk.TransferID, Index: chunk.Index,
		BytesReceived: st.received,
	})
	if !chunk.Last {
		return
	}
	_ = st.file.Close()
	m.mu.Lock()
	delete(m.recv, chunk.TransferID)
	m.mu.Unlock()
	if err := m.finalizeRecv(st.offer, st.finalPath, st.partPath); err != nil {
		m.send(protocol.TransferAbort{Type: protocol.TypeTransferAbort, TransferID: chunk.TransferID, DeviceID: m.DeviceID, Reason: err.Error()})
		return
	}
	m.send(protocol.TransferComplete{Type: protocol.TypeTransferComplete, TransferID: chunk.TransferID, DeviceID: m.DeviceID, SHA256: st.offer.SHA256})
	log.Printf("relay recv complete %s", chunk.TransferID)
}

func (m *Manager) abortRecv(id, reason string) {
	m.cleanupRecv(id)
	m.send(protocol.TransferAbort{Type: protocol.TypeTransferAbort, TransferID: id, DeviceID: m.DeviceID, Reason: reason})
}

func (m *Manager) cleanupRecv(id string) {
	m.mu.Lock()
	st := m.recv[id]
	delete(m.recv, id)
	m.mu.Unlock()
	if st != nil && st.file != nil {
		_ = st.file.Sync()
		_ = st.file.Close()
		if st.partPath == st.finalPath {
			_ = os.Remove(st.partPath)
			return
		}
		// Keep bytes for resume under stable file_id part name.
		if st.offer.FileID != "" {
			stable := protocol.PartPath(st.finalPath, st.offer.FileID)
			if st.partPath != stable {
				_ = os.Remove(stable)
				_ = os.Rename(st.partPath, stable)
			}
		}
	}
}

func (m *Manager) closeDirect(id string) {
	m.mu.Lock()
	s := m.directSess[id]
	delete(m.directSess, id)
	m.mu.Unlock()
	if s != nil {
		_ = s.Close()
	}
}

func (m *Manager) send(v any) error {
	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	return m.conn.WriteJSON(v)
}

func extractID(data []byte) string {
	var m map[string]any
	_ = json.Unmarshal(data, &m)
	if id, ok := m["transfer_id"].(string); ok {
		return id
	}
	return ""
}

func FileInfo(outboxDir, rel string) (size int64, sum string, err error) {
	rel = filepath.Clean(filepath.FromSlash(rel))
	full := filepath.Join(outboxDir, rel)
	r, err := filepath.Rel(outboxDir, full)
	if err != nil || strings.HasPrefix(r, "..") {
		return 0, "", fmt.Errorf("path outside outbox")
	}
	f, err := os.Open(full)
	if err != nil {
		return 0, "", err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return 0, "", err
	}
	return n, hex.EncodeToString(h.Sum(nil)), nil
}

func DefaultShareDir() string {
	if v := os.Getenv("KNOT_SHARE_DIR"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "./knot-share"
	}
	return filepath.Join(home, "knot-inbox")
}
