// Package files is Stage 6.5 — a metadata index over Storage on every Node.
// Control Plane never copies file bytes; nodes remain the source of truth.
package files

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/knot-infra/knot/internal/storage"
	"github.com/knot-infra/knot/internal/store"
	"github.com/knot-infra/knot/pkg/protocol"
)

const maxIndexEntries = 100000

type OnlineChecker interface {
	IsOnline(deviceID string) bool
}

type Service struct {
	Store   *store.Store
	Storage *storage.Service
	Online  OnlineChecker

	mu     sync.Mutex
	timers map[string]*time.Timer
}

func New(st *store.Store, stor *storage.Service, online OnlineChecker) *Service {
	return &Service{
		Store:   st,
		Storage: stor,
		Online:  online,
		timers:  make(map[string]*time.Timer),
	}
}

// OnMutate implements storage.Service.OnMutate — fire-and-forget reindex.
func (s *Service) OnMutate(userID, deviceID string) {
	s.ScheduleReindex(userID, deviceID)
}

// ScheduleReindex coalesces reconnect/mutation bursts per device.
func (s *Service) ScheduleReindex(userID, deviceID string) {
	if s == nil || userID == "" || deviceID == "" {
		return
	}
	key := userID + "\x00" + deviceID
	s.mu.Lock()
	defer s.mu.Unlock()
	if t := s.timers[key]; t != nil {
		t.Stop()
	}
	s.timers[key] = time.AfterFunc(500*time.Millisecond, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if _, _, err := s.ReindexDevice(ctx, userID, deviceID); err != nil {
			log.Printf("file index %s: %v", deviceID, err)
		}
	})
}

type ReindexResult struct {
	DeviceIDs []string `json:"device_ids"`
	Entries   int      `json:"entries"`
	Skipped   []string `json:"skipped,omitempty"`
	Errors    []string `json:"errors,omitempty"`
}

// Reindex refreshes one device, or every non-revoked device when deviceID is empty.
func (s *Service) Reindex(ctx context.Context, userID, deviceID string) (*ReindexResult, error) {
	if deviceID != "" {
		n, skipped, err := s.ReindexDevice(ctx, userID, deviceID)
		if err != nil {
			return nil, err
		}
		out := &ReindexResult{DeviceIDs: []string{deviceID}, Entries: n}
		if skipped {
			out.Skipped = []string{deviceID}
			out.DeviceIDs = nil
		}
		return out, nil
	}
	devs, err := s.Store.ListDevices(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := &ReindexResult{}
	total := 0
	for i := range devs {
		d := &devs[i]
		if d.RevokedAt != nil {
			continue
		}
		n, skipped, err := s.ReindexDevice(ctx, userID, d.ID)
		if err != nil {
			out.Errors = append(out.Errors, d.ID+": "+err.Error())
			continue
		}
		if skipped {
			out.Skipped = append(out.Skipped, d.ID)
			continue
		}
		out.DeviceIDs = append(out.DeviceIDs, d.ID)
		total += n
	}
	out.Entries = total
	return out, nil
}

// ReindexDevice replaces the snapshot for one node from a live Storage.List walk.
// Offline / list failure leaves the previous snapshot (stale is OK).
func (s *Service) ReindexDevice(ctx context.Context, userID, deviceID string) (n int, skipped bool, err error) {
	if s.Online != nil && !s.Online.IsOnline(deviceID) {
		return 0, true, nil
	}
	prev, _ := s.Store.ListFileIndexForDevice(ctx, userID, deviceID)
	prevByPath := make(map[string]store.FileIndexRow, len(prev))
	for i := range prev {
		prevByPath[prev[i].Path] = prev[i]
	}
	ents, err := s.walk(ctx, userID, deviceID)
	if err != nil {
		if errors.Is(err, storage.ErrDeviceOffline) {
			return 0, true, nil
		}
		return 0, false, err
	}
	rows := make([]store.FileIndexRow, 0, len(ents))
	for i := range ents {
		e := &ents[i]
		r := store.FileIndexRow{
			UserID:      userID,
			DeviceID:    deviceID,
			Path:        e.Path,
			Name:        e.Name,
			Size:        e.Size,
			Mtime:       e.Mtime,
			SHA256:      e.SHA256,
			MimeType:    e.MimeType,
			IsDirectory: e.IsDir,
			FileID:      e.FileID,
		}
		if old, ok := prevByPath[e.Path]; ok && old.Size == e.Size && old.Mtime == e.Mtime {
			if r.SHA256 == "" {
				r.SHA256 = old.SHA256
			}
			if r.FileID == "" {
				r.FileID = old.FileID
			}
		}
		rows = append(rows, r)
	}
	if err := s.Store.ReplaceFileIndexForDevice(ctx, userID, deviceID, rows); err != nil {
		return 0, false, err
	}
	return len(rows), false, nil
}

func (s *Service) Search(ctx context.Context, userID string, q store.FileSearchQuery) ([]store.FileIndexRow, error) {
	return s.Store.SearchFileIndex(ctx, userID, q)
}

func (s *Service) walk(ctx context.Context, userID, deviceID string) ([]protocol.StorageEntry, error) {
	var out []protocol.StorageEntry
	var rec func(rel string) error
	rec = func(rel string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if len(out) >= maxIndexEntries {
			return nil
		}
		ents, err := s.Storage.List(ctx, userID, deviceID, rel)
		if err != nil {
			if rel == "" {
				return err
			}
			return nil
		}
		for _, e := range ents {
			out = append(out, e)
			if e.IsDir {
				p := e.Path
				if p == "" {
					p = e.Name
				}
				if err := rec(p); err != nil {
					return err
				}
			}
			if len(out) >= maxIndexEntries {
				return nil
			}
		}
		return nil
	}
	if err := rec(""); err != nil {
		return nil, err
	}
	return out, nil
}
