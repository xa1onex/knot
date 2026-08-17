package inventory

import (
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/knot-infra/knot/pkg/protocol"
)

func readGPUs() *[]protocol.ComputeGPU {
	if gpus, ok := nvidiaSMI(); ok {
		return &gpus
	}
	switch runtime.GOOS {
	case "darwin":
		if gpus, ok := darwinGPUs(); ok {
			return &gpus
		}
	case "windows":
		if gpus, ok := windowsGPUs(); ok {
			return &gpus
		}
	case "linux":
		if gpus, ok := lspciGPUs(); ok {
			return &gpus
		}
	}
	return nil
}

func nvidiaSMI() ([]protocol.ComputeGPU, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "nvidia-smi",
		"--query-gpu=name,memory.total",
		"--format=csv,noheader,nounits").Output()
	if err != nil {
		return nil, false
	}
	return parseNvidiaSMI(out)
}

func parseNvidiaSMI(out []byte) ([]protocol.ComputeGPU, bool) {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var gpus []protocol.ComputeGPU
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		name, vramStr, _ := strings.Cut(line, ",")
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		g := protocol.ComputeGPU{
			Vendor:    nvidiaVendor(name),
			Model:     name,
			Available: boolPtr(true),
		}
		if mb, err := strconv.ParseUint(strings.TrimSpace(vramStr), 10, 64); err == nil && mb > 0 {
			g.VRAMBytes = uintPtr(mb * 1024 * 1024)
		}
		gpus = append(gpus, g)
	}
	if len(gpus) == 0 {
		return nil, false
	}
	return gpus, true
}

func nvidiaVendor(name string) string {
	n := strings.ToLower(name)
	if strings.Contains(n, "nvidia") || strings.Contains(n, "geforce") || strings.Contains(n, "rtx") || strings.Contains(n, "quadro") || strings.Contains(n, "tesla") {
		return "NVIDIA"
	}
	return "NVIDIA"
}

func darwinGPUs() ([]protocol.ComputeGPU, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "system_profiler", "SPDisplaysDataType", "-json").Output()
	if err != nil {
		return nil, false
	}
	return parseDarwinGPUs(out)
}

func parseDarwinGPUs(out []byte) ([]protocol.ComputeGPU, bool) {
	var payload struct {
		Displays []map[string]any `json:"SPDisplaysDataType"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		return nil, false
	}
	var gpus []protocol.ComputeGPU
	for _, d := range payload.Displays {
		model := firstString(d, "sppci_model", "spdisplays_device-id", "_name")
		if model == "" {
			continue
		}
		vendor := firstString(d, "spdisplays_vendor", "sppci_vendor")
		vendor = cleanAppleVendor(vendor, model)
		g := protocol.ComputeGPU{Vendor: vendor, Model: model, Available: boolPtr(true)}
		if vram := parseVRAM(firstString(d, "spdisplays_vram", "spdisplays_vram_shared", "sppci_vram")); vram != nil {
			g.VRAMBytes = vram
		}
		gpus = append(gpus, g)
	}
	if len(gpus) == 0 {
		return nil, false
	}
	return gpus, true
}

func cleanAppleVendor(vendor, model string) string {
	v := strings.TrimSpace(vendor)
	v = strings.TrimPrefix(v, "sppci_vendor_")
	if v == "" || strings.EqualFold(v, "Apple") || strings.Contains(strings.ToLower(model), "apple") {
		if strings.Contains(strings.ToLower(model), "apple") || strings.Contains(strings.ToLower(v), "apple") {
			return "Apple"
		}
	}
	if v == "" {
		return "unknown"
	}
	return v
}

func parseVRAM(s string) *uint64 {
	s = strings.TrimSpace(s)
	if s == "" || strings.EqualFold(s, "shared") {
		return nil
	}
	s = strings.ReplaceAll(s, ",", "")
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return nil
	}
	n, err := strconv.ParseFloat(fields[0], 64)
	if err != nil || n <= 0 {
		return nil
	}
	mult := uint64(1)
	unit := ""
	if len(fields) > 1 {
		unit = strings.ToLower(fields[1])
	} else {
		unit = strings.ToLower(s)
	}
	switch {
	case strings.Contains(unit, "gb"):
		mult = 1 << 30
	case strings.Contains(unit, "mb"):
		mult = 1 << 20
	case strings.Contains(unit, "kb"):
		mult = 1 << 10
	default:
		return nil
	}
	v := uint64(n * float64(mult))
	return &v
}

func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

func windowsGPUs() ([]protocol.ComputeGPU, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command",
		"Get-CimInstance Win32_VideoController | Select-Object Name, AdapterCompatibility, AdapterRAM | ConvertTo-Json -Compress")
	out, err := cmd.Output()
	if err != nil {
		return nil, false
	}
	return parseWindowsGPUs(out)
}

func parseWindowsGPUs(out []byte) ([]protocol.ComputeGPU, bool) {
	out = bytes.TrimSpace(out)
	if len(out) == 0 {
		return nil, false
	}
	var rows []map[string]any
	if out[0] == '{' {
		var one map[string]any
		if err := json.Unmarshal(out, &one); err != nil {
			return nil, false
		}
		rows = []map[string]any{one}
	} else {
		if err := json.Unmarshal(out, &rows); err != nil {
			return nil, false
		}
	}
	var gpus []protocol.ComputeGPU
	for _, r := range rows {
		name, _ := r["Name"].(string)
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		vendor, _ := r["AdapterCompatibility"].(string)
		if vendor == "" {
			vendor = gpuVendorFromName(name)
		}
		g := protocol.ComputeGPU{Vendor: vendor, Model: name, Available: boolPtr(true)}
		if ram := jsonNumber(r["AdapterRAM"]); ram >= 128<<20 {
			g.VRAMBytes = uintPtr(uint64(ram))
		}
		gpus = append(gpus, g)
	}
	if len(gpus) == 0 {
		return nil, false
	}
	return gpus, true
}

func jsonNumber(v any) int64 {
	switch t := v.(type) {
	case float64:
		return int64(t)
	case json.Number:
		n, _ := t.Int64()
		return n
	default:
		return 0
	}
}

func gpuVendorFromName(name string) string {
	n := strings.ToLower(name)
	switch {
	case strings.Contains(n, "nvidia") || strings.Contains(n, "geforce") || strings.Contains(n, "rtx"):
		return "NVIDIA"
	case strings.Contains(n, "amd") || strings.Contains(n, "radeon"):
		return "AMD"
	case strings.Contains(n, "intel"):
		return "Intel"
	case strings.Contains(n, "apple"):
		return "Apple"
	default:
		return "unknown"
	}
}

func lspciGPUs() ([]protocol.ComputeGPU, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "lspci", "-mm").Output()
	if err != nil {
		return nil, false
	}
	return parseLspci(out)
}

func parseLspci(out []byte) ([]protocol.ComputeGPU, bool) {
	var gpus []protocol.ComputeGPU
	for _, line := range strings.Split(string(out), "\n") {
		l := strings.ToLower(line)
		if !strings.Contains(l, "vga") && !strings.Contains(l, "3d controller") && !strings.Contains(l, "display controller") {
			continue
		}
		model := strings.TrimSpace(line)
		if model == "" {
			continue
		}
		gpus = append(gpus, protocol.ComputeGPU{
			Vendor:    gpuVendorFromName(line),
			Model:     model,
			VRAMBytes: nil,
			Available: boolPtr(true),
		})
	}
	if len(gpus) == 0 {
		return nil, false
	}
	return gpus, true
}
