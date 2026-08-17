package transfers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/knot-infra/knot/internal/network/transport"
	"github.com/knot-infra/knot/internal/store"
	"github.com/knot-infra/knot/pkg/protocol"
)

var (
	ErrTooLarge      = errors.New("transfer exceeds size limit")
	ErrDeviceOffline = errors.New("device offline")
	ErrBadPath       = errors.New("invalid source path")
	ErrNotFound      = errors.New("transfer not found")
)

type Sender interface {
	SendJSON(deviceID string, v any) error
	IsOnline(deviceID string) bool
}

type Options struct {
	ForceRelay    bool
	STUNURLs      []string
	DirectTimeout time.Duration
}

type Service struct {
	Store  *store.Store
	Sender Sender
	Opts   Options

	mu         sync.Mutex
	accepted   map[string]map[string]bool
	pathPicked map[string]bool
	timers     map[string]*time.Timer
	hooks      []func(ctx context.Context, t *store.Transfer)
}

func New(st *store.Store, sender Sender, opts Options) *Service {
	if opts.DirectTimeout <= 0 {
		opts.DirectTimeout = 3 * time.Second
	}
	return &Service{
		Store:      st,
		Sender:     sender,
		Opts:       opts,
		accepted:   make(map[string]map[string]bool),
		pathPicked: make(map[string]bool),
		timers:     make(map[string]*time.Timer),
	}
}

// OnTerminal registers a hook invoked when a transfer reaches completed/failed/aborted.
func (s *Service) OnTerminal(fn func(ctx context.Context, t *store.Transfer)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hooks = append(s.hooks, fn)
}

func (s *Service) fireTerminal(ctx context.Context, id string) {
	t, err := s.Store.GetTransferByID(ctx, id)
	if err != nil {
		return
	}
	s.mu.Lock()
	hooks := append([]func(context.Context, *store.Transfer){}, s.hooks...)
	s.mu.Unlock()
	for _, h := range hooks {
		h(ctx, t)
	}
}

type CreateRequest struct {
	UserID            string
	FromDeviceID      string
	ToDeviceID        string
	Filename          string
	SourcePath        string
	Size              int64
	SHA256            string
	SourceFromStorage bool
	DestStoragePath   string
	FileID            string
	ResumeOffset      int64
	IsStorage         bool
}

func (s *Service) Create(ctx context.Context, req CreateRequest) (*store.Transfer, error) {
	maxBytes := int64(protocol.MaxTransferBytes)
	if req.IsStorage || req.DestStoragePath != "" || req.SourceFromStorage {
		req.IsStorage = true
		maxBytes = protocol.MaxStorageTransferBytes
	}
	if req.Size <= 0 || req.Size > maxBytes {
		return nil, ErrTooLarge
	}
	if req.ResumeOffset < 0 || (req.Size > 0 && req.ResumeOffset >= req.Size) {
		return nil, fmt.Errorf("invalid resume offset")
	}
	if req.FromDeviceID == "" || req.ToDeviceID == "" || req.FromDeviceID == req.ToDeviceID {
		return nil, fmt.Errorf("from_device_id and to_device_id required and must differ")
	}
	if req.SHA256 == "" || len(req.SHA256) != 64 {
		return nil, fmt.Errorf("sha256 must be 64 hex chars")
	}
	filename := path.Base(strings.ReplaceAll(req.Filename, "\\", "/"))
	if filename == "" || filename == "." || filename == ".." {
		return nil, fmt.Errorf("filename required")
	}
	srcPath := sanitizeRelPath(req.SourcePath)
	if srcPath == "" {
		srcPath = filename
	}
	if req.SourceFromStorage && srcPath == "" {
		return nil, ErrBadPath
	}
	destStorage := ""
	if req.DestStoragePath != "" {
		destStorage = sanitizeRelPath(req.DestStoragePath)
		if destStorage == "" {
			return nil, ErrBadPath
		}
	}
	if !s.Sender.IsOnline(req.FromDeviceID) || !s.Sender.IsOnline(req.ToDeviceID) {
		return nil, ErrDeviceOffline
	}

	from, err := s.Store.GetDevice(ctx, req.UserID, req.FromDeviceID)
	if err != nil || from.RevokedAt != nil {
		return nil, fmt.Errorf("from device not found")
	}
	to, err := s.Store.GetDevice(ctx, req.UserID, req.ToDeviceID)
	if err != nil || to.RevokedAt != nil {
		return nil, fmt.Errorf("to device not found")
	}

	t := &store.Transfer{
		ID:           store.NewID(),
		UserID:       req.UserID,
		FromDeviceID: req.FromDeviceID,
		ToDeviceID:   req.ToDeviceID,
		Filename:     filename,
		SourcePath:   srcPath,
		Size:         req.Size,
		SHA256:       strings.ToLower(req.SHA256),
		Status:       store.TransferOffered,
		FileID:       req.FileID,
		ResumeOffset: req.ResumeOffset,
		IsStorage:    req.IsStorage,
	}
	if err := s.Store.CreateTransfer(ctx, t); err != nil {
		return nil, err
	}

	s.mu.Lock()
	s.accepted[t.ID] = map[string]bool{}
	s.mu.Unlock()

	offerSrc := protocol.TransferOffer{
		Type: protocol.TypeTransferOffer, TransferID: t.ID, Role: "source",
		FromDeviceID: t.FromDeviceID, ToDeviceID: t.ToDeviceID,
		Filename: t.Filename, SourcePath: t.SourcePath, Size: t.Size, SHA256: t.SHA256,
		ChunkBytes:        protocol.DefaultChunkBytes,
		SourceFromStorage: req.SourceFromStorage,
		DestStoragePath:   destStorage,
		FileID:            req.FileID,
		ResumeOffset:      req.ResumeOffset,
	}
	offerDst := offerSrc
	offerDst.Role = "dest"
	if !req.SourceFromStorage {
		offerDst.SourcePath = ""
	}

	if err := s.Sender.SendJSON(t.FromDeviceID, offerSrc); err != nil {
		_ = s.fail(ctx, t.ID, "failed to offer to source: "+err.Error())
		return nil, err
	}
	if err := s.Sender.SendJSON(t.ToDeviceID, offerDst); err != nil {
		_ = s.fail(ctx, t.ID, "failed to offer to dest: "+err.Error())
		_ = s.Sender.SendJSON(t.FromDeviceID, protocol.TransferAbort{
			Type: protocol.TypeTransferAbort, TransferID: t.ID, Reason: "dest unreachable",
		})
		return nil, err
	}
	return t, nil
}

func (s *Service) Get(ctx context.Context, userID, id string) (*store.Transfer, error) {
	t, err := s.Store.GetTransfer(ctx, userID, id)
	if err != nil {
		if store.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return t, nil
}

func (s *Service) List(ctx context.Context, userID string) ([]store.Transfer, error) {
	return s.Store.ListTransfers(ctx, userID, 50)
}

func (s *Service) Abort(ctx context.Context, userID, id, reason string) error {
	t, err := s.Get(ctx, userID, id)
	if err != nil {
		return err
	}
	if t.Status == store.TransferCompleted || t.Status == store.TransferAborted {
		return nil
	}
	s.cancelTimer(id)
	_ = s.Store.UpdateTransferStatus(ctx, id, store.TransferAborted, reason, true)
	msg := protocol.TransferAbort{Type: protocol.TypeTransferAbort, TransferID: id, Reason: reason}
	_ = s.Sender.SendJSON(t.FromDeviceID, msg)
	_ = s.Sender.SendJSON(t.ToDeviceID, msg)
	s.fireTerminal(ctx, id)
	return nil
}

func (s *Service) HandleAgentMessage(ctx context.Context, fromDeviceID string, envelopeType string, raw []byte) error {
	switch envelopeType {
	case protocol.TypeTransferAccept:
		var msg protocol.TransferAccept
		if err := decode(raw, &msg); err != nil {
			return err
		}
		return s.onAccept(ctx, fromDeviceID, msg)
	case protocol.TypeTransferReject:
		var msg protocol.TransferReject
		if err := decode(raw, &msg); err != nil {
			return err
		}
		return s.onReject(ctx, fromDeviceID, msg)
	case protocol.TypePathCandidate:
		var msg protocol.PathCandidateMsg
		if err := decode(raw, &msg); err != nil {
			return err
		}
		return s.onPathCandidate(ctx, fromDeviceID, msg)
	case protocol.TypePathSelected:
		var msg protocol.PathSelected
		if err := decode(raw, &msg); err != nil {
			return err
		}
		return s.onPathSelected(ctx, fromDeviceID, msg)
	case protocol.TypeTransferChunk:
		var msg protocol.TransferChunk
		if err := decode(raw, &msg); err != nil {
			return err
		}
		return s.onChunk(ctx, fromDeviceID, msg)
	case protocol.TypeTransferAck:
		var msg protocol.TransferAck
		if err := decode(raw, &msg); err != nil {
			return err
		}
		return s.onAck(ctx, fromDeviceID, msg)
	case protocol.TypeTransferComplete:
		var msg protocol.TransferComplete
		if err := decode(raw, &msg); err != nil {
			return err
		}
		return s.onComplete(ctx, fromDeviceID, msg)
	case protocol.TypeTransferAbort:
		var msg protocol.TransferAbort
		if err := decode(raw, &msg); err != nil {
			return err
		}
		return s.onAbort(ctx, fromDeviceID, msg)
	}
	return nil
}

func (s *Service) onAccept(ctx context.Context, deviceID string, msg protocol.TransferAccept) error {
	t, err := s.Store.GetTransferByID(ctx, msg.TransferID)
	if err != nil {
		return err
	}
	if deviceID != t.FromDeviceID && deviceID != t.ToDeviceID {
		return fmt.Errorf("device not part of transfer")
	}
	s.mu.Lock()
	if s.accepted[msg.TransferID] == nil {
		s.accepted[msg.TransferID] = map[string]bool{}
	}
	s.accepted[msg.TransferID][deviceID] = true
	both := s.accepted[msg.TransferID][t.FromDeviceID] && s.accepted[msg.TransferID][t.ToDeviceID]
	s.mu.Unlock()
	if !both {
		return nil
	}

	_ = s.Store.UpdateTransferStatus(ctx, t.ID, store.TransferNegotiating, "", false)

	fromDev, _ := s.Store.GetDeviceByID(ctx, t.FromDeviceID)
	toDev, _ := s.Store.GetDeviceByID(ctx, t.ToDeviceID)
	force := s.Opts.ForceRelay
	_ = s.Sender.SendJSON(t.FromDeviceID, protocol.PathNegotiate{
		Type: protocol.TypePathNegotiate, TransferID: t.ID, Role: "source",
		PeerDeviceID: t.ToDeviceID, PeerPublicKey: toDev.PublicKey,
		ForceRelay: force, STUNURLs: s.Opts.STUNURLs,
	})
	_ = s.Sender.SendJSON(t.ToDeviceID, protocol.PathNegotiate{
		Type: protocol.TypePathNegotiate, TransferID: t.ID, Role: "dest",
		PeerDeviceID: t.FromDeviceID, PeerPublicKey: fromDev.PublicKey,
		ForceRelay: force, STUNURLs: s.Opts.STUNURLs,
	})

	// Fallback timer → relay if no path_selected
	s.mu.Lock()
	if s.timers[t.ID] != nil {
		s.timers[t.ID].Stop()
	}
	tid := t.ID
	s.timers[tid] = time.AfterFunc(s.Opts.DirectTimeout+500*time.Millisecond, func() {
		_ = s.startWithPath(context.Background(), tid, transport.PathRelay)
	})
	s.mu.Unlock()
	return nil
}

func (s *Service) onPathCandidate(ctx context.Context, fromDeviceID string, msg protocol.PathCandidateMsg) error {
	t, err := s.Store.GetTransferByID(ctx, msg.TransferID)
	if err != nil {
		return err
	}
	peer := t.ToDeviceID
	if fromDeviceID == t.ToDeviceID {
		peer = t.FromDeviceID
	}
	msg.Type = protocol.TypePathCandidate
	msg.DeviceID = fromDeviceID
	return s.Sender.SendJSON(peer, msg)
}

func (s *Service) onPathSelected(ctx context.Context, fromDeviceID string, msg protocol.PathSelected) error {
	t, err := s.Store.GetTransferByID(ctx, msg.TransferID)
	if err != nil {
		return err
	}
	// Prefer source decision; dest may also report.
	if fromDeviceID != t.FromDeviceID && fromDeviceID != t.ToDeviceID {
		return fmt.Errorf("device not part of transfer")
	}
	path := msg.Path
	if path != transport.PathDirect && path != transport.PathRelay {
		path = transport.PathRelay
	}
	// If source reports direct, use it; if dest reports first as relay, wait for source unless timer.
	if fromDeviceID == t.FromDeviceID {
		return s.startWithPath(ctx, t.ID, path)
	}
	// dest-only report: ignore if source will decide; keep for logging
	return nil
}

func (s *Service) startWithPath(ctx context.Context, transferID, path string) error {
	s.mu.Lock()
	if s.pathPicked[transferID] {
		s.mu.Unlock()
		return nil
	}
	s.pathPicked[transferID] = true
	if tm := s.timers[transferID]; tm != nil {
		tm.Stop()
		delete(s.timers, transferID)
	}
	s.mu.Unlock()

	t, err := s.Store.GetTransferByID(ctx, transferID)
	if err != nil {
		return err
	}
	_ = s.Store.UpdateTransferPath(ctx, transferID, path)
	_ = s.Store.UpdateTransferStatus(ctx, transferID, store.TransferTransferring, "", false)
	start := protocol.TransferStart{Type: protocol.TypeTransferStart, TransferID: transferID, Path: path}
	_ = s.Sender.SendJSON(t.FromDeviceID, start)
	_ = s.Sender.SendJSON(t.ToDeviceID, start)
	return nil
}

func (s *Service) cancelTimer(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if tm := s.timers[id]; tm != nil {
		tm.Stop()
		delete(s.timers, id)
	}
	delete(s.pathPicked, id)
	delete(s.accepted, id)
}

func (s *Service) onReject(ctx context.Context, deviceID string, msg protocol.TransferReject) error {
	t, err := s.Store.GetTransferByID(ctx, msg.TransferID)
	if err != nil {
		return err
	}
	reason := msg.Reason
	if reason == "" {
		reason = "rejected by " + deviceID
	}
	s.cancelTimer(t.ID)
	_ = s.fail(ctx, t.ID, reason)
	peer := t.ToDeviceID
	if deviceID == t.ToDeviceID {
		peer = t.FromDeviceID
	}
	_ = s.Sender.SendJSON(peer, protocol.TransferAbort{
		Type: protocol.TypeTransferAbort, TransferID: t.ID, DeviceID: deviceID, Reason: reason,
	})
	return nil
}

func (s *Service) onChunk(ctx context.Context, deviceID string, msg protocol.TransferChunk) error {
	t, err := s.Store.GetTransferByID(ctx, msg.TransferID)
	if err != nil {
		return err
	}
	if deviceID != t.FromDeviceID {
		return fmt.Errorf("only source may send chunks")
	}
	_ = s.Store.UpdateTransferStatus(ctx, t.ID, store.TransferTransferring, "", false)
	return s.Sender.SendJSON(t.ToDeviceID, msg)
}

func (s *Service) onAck(ctx context.Context, deviceID string, msg protocol.TransferAck) error {
	t, err := s.Store.GetTransferByID(ctx, msg.TransferID)
	if err != nil {
		return err
	}
	if deviceID != t.ToDeviceID {
		return fmt.Errorf("only dest may ack")
	}
	if msg.BytesReceived > 0 {
		_ = s.Store.UpdateTransferBytes(ctx, t.ID, msg.BytesReceived)
		if t.FileID != "" {
			_ = s.Store.UpdateStorageFileProgress(ctx, t.FileID, msg.BytesReceived, t.ID, store.FileUploading)
		}
	}
	return s.Sender.SendJSON(t.FromDeviceID, msg)
}

func (s *Service) onComplete(ctx context.Context, deviceID string, msg protocol.TransferComplete) error {
	t, err := s.Store.GetTransferByID(ctx, msg.TransferID)
	if err != nil {
		return err
	}
	if deviceID != t.ToDeviceID {
		return fmt.Errorf("only dest may complete")
	}
	if !strings.EqualFold(msg.SHA256, t.SHA256) {
		_ = s.fail(ctx, t.ID, "sha256 mismatch")
		_ = s.Sender.SendJSON(t.FromDeviceID, protocol.TransferAbort{
			Type: protocol.TypeTransferAbort, TransferID: t.ID, Reason: "sha256 mismatch",
		})
		return fmt.Errorf("sha256 mismatch")
	}
	_ = s.Store.UpdateTransferStatus(ctx, t.ID, store.TransferCompleted, "", true)
	_ = s.Sender.SendJSON(t.FromDeviceID, protocol.TransferComplete{
		Type: protocol.TypeTransferComplete, TransferID: t.ID, DeviceID: deviceID, SHA256: msg.SHA256,
	})
	s.cancelTimer(t.ID)
	s.fireTerminal(ctx, t.ID)
	return nil
}

func (s *Service) onAbort(ctx context.Context, deviceID string, msg protocol.TransferAbort) error {
	t, err := s.Store.GetTransferByID(ctx, msg.TransferID)
	if err != nil {
		return err
	}
	reason := msg.Reason
	if reason == "" {
		reason = "aborted by agent"
	}
	s.cancelTimer(t.ID)
	_ = s.fail(ctx, t.ID, reason)
	peer := t.ToDeviceID
	if deviceID == t.ToDeviceID {
		peer = t.FromDeviceID
	}
	msg.Type = protocol.TypeTransferAbort
	_ = s.Sender.SendJSON(peer, msg)
	return nil
}

func (s *Service) fail(ctx context.Context, id, reason string) error {
	s.cancelTimer(id)
	err := s.Store.UpdateTransferStatus(ctx, id, store.TransferFailed, reason, true)
	s.fireTerminal(ctx, id)
	return err
}

func sanitizeRelPath(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	p = path.Clean("/" + p)
	p = strings.TrimPrefix(p, "/")
	if p == "." || p == ".." || strings.HasPrefix(p, "../") {
		return ""
	}
	return p
}

func decode(raw []byte, v any) error {
	return json.Unmarshal(raw, v)
}
