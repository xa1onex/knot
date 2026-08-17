package client

import (
	"context"
	"net/http"
)

type ComputeCPU struct {
	Cores        int      `json:"cores"`
	Architecture string   `json:"architecture"`
	UsagePercent *float64 `json:"usage_percent,omitempty"`
}

type ComputeMemory struct {
	TotalBytes     uint64   `json:"total_bytes"`
	AvailableBytes uint64   `json:"available_bytes"`
	UsedBytes      uint64   `json:"used_bytes"`
	UsagePercent   *float64 `json:"usage_percent,omitempty"`
}

type ComputeGPU struct {
	Vendor    string  `json:"vendor"`
	Model     string  `json:"model"`
	VRAMBytes *uint64 `json:"vram_bytes"`
	Available *bool   `json:"available,omitempty"`
}

type ComputeDisk struct {
	Mount      string `json:"mount"`
	Name       string `json:"name,omitempty"`
	FSType     string `json:"fstype,omitempty"`
	TotalBytes uint64 `json:"total_bytes"`
	FreeBytes  uint64 `json:"free_bytes"`
	UsedBytes  uint64 `json:"used_bytes"`
}

type ComputeDevice struct {
	DeviceID        string            `json:"device_id"`
	Name            string            `json:"name"`
	Hostname        string            `json:"hostname"`
	OS              string            `json:"os"`
	Arch            string            `json:"arch"`
	AgentVersion    string            `json:"agent_version"`
	Online          bool              `json:"online"`
	Status          string            `json:"status"`
	LastSeenAt      *string           `json:"last_seen_at,omitempty"`
	LastTelemetryAt *string           `json:"last_telemetry_at,omitempty"`
	CPU             *ComputeCPU       `json:"cpu"`
	Memory          *ComputeMemory    `json:"memory"`
	GPU             *[]ComputeGPU     `json:"gpu"`
	Disks           []ComputeDisk     `json:"disks"`
	Labels          map[string]string `json:"labels,omitempty"`
}

func (c *Client) ListComputeDevices(ctx context.Context) ([]ComputeDevice, error) {
	var out struct {
		Devices []ComputeDevice `json:"devices"`
	}
	if err := c.do(ctx, http.MethodGet, "/v1/compute/devices", nil, &out, true); err != nil {
		return nil, err
	}
	return out.Devices, nil
}

func (c *Client) GetComputeDevice(ctx context.Context, deviceID string) (*ComputeDevice, error) {
	var out ComputeDevice
	if err := c.do(ctx, http.MethodGet, "/v1/compute/devices/"+deviceID, nil, &out, true); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) SetComputeLabels(ctx context.Context, deviceID string, labels map[string]string) (*ComputeDevice, error) {
	var out ComputeDevice
	if err := c.do(ctx, http.MethodPut, "/v1/compute/devices/"+deviceID+"/labels", map[string]any{"labels": labels}, &out, true); err != nil {
		return nil, err
	}
	return &out, nil
}
