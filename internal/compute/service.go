package compute

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/knot-infra/knot/internal/store"
	"github.com/knot-infra/knot/pkg/protocol"
)

const DefaultFreshFor = 90 * time.Second

type Record struct {
	DeviceID        string                  `json:"device_id"`
	Name            string                  `json:"name"`
	Hostname        string                  `json:"hostname"`
	OS              string                  `json:"os"`
	Arch            string                  `json:"arch"`
	AgentVersion    string                  `json:"agent_version"`
	Online          bool                    `json:"online"`
	Status          string                  `json:"status"`
	LastSeenAt      *time.Time              `json:"last_seen_at,omitempty"`
	LastTelemetryAt *time.Time              `json:"last_telemetry_at,omitempty"`
	CPU             *protocol.ComputeCPU    `json:"cpu"`
	Memory          *protocol.ComputeMemory `json:"memory"`
	GPU             *[]protocol.ComputeGPU  `json:"gpu"`
	Disks           []protocol.ComputeDisk  `json:"disks"`
	Labels          map[string]string       `json:"labels"`
}

func Status(online bool, collectedAt *time.Time, freshFor time.Duration) string {
	if !online {
		return protocol.ComputeStatusOffline
	}
	if collectedAt == nil || collectedAt.IsZero() {
		return protocol.ComputeStatusStale
	}
	if freshFor <= 0 {
		freshFor = DefaultFreshFor
	}
	if time.Since(collectedAt.UTC()) > freshFor {
		return protocol.ComputeStatusStale
	}
	return protocol.ComputeStatusAvailable
}

func FreshFor(heartbeatTimeout time.Duration) time.Duration {
	d := heartbeatTimeout * 2
	if d < DefaultFreshFor {
		return DefaultFreshFor
	}
	return d
}

func Build(d store.Device, snap *store.DeviceCompute, freshFor time.Duration) Record {
	rec := Record{
		DeviceID:     d.ID,
		Name:         d.Name,
		Hostname:     d.Hostname,
		OS:           d.OS,
		Arch:         d.Arch,
		AgentVersion: d.AgentVersion,
		Online:       d.Online,
		LastSeenAt:   d.LastSeenAt,
		Disks:        []protocol.ComputeDisk{},
	}
	var collected *time.Time
	if snap != nil && snap.SnapshotJSON != "" {
		collected = &snap.CollectedAt
		rec.LastTelemetryAt = &snap.CollectedAt
		var inv protocol.ComputeInventory
		if json.Unmarshal([]byte(snap.SnapshotJSON), &inv) == nil {
			cpu := inv.CPU
			mem := inv.Memory
			rec.CPU = &cpu
			rec.Memory = &mem
			rec.GPU = inv.GPU
			if inv.Disks != nil {
				rec.Disks = inv.Disks
			}
		}
	}
	rec.Status = Status(d.Online, collected, freshFor)
	rec.Labels = LabelsFor(rec, nil)
	return rec
}

type Service struct {
	Store    *store.Store
	FreshFor time.Duration
}

func New(st *store.Store, freshFor time.Duration) *Service {
	return &Service{Store: st, FreshFor: FreshFor(freshFor)}
}

func (s *Service) List(ctx context.Context, userID string) ([]Record, error) {
	devs, err := s.Store.ListDevices(ctx, userID)
	if err != nil {
		return nil, err
	}
	snaps, err := s.Store.ListDeviceCompute(ctx, userID)
	if err != nil {
		return nil, err
	}
	labelRaw, err := s.Store.ListDeviceLabels(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]Record, 0, len(devs))
	for _, d := range devs {
		if d.RevokedAt != nil {
			continue
		}
		var snap *store.DeviceCompute
		if c, ok := snaps[d.ID]; ok {
			cp := c
			snap = &cp
		}
		rec := Build(d, snap, s.FreshFor)
		rec.Labels = LabelsFor(rec, parseLabels(labelRaw[d.ID]))
		out = append(out, rec)
	}
	return out, nil
}

func (s *Service) Get(ctx context.Context, userID, deviceID string) (*Record, error) {
	d, err := s.Store.GetDevice(ctx, userID, deviceID)
	if err != nil {
		return nil, err
	}
	snap, err := s.Store.GetDeviceCompute(ctx, deviceID)
	if err != nil && !store.IsNotFound(err) {
		return nil, err
	}
	rec := Build(*d, snap, s.FreshFor)
	raw, err := s.Store.GetDeviceLabels(ctx, deviceID)
	if err != nil {
		return nil, err
	}
	rec.Labels = LabelsFor(rec, parseLabels(raw))
	return &rec, nil
}

func (s *Service) SetLabels(ctx context.Context, userID, deviceID string, labels map[string]string) (*Record, error) {
	if _, err := s.Store.GetDevice(ctx, userID, deviceID); err != nil {
		return nil, err
	}
	if labels == nil {
		labels = map[string]string{}
	}
	if err := ValidateUserLabels(labels); err != nil {
		return nil, err
	}
	b, err := json.Marshal(labels)
	if err != nil {
		return nil, err
	}
	if err := s.Store.UpsertDeviceLabels(ctx, deviceID, string(b)); err != nil {
		return nil, err
	}
	return s.Get(ctx, userID, deviceID)
}

func parseLabels(raw string) map[string]string {
	out := map[string]string{}
	if strings.TrimSpace(raw) == "" || raw == "{}" {
		return out
	}
	_ = json.Unmarshal([]byte(raw), &out)
	if out == nil {
		return map[string]string{}
	}
	return out
}
