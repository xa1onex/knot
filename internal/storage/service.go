package storage

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/knot-infra/knot/internal/storage/pathsafe"
	"github.com/knot-infra/knot/internal/store"
	"github.com/knot-infra/knot/internal/transfers"
	"github.com/knot-infra/knot/pkg/protocol"
)

var (
	ErrDeviceOffline = errors.New("device offline")
	ErrBadPath       = errors.New("invalid storage path")
	ErrAgent         = errors.New("agent storage op failed")
	ErrTimeout       = errors.New("storage op timeout")
	ErrNotFound      = errors.New("storage file not found")
	ErrQuota         = errors.New("storage quota exceeded")
	ErrNameConflict  = errors.New("name conflict")
)

type Sender interface {
	SendJSON(deviceID string, v any) error
	IsOnline(deviceID string) bool
}

type Quotas struct {
	MaxTotalBytes int64 // 0 = unlimited
	MaxFileBytes  int64
	MaxFiles      int64
}

type Service struct {
	Store     *store.Store
	Sender    Sender
	Transfers *transfers.Service
	Timeout   time.Duration
	Quotas    Quotas

	mu      sync.Mutex
	pending map[string]chan protocol.StorageOpResult
	// credByTransfer remembers which credential started a storage upload so
	// pre-commit quota checks can apply per-credential limits.
	credByTransfer map[string]string

	// OnMutate is called after a successful mutation on a device (mkdir/delete/move/copy/put/transfer).
	// The files index uses this; storage must not import the files package.
	OnMutate func(userID, deviceID string)
}

func New(st *store.Store, sender Sender, xfer *transfers.Service) *Service {
	s := &Service{
		Store:          st,
		Sender:         sender,
		Transfers:      xfer,
		Timeout:        15 * time.Second,
		pending:        make(map[string]chan protocol.StorageOpResult),
		credByTransfer: make(map[string]string),
	}
	if xfer != nil {
		xfer.OnTerminal(s.onTransferTerminal)
	}
	return s
}

func (s *Service) onTransferTerminal(ctx context.Context, t *store.Transfer) {
	if t == nil || t.FileID == "" {
		return
	}
	s.mu.Lock()
	credID := s.credByTransfer[t.ID]
	delete(s.credByTransfer, t.ID)
	s.mu.Unlock()

	switch t.Status {
	case store.TransferCompleted:
		// Pre-commit quota re-check (concurrent uploads / resume)
		if err := s.checkQuotaCommit(ctx, t.UserID, credID, t.ToDeviceID, t.FileID, t.Size); err != nil {
			_ = s.Store.MarkStorageFileIncomplete(ctx, t.FileID, t.Size)
			if f, e := s.Store.GetStorageFile(ctx, t.UserID, t.FileID); e == nil {
				_, _ = s.call(ctx, protocol.StorageOp{
					Type: protocol.TypeStorageOp, Op: protocol.StorageOpDelete, Path: f.Path,
				}, t.ToDeviceID)
			}
			_ = s.Store.UpdateTransferStatus(ctx, t.ID, store.TransferFailed, ErrQuota.Error(), true)
			s.notifyMutate(t.UserID, t.ToDeviceID)
			return
		}
		_ = s.Store.CompleteStorageFile(ctx, t.FileID, t.Size, t.SHA256)
		s.notifyMutate(t.UserID, t.ToDeviceID)
	case store.TransferAborted, store.TransferFailed:
		bytes := t.ResumeOffset
		if f, err := s.Store.GetStorageFile(ctx, t.UserID, t.FileID); err == nil {
			if res, err := s.call(ctx, protocol.StorageOp{
				Type: protocol.TypeStorageOp, Op: protocol.StorageOpPartial, Path: f.Path, FileID: f.ID,
			}, t.ToDeviceID); err == nil {
				bytes = res.PartialBytes
			}
			_ = s.Store.MarkStorageFileIncomplete(ctx, t.FileID, bytes)
		}
	}
}

func (s *Service) HandleAgentMessage(_ context.Context, _ string, envelopeType string, raw []byte) error {
	if envelopeType != protocol.TypeStorageOpResult {
		return nil
	}
	var res protocol.StorageOpResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return err
	}
	s.mu.Lock()
	ch := s.pending[res.RequestID]
	s.mu.Unlock()
	if ch == nil {
		return nil
	}
	select {
	case ch <- res:
	default:
	}
	return nil
}

func (s *Service) call(ctx context.Context, msg protocol.StorageOp, deviceID string) (protocol.StorageOpResult, error) {
	if !s.Sender.IsOnline(deviceID) {
		return protocol.StorageOpResult{}, ErrDeviceOffline
	}
	reqID := store.NewID()
	msg.Type = protocol.TypeStorageOp
	msg.RequestID = reqID
	ch := make(chan protocol.StorageOpResult, 1)
	s.mu.Lock()
	s.pending[reqID] = ch
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.pending, reqID)
		s.mu.Unlock()
	}()
	if err := s.Sender.SendJSON(deviceID, msg); err != nil {
		return protocol.StorageOpResult{}, err
	}
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return protocol.StorageOpResult{}, ctx.Err()
	case <-timer.C:
		return protocol.StorageOpResult{}, ErrTimeout
	case res := <-ch:
		if !res.OK {
			return res, fmt.Errorf("%w: %s", ErrAgent, res.Error)
		}
		return res, nil
	}
}

func (s *Service) validateDevice(ctx context.Context, userID, deviceID string) error {
	d, err := s.Store.GetDevice(ctx, userID, deviceID)
	if err != nil || d.RevokedAt != nil {
		return fmt.Errorf("device not found")
	}
	if !s.Sender.IsOnline(deviceID) {
		return ErrDeviceOffline
	}
	return nil
}

func normalizePath(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" || p == "." {
		return "", nil
	}
	canon, err := pathsafe.CanonicalRel(p)
	if err != nil {
		return "", ErrBadPath
	}
	return canon, nil
}

func (s *Service) effectiveQuotas(cred *store.Credential) Quotas {
	q := s.Quotas
	if cred == nil {
		return q
	}
	if cred.MaxStorageBytes != nil && (q.MaxTotalBytes == 0 || *cred.MaxStorageBytes < q.MaxTotalBytes) {
		q.MaxTotalBytes = *cred.MaxStorageBytes
	}
	if cred.MaxFileBytes != nil && (q.MaxFileBytes == 0 || *cred.MaxFileBytes < q.MaxFileBytes) {
		q.MaxFileBytes = *cred.MaxFileBytes
	}
	if cred.MaxFiles != nil && (q.MaxFiles == 0 || *cred.MaxFiles < q.MaxFiles) {
		q.MaxFiles = *cred.MaxFiles
	}
	return q
}

func (s *Service) checkQuotaStart(ctx context.Context, userID, credID, deviceID, fileID string, size int64) error {
	var cred *store.Credential
	if credID != "" {
		cred, _ = s.Store.GetCredentialByID(ctx, userID, credID)
	}
	q := s.effectiveQuotas(cred)
	if q.MaxFileBytes > 0 && size > q.MaxFileBytes {
		return fmt.Errorf("%w: file size", ErrQuota)
	}
	used, count, err := s.Store.StorageUsage(ctx, userID, deviceID)
	if err != nil {
		return err
	}
	// Replacing incomplete/complete same file_id does not add a new file slot
	replacing := false
	var oldSize int64
	if fileID != "" {
		if f, err := s.Store.GetStorageFile(ctx, userID, fileID); err == nil {
			replacing = true
			if f.Status == store.FileComplete {
				oldSize = f.Size
			}
		}
	}
	if q.MaxFiles > 0 && !replacing && count >= q.MaxFiles {
		return fmt.Errorf("%w: file count", ErrQuota)
	}
	if q.MaxTotalBytes > 0 && used-oldSize+size > q.MaxTotalBytes {
		return fmt.Errorf("%w: total storage", ErrQuota)
	}
	return nil
}

func (s *Service) checkQuotaCommit(ctx context.Context, userID, credID, deviceID, fileID string, size int64) error {
	return s.checkQuotaStart(ctx, userID, credID, deviceID, fileID, size)
}

func (s *Service) List(ctx context.Context, userID, deviceID, rel string) ([]protocol.StorageEntry, error) {
	if err := s.validateDevice(ctx, userID, deviceID); err != nil {
		return nil, err
	}
	canon, err := normalizePath(rel)
	if err != nil {
		return nil, err
	}
	res, err := s.call(ctx, protocol.StorageOp{Op: protocol.StorageOpList, Path: canon}, deviceID)
	if err != nil {
		return nil, err
	}
	ents := res.Entries
	if ents == nil {
		ents = []protocol.StorageEntry{}
	}
	for i := range ents {
		if f, err := s.Store.GetStorageFileByPath(ctx, userID, deviceID, ents[i].Path); err == nil && f.Status == store.FileComplete {
			ents[i].FileID = f.ID
			if f.SHA256 != "" {
				ents[i].SHA256 = f.SHA256
			}
		}
	}
	return ents, nil
}

func (s *Service) Stat(ctx context.Context, userID, deviceID, rel string) (*protocol.StorageStat, error) {
	if err := s.validateDevice(ctx, userID, deviceID); err != nil {
		return nil, err
	}
	canon, err := normalizePath(rel)
	if err != nil {
		return nil, err
	}
	res, err := s.call(ctx, protocol.StorageOp{Op: protocol.StorageOpStat, Path: canon}, deviceID)
	if err != nil {
		return nil, err
	}
	if res.Stat == nil {
		return nil, fmt.Errorf("%w: empty stat", ErrAgent)
	}
	if f, err := s.Store.GetStorageFileByPath(ctx, userID, deviceID, canon); err == nil {
		res.Stat.FileID = f.ID
	}
	return res.Stat, nil
}

func (s *Service) Mkdir(ctx context.Context, userID, deviceID, rel string) (*protocol.StorageStat, error) {
	if err := s.validateDevice(ctx, userID, deviceID); err != nil {
		return nil, err
	}
	canon, err := normalizePath(rel)
	if err != nil || canon == "" {
		return nil, ErrBadPath
	}
	res, err := s.call(ctx, protocol.StorageOp{Op: protocol.StorageOpMkdir, Path: canon}, deviceID)
	if err != nil {
		return nil, err
	}
	s.notifyMutate(userID, deviceID)
	return res.Stat, nil
}

func (s *Service) Delete(ctx context.Context, userID, deviceID, rel string) error {
	if err := s.validateDevice(ctx, userID, deviceID); err != nil {
		return err
	}
	canon, err := normalizePath(rel)
	if err != nil || canon == "" {
		return ErrBadPath
	}
	if _, err = s.call(ctx, protocol.StorageOp{Op: protocol.StorageOpDelete, Path: canon}, deviceID); err != nil {
		return err
	}
	_ = s.Store.DeleteStorageFileByPath(ctx, userID, deviceID, canon)
	s.notifyMutate(userID, deviceID)
	return nil
}

func (s *Service) Move(ctx context.Context, userID, deviceID, from, to string) (*protocol.StorageStat, error) {
	if err := s.validateDevice(ctx, userID, deviceID); err != nil {
		return nil, err
	}
	fromC, err := normalizePath(from)
	if err != nil || fromC == "" {
		return nil, ErrBadPath
	}
	toC, err := normalizePath(to)
	if err != nil || toC == "" {
		return nil, ErrBadPath
	}
	res, err := s.call(ctx, protocol.StorageOp{Op: protocol.StorageOpMove, FromPath: fromC, ToPath: toC}, deviceID)
	if err != nil {
		return nil, err
	}
	if f, err := s.Store.GetStorageFileByPath(ctx, userID, deviceID, fromC); err == nil {
		_ = s.Store.UpdateStorageFilePath(ctx, f.ID, toC)
		if res.Stat != nil {
			res.Stat.FileID = f.ID
		}
	}
	s.notifyMutate(userID, deviceID)
	return res.Stat, nil
}

func (s *Service) Copy(ctx context.Context, userID, deviceID, from, to string) (*protocol.StorageStat, error) {
	if err := s.validateDevice(ctx, userID, deviceID); err != nil {
		return nil, err
	}
	fromC, err := normalizePath(from)
	if err != nil || fromC == "" {
		return nil, ErrBadPath
	}
	toC, err := normalizePath(to)
	if err != nil || toC == "" {
		return nil, ErrBadPath
	}
	st, err := s.Stat(ctx, userID, deviceID, fromC)
	if err != nil {
		return nil, err
	}
	if err := s.checkQuotaStart(ctx, userID, "", deviceID, "", st.Size); err != nil {
		return nil, err
	}
	res, err := s.call(ctx, protocol.StorageOp{Op: protocol.StorageOpCopy, FromPath: fromC, ToPath: toC}, deviceID)
	if err != nil {
		return nil, err
	}
	if res.Stat != nil && !res.Stat.IsDir {
		meta := &store.StorageFile{
			UserID: userID, DeviceID: deviceID, Path: toC,
			Size: res.Stat.Size, SHA256: res.Stat.SHA256, Status: store.FileComplete,
		}
		_ = s.Store.UpsertStorageFile(ctx, meta)
		res.Stat.FileID = meta.ID
	}
	s.notifyMutate(userID, deviceID)
	return res.Stat, nil
}

func (s *Service) GetFile(ctx context.Context, userID, fileID string) (*store.StorageFile, error) {
	f, err := s.Store.GetStorageFile(ctx, userID, fileID)
	if err != nil {
		if store.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return f, nil
}

type UploadRequest struct {
	UserID       string
	CredID       string
	DeviceID     string
	Path         string
	FromDeviceID string
	SourcePath   string
	Size         int64
	SHA256       string
	Resume       bool
}

type UploadResult struct {
	Transfer *store.Transfer
	File     *store.StorageFile
}

func (s *Service) Upload(ctx context.Context, req UploadRequest) (*UploadResult, error) {
	if s.Transfers == nil {
		return nil, fmt.Errorf("transfers unavailable")
	}
	if err := s.validateDevice(ctx, req.UserID, req.DeviceID); err != nil {
		return nil, err
	}
	if err := s.validateDevice(ctx, req.UserID, req.FromDeviceID); err != nil {
		return nil, err
	}
	canon, err := normalizePath(req.Path)
	if err != nil || canon == "" {
		return nil, ErrBadPath
	}
	filename := path.Base(canon)
	src := req.SourcePath
	if src == "" {
		src = filename
	}

	meta := &store.StorageFile{
		UserID: req.UserID, DeviceID: req.DeviceID, Path: canon,
		Size: req.Size, SHA256: strings.ToLower(req.SHA256),
		Status: store.FileUploading,
	}
	if req.Resume {
		if existing, err := s.Store.GetStorageFileByPath(ctx, req.UserID, req.DeviceID, canon); err == nil {
			meta.ID = existing.ID
		}
	} else {
		// Fresh upload always gets a new file_id so concurrent uploads to the same
		// path write distinct part files (never a byte mix of A and B).
		meta.ID = store.NewID()
	}
	if err := s.Store.UpsertStorageFile(ctx, meta); err != nil {
		return nil, err
	}

	var offset int64
	if req.Resume {
		res, err := s.call(ctx, protocol.StorageOp{
			Op: protocol.StorageOpPartial, Path: canon, FileID: meta.ID,
		}, req.DeviceID)
		if err != nil {
			return nil, err
		}
		offset = res.PartialBytes
		if offset >= req.Size && req.Size > 0 {
			return nil, fmt.Errorf("upload already complete on device")
		}
	}

	if err := s.checkQuotaStart(ctx, req.UserID, req.CredID, req.DeviceID, meta.ID, req.Size); err != nil {
		return nil, err
	}

	meta.BytesReceived = offset
	t, err := s.Transfers.Create(ctx, transfers.CreateRequest{
		UserID:          req.UserID,
		FromDeviceID:    req.FromDeviceID,
		ToDeviceID:      req.DeviceID,
		Filename:        filename,
		SourcePath:      src,
		Size:            req.Size,
		SHA256:          req.SHA256,
		DestStoragePath: canon,
		FileID:          meta.ID,
		ResumeOffset:    offset,
		IsStorage:       true,
	})
	if err != nil {
		return nil, err
	}
	_ = s.Store.UpdateStorageFileProgress(ctx, meta.ID, offset, t.ID, store.FileUploading)
	meta.TransferID = t.ID
	if req.CredID != "" {
		s.mu.Lock()
		s.credByTransfer[t.ID] = req.CredID
		s.mu.Unlock()
	}
	return &UploadResult{Transfer: t, File: meta}, nil
}

type TransferBetweenRequest struct {
	UserID       string
	CredID       string
	FromDeviceID string
	FromPath     string
	ToDeviceID   string
	ToPath       string
}

// TransferBetween copies a complete storage file from one node to another via Transfer.
// Same-device paths should use Copy; this method requires distinct devices.
func (s *Service) TransferBetween(ctx context.Context, req TransferBetweenRequest) (*UploadResult, error) {
	if s.Transfers == nil {
		return nil, fmt.Errorf("transfers unavailable")
	}
	if req.FromDeviceID == "" || req.ToDeviceID == "" || req.FromDeviceID == req.ToDeviceID {
		return nil, fmt.Errorf("from_device_id and to_device_id required and must differ")
	}
	if err := s.validateDevice(ctx, req.UserID, req.FromDeviceID); err != nil {
		return nil, err
	}
	if err := s.validateDevice(ctx, req.UserID, req.ToDeviceID); err != nil {
		return nil, err
	}
	fromC, err := normalizePath(req.FromPath)
	if err != nil || fromC == "" {
		return nil, ErrBadPath
	}
	toC, err := normalizePath(req.ToPath)
	if err != nil || toC == "" {
		return nil, ErrBadPath
	}
	st, err := s.Stat(ctx, req.UserID, req.FromDeviceID, fromC)
	if err != nil {
		return nil, err
	}
	if st.IsDir {
		return nil, fmt.Errorf("cannot transfer a directory")
	}
	if st.SHA256 == "" || st.Size <= 0 {
		return nil, fmt.Errorf("incomplete source file")
	}

	meta := &store.StorageFile{
		ID: store.NewID(), UserID: req.UserID, DeviceID: req.ToDeviceID, Path: toC,
		Size: st.Size, SHA256: strings.ToLower(st.SHA256), Status: store.FileUploading,
	}
	if err := s.Store.UpsertStorageFile(ctx, meta); err != nil {
		return nil, err
	}
	if err := s.checkQuotaStart(ctx, req.UserID, req.CredID, req.ToDeviceID, meta.ID, st.Size); err != nil {
		return nil, err
	}

	t, err := s.Transfers.Create(ctx, transfers.CreateRequest{
		UserID:            req.UserID,
		FromDeviceID:      req.FromDeviceID,
		ToDeviceID:        req.ToDeviceID,
		Filename:          path.Base(toC),
		SourcePath:        fromC,
		Size:              st.Size,
		SHA256:            st.SHA256,
		SourceFromStorage: true,
		DestStoragePath:   toC,
		FileID:            meta.ID,
		IsStorage:         true,
	})
	if err != nil {
		return nil, err
	}
	_ = s.Store.UpdateStorageFileProgress(ctx, meta.ID, 0, t.ID, store.FileUploading)
	meta.TransferID = t.ID
	if req.CredID != "" {
		s.mu.Lock()
		s.credByTransfer[t.ID] = req.CredID
		s.mu.Unlock()
	}
	return &UploadResult{Transfer: t, File: meta}, nil
}

type ReadRequest struct {
	UserID     string
	DeviceID   string
	Path       string
	ToDeviceID string
}

func (s *Service) Read(ctx context.Context, req ReadRequest) (*store.Transfer, error) {
	if s.Transfers == nil {
		return nil, fmt.Errorf("transfers unavailable")
	}
	if err := s.validateDevice(ctx, req.UserID, req.DeviceID); err != nil {
		return nil, err
	}
	if err := s.validateDevice(ctx, req.UserID, req.ToDeviceID); err != nil {
		return nil, err
	}
	canon, err := normalizePath(req.Path)
	if err != nil || canon == "" {
		return nil, ErrBadPath
	}
	st, err := s.Stat(ctx, req.UserID, req.DeviceID, canon)
	if err != nil {
		return nil, err
	}
	if st.IsDir {
		return nil, fmt.Errorf("path is a directory")
	}
	if st.SHA256 == "" || st.Size < 0 {
		return nil, fmt.Errorf("incomplete file metadata")
	}
	return s.Transfers.Create(ctx, transfers.CreateRequest{
		UserID:            req.UserID,
		FromDeviceID:      req.DeviceID,
		ToDeviceID:        req.ToDeviceID,
		Filename:          path.Base(canon),
		SourcePath:        canon,
		Size:              st.Size,
		SHA256:            st.SHA256,
		SourceFromStorage: true,
		FileID:            st.FileID,
		IsStorage:         true,
	})
}

type PutRequest struct {
	UserID    string
	CredID    string
	DeviceID  string
	Path      string
	Size      int64
	SHA256    string
	Overwrite bool
	Body      io.Reader
}

// Put streams bytes from the client (browser/desktop) onto a node's storage via agent write_* ops.
func (s *Service) Put(ctx context.Context, req PutRequest) (*protocol.StorageStat, error) {
	if err := s.validateDevice(ctx, req.UserID, req.DeviceID); err != nil {
		return nil, err
	}
	canon, err := normalizePath(req.Path)
	if err != nil || canon == "" {
		return nil, ErrBadPath
	}
	if req.Size <= 0 || req.Size > protocol.MaxStorageTransferBytes {
		return nil, fmt.Errorf("%w: size", ErrQuota)
	}
	if len(req.SHA256) != 64 {
		return nil, fmt.Errorf("sha256 must be 64 hex chars")
	}
	if !req.Overwrite {
		if _, err := s.Stat(ctx, req.UserID, req.DeviceID, canon); err == nil {
			return nil, ErrNameConflict
		}
	}
	meta := &store.StorageFile{
		ID: store.NewID(), UserID: req.UserID, DeviceID: req.DeviceID, Path: canon,
		Size: req.Size, SHA256: strings.ToLower(req.SHA256), Status: store.FileUploading,
	}
	if err := s.Store.UpsertStorageFile(ctx, meta); err != nil {
		return nil, err
	}
	if err := s.checkQuotaStart(ctx, req.UserID, req.CredID, req.DeviceID, meta.ID, req.Size); err != nil {
		return nil, err
	}

	if _, err := s.call(ctx, protocol.StorageOp{
		Op: protocol.StorageOpWriteStart, Path: canon, FileID: meta.ID,
		Size: req.Size, SHA256: meta.SHA256,
	}, req.DeviceID); err != nil {
		return nil, err
	}

	limited := io.LimitReader(req.Body, req.Size)
	const chunk = 256 << 10
	buf := make([]byte, chunk)
	var offset int64
	for offset < req.Size {
		n, err := limited.Read(buf)
		if n > 0 {
			if _, callErr := s.call(ctx, protocol.StorageOp{
				Op: protocol.StorageOpWriteChunk, FileID: meta.ID, Offset: offset,
				DataB64: base64.StdEncoding.EncodeToString(buf[:n]),
			}, req.DeviceID); callErr != nil {
				_, _ = s.call(ctx, protocol.StorageOp{Op: protocol.StorageOpWriteAbort, FileID: meta.ID}, req.DeviceID)
				_ = s.Store.MarkStorageFileIncomplete(ctx, meta.ID, offset)
				return nil, callErr
			}
			offset += int64(n)
			_ = s.Store.UpdateStorageFileProgress(ctx, meta.ID, offset, "", store.FileUploading)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			_, _ = s.call(ctx, protocol.StorageOp{Op: protocol.StorageOpWriteAbort, FileID: meta.ID}, req.DeviceID)
			_ = s.Store.MarkStorageFileIncomplete(ctx, meta.ID, offset)
			return nil, err
		}
	}
	if offset != req.Size {
		_, _ = s.call(ctx, protocol.StorageOp{Op: protocol.StorageOpWriteAbort, FileID: meta.ID}, req.DeviceID)
		return nil, fmt.Errorf("short body: got %d want %d", offset, req.Size)
	}

	res, err := s.call(ctx, protocol.StorageOp{Op: protocol.StorageOpWriteCommit, FileID: meta.ID}, req.DeviceID)
	if err != nil {
		_ = s.Store.MarkStorageFileIncomplete(ctx, meta.ID, offset)
		return nil, err
	}
	if err := s.checkQuotaCommit(ctx, req.UserID, req.CredID, req.DeviceID, meta.ID, req.Size); err != nil {
		_, _ = s.call(ctx, protocol.StorageOp{Op: protocol.StorageOpDelete, Path: canon}, req.DeviceID)
		_ = s.Store.MarkStorageFileIncomplete(ctx, meta.ID, 0)
		return nil, err
	}
	_ = s.Store.CompleteStorageFile(ctx, meta.ID, req.Size, meta.SHA256)
	if res.Stat != nil {
		res.Stat.FileID = meta.ID
	}
	s.notifyMutate(req.UserID, req.DeviceID)
	return res.Stat, nil
}

func (s *Service) notifyMutate(userID, deviceID string) {
	if s.OnMutate == nil || userID == "" || deviceID == "" {
		return
	}
	go s.OnMutate(userID, deviceID)
}

// Content returns small file bytes (preview / browser download), max 8 MiB.
func (s *Service) Content(ctx context.Context, userID, deviceID, rel string, maxBytes int64) (data []byte, mimeType string, st *protocol.StorageStat, err error) {
	if err := s.validateDevice(ctx, userID, deviceID); err != nil {
		return nil, "", nil, err
	}
	canon, err := normalizePath(rel)
	if err != nil || canon == "" {
		return nil, "", nil, ErrBadPath
	}
	if maxBytes <= 0 {
		maxBytes = 8 << 20
	}
	res, err := s.call(ctx, protocol.StorageOp{Op: protocol.StorageOpRead, Path: canon, MaxBytes: maxBytes}, deviceID)
	if err != nil {
		return nil, "", nil, err
	}
	raw, err := base64.StdEncoding.DecodeString(res.DataB64)
	if err != nil {
		return nil, "", nil, err
	}
	return raw, res.MimeType, res.Stat, nil
}

// Preview returns a derived thumbnail/preview from the agent cache.
// It never mutates user storage and may be regenerated after cache deletion.
func (s *Service) Preview(ctx context.Context, userID, deviceID, rel, variant string, maxPixels int) (data []byte, mimeType string, st *protocol.StorageStat, cacheKey string, err error) {
	if err := s.validateDevice(ctx, userID, deviceID); err != nil {
		return nil, "", nil, "", err
	}
	canon, err := normalizePath(rel)
	if err != nil || canon == "" {
		return nil, "", nil, "", ErrBadPath
	}
	if variant == "" {
		variant = "preview"
	}
	res, err := s.call(ctx, protocol.StorageOp{
		Op:        protocol.StorageOpPreview,
		Path:      canon,
		Preview:   variant,
		MaxPixels: maxPixels,
	}, deviceID)
	if err != nil {
		return nil, "", nil, "", err
	}
	raw, err := base64.StdEncoding.DecodeString(res.DataB64)
	if err != nil {
		return nil, "", nil, "", err
	}
	return raw, res.MimeType, res.Stat, res.CacheKey, nil
}

// ResolveConflictPath returns path or path with (n) suffix until free.
func (s *Service) ResolveConflictPath(ctx context.Context, userID, deviceID, desired string) (string, error) {
	canon, err := normalizePath(desired)
	if err != nil || canon == "" {
		return "", ErrBadPath
	}
	if _, err := s.Stat(ctx, userID, deviceID, canon); err != nil {
		return canon, nil
	}
	dir := path.Dir(canon)
	base := path.Base(canon)
	ext := path.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	for i := 1; i < 1000; i++ {
		candidate := path.Join(dir, fmt.Sprintf("%s (%d)%s", stem, i, ext))
		if dir == "." {
			candidate = fmt.Sprintf("%s (%d)%s", stem, i, ext)
		}
		if _, err := s.Stat(ctx, userID, deviceID, candidate); err != nil {
			return candidate, nil
		}
	}
	return "", ErrNameConflict
}
