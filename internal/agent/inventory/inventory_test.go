package inventory

import (
	"encoding/json"
	"testing"
)

func TestParseNvidiaSMI(t *testing.T) {
	gpus, ok := parseNvidiaSMI([]byte("NVIDIA GeForce RTX 4090, 24564\n"))
	if !ok || len(gpus) != 1 {
		t.Fatalf("got %+v ok=%v", gpus, ok)
	}
	if gpus[0].Vendor != "NVIDIA" || gpus[0].Model != "NVIDIA GeForce RTX 4090" {
		t.Fatalf("%+v", gpus[0])
	}
	if gpus[0].VRAMBytes == nil || *gpus[0].VRAMBytes != 24564*1024*1024 {
		t.Fatalf("vram %+v", gpus[0].VRAMBytes)
	}
}

func TestParseNvidiaSMIEmptyIsUnknown(t *testing.T) {
	if _, ok := parseNvidiaSMI([]byte("\n")); ok {
		t.Fatal("empty output must not claim zero GPUs")
	}
}

func TestParseVRAMSharedIsNull(t *testing.T) {
	if parseVRAM("shared") != nil {
		t.Fatal("shared VRAM must be null")
	}
	if parseVRAM("") != nil {
		t.Fatal("empty VRAM must be null")
	}
	got := parseVRAM("8 GB")
	if got == nil || *got != 8<<30 {
		t.Fatalf("got %v", got)
	}
}

func TestSkipVirtualMounts(t *testing.T) {
	if !skipMount("/proc", "proc", 1<<30) {
		t.Fatal("proc")
	}
	if skipMount("/", "apfs", 500<<30) {
		t.Fatal("root apfs should be kept")
	}
	if !skipMount("/tiny", "ext4", 8<<20) {
		t.Fatal("tiny volume")
	}
}

func TestGPUJSONNull(t *testing.T) {
	inv := Collect()
	b, err := json.Marshal(inv)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	if inv.CPU.Cores < 1 {
		t.Fatal("cores")
	}
	if inv.Memory.TotalBytes == 0 {
		t.Fatal("memory total")
	}
	if len(inv.Disks) == 0 {
		t.Fatal("expected at least one disk")
	}
	if _, ok := raw["gpu"]; !ok {
		t.Fatal("gpu key required (null or array)")
	}
}
