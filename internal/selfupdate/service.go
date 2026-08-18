package selfupdate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/knot-infra/knot/internal/config"
	"github.com/knot-infra/knot/internal/store"
	"github.com/knot-infra/knot/pkg/protocol"
)

var (
	ErrUnavailable = errors.New("self-update unavailable")
	ErrOffline     = errors.New("device offline")
)

type Sender interface {
	SendJSON(deviceID string, v any) error
	IsOnline(deviceID string) bool
}

type Service struct {
	Store  *store.Store
	Sender Sender
	Cfg    config.Config

	mu           sync.Mutex
	pendingCheck map[string]chan protocol.UpdateCheckResult
	pendingApply map[string]chan protocol.UpdateApplyResult
}

type FleetStatus struct {
	ControlPlane protocol.UpdateComponentStatus   `json:"control_plane"`
	Devices      []DeviceUpdateStatus             `json:"devices"`
}

type DeviceUpdateStatus struct {
	DeviceID string                         `json:"device_id"`
	Name     string                         `json:"name"`
	Online   bool                           `json:"online"`
	Status   *protocol.UpdateComponentStatus `json:"status,omitempty"`
	Error    string                         `json:"error,omitempty"`
}

func New(st *store.Store, sender Sender, cfg config.Config) *Service {
	return &Service{
		Store: st, Sender: sender, Cfg: cfg,
		pendingCheck: map[string]chan protocol.UpdateCheckResult{},
		pendingApply: map[string]chan protocol.UpdateApplyResult{},
	}
}

func (s *Service) ControlPlaneStatus(ctx context.Context) protocol.UpdateComponentStatus {
	dataDir := filepath.Dir(s.Cfg.DatabasePath)
	sourceDir := InferSourceDir(dataDir)
	return SourceStatus(ctx, "knotd", sourceDir, "", s.canApplyControlPlane())
}

func (s *Service) Fleet(ctx context.Context, userID string) (*FleetStatus, error) {
	out := &FleetStatus{ControlPlane: s.ControlPlaneStatus(ctx)}
	if s.Store == nil {
		return out, nil
	}
	devs, err := s.Store.ListDevices(ctx, userID)
	if err != nil {
		return nil, err
	}
	for _, d := range devs {
		item := DeviceUpdateStatus{DeviceID: d.ID, Name: d.Name, Online: d.Online}
		if !d.Online || s.Sender == nil || !s.Sender.IsOnline(d.ID) {
			item.Error = "offline"
			out.Devices = append(out.Devices, item)
			continue
		}
		st, err := s.checkDevice(ctx, d.ID)
		if err != nil {
			item.Error = err.Error()
		} else {
			item.Status = &st
		}
		out.Devices = append(out.Devices, item)
	}
	return out, nil
}

func (s *Service) ApplyControlPlane(ctx context.Context, force bool) (protocol.UpdateComponentStatus, []string, error) {
	st := s.ControlPlaneStatus(ctx)
	if !st.CanApply {
		return st, nil, ErrUnavailable
	}
	if !st.Available && !force {
		return st, nil, nil
	}
	logs, err := UpdateSource(ctx, st.SourceDir)
	if err != nil {
		st.Error = err.Error()
		return st, logs, err
	}
	buildCmd := exec.CommandContext(ctx, "sudo", "/usr/local/lib/knot/update-control-plane.sh")
	var out []byte
	out, err = buildCmd.CombinedOutput()
	if strings.TrimSpace(string(out)) != "" {
		logs = append(logs, strings.TrimSpace(string(out)))
	}
	if err != nil {
		st.Error = strings.TrimSpace(string(out))
		if st.Error == "" {
			st.Error = err.Error()
		}
		return st, logs, err
	}
	st = s.ControlPlaneStatus(context.Background())
	return st, logs, nil
}

func (s *Service) ApplyDevice(ctx context.Context, deviceID string, force bool) (protocol.UpdateComponentStatus, []string, error) {
	res, err := s.applyDevice(ctx, deviceID, force)
	return res.Status, res.LogLines, err
}

func (s *Service) HandleAgentMessage(_ context.Context, _ string, envelopeType string, raw []byte) error {
	switch envelopeType {
	case protocol.TypeUpdateCheckResult:
		var res protocol.UpdateCheckResult
		if err := json.Unmarshal(raw, &res); err != nil {
			return err
		}
		s.mu.Lock()
		ch := s.pendingCheck[res.RequestID]
		s.mu.Unlock()
		if ch != nil {
			ch <- res
		}
	case protocol.TypeUpdateApplyResult:
		var res protocol.UpdateApplyResult
		if err := json.Unmarshal(raw, &res); err != nil {
			return err
		}
		s.mu.Lock()
		ch := s.pendingApply[res.RequestID]
		s.mu.Unlock()
		if ch != nil {
			ch <- res
		}
	}
	return nil
}

func (s *Service) checkDevice(ctx context.Context, deviceID string) (protocol.UpdateComponentStatus, error) {
	if s.Sender == nil || !s.Sender.IsOnline(deviceID) {
		return protocol.UpdateComponentStatus{}, ErrOffline
	}
	reqID := store.NewID()
	ch := make(chan protocol.UpdateCheckResult, 1)
	s.mu.Lock()
	s.pendingCheck[reqID] = ch
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.pendingCheck, reqID)
		s.mu.Unlock()
	}()
	if err := s.Sender.SendJSON(deviceID, protocol.UpdateCheck{Type: protocol.TypeUpdateCheck, RequestID: reqID}); err != nil {
		return protocol.UpdateComponentStatus{}, err
	}
	select {
	case <-ctx.Done():
		return protocol.UpdateComponentStatus{}, ctx.Err()
	case res := <-ch:
		if !res.OK && res.Error != "" {
			return res.Status, errors.New(res.Error)
		}
		return res.Status, nil
	case <-time.After(20 * time.Second):
		return protocol.UpdateComponentStatus{}, fmt.Errorf("device update check timeout")
	}
}

func (s *Service) applyDevice(ctx context.Context, deviceID string, force bool) (protocol.UpdateApplyResult, error) {
	if s.Sender == nil || !s.Sender.IsOnline(deviceID) {
		return protocol.UpdateApplyResult{}, ErrOffline
	}
	reqID := store.NewID()
	ch := make(chan protocol.UpdateApplyResult, 1)
	s.mu.Lock()
	s.pendingApply[reqID] = ch
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.pendingApply, reqID)
		s.mu.Unlock()
	}()
	if err := s.Sender.SendJSON(deviceID, protocol.UpdateApply{Type: protocol.TypeUpdateApply, RequestID: reqID, Force: force}); err != nil {
		return protocol.UpdateApplyResult{}, err
	}
	select {
	case <-ctx.Done():
		return protocol.UpdateApplyResult{}, ctx.Err()
	case res := <-ch:
		if !res.OK && res.Error != "" {
			return res, errors.New(res.Error)
		}
		return res, nil
	case <-time.After(5 * time.Minute):
		return protocol.UpdateApplyResult{}, fmt.Errorf("device update timeout")
	}
}

func (s *Service) canApplyControlPlane() bool {
	if v := strings.TrimSpace(os.Getenv("KNOT_UPDATE_HELPER")); v != "" {
		_, err := os.Stat(v)
		return err == nil
	}
	_, err := os.Stat("/usr/local/lib/knot/update-control-plane.sh")
	return err == nil && runtime.GOOS == "linux"
}
