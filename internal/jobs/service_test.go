package jobs

import (
	"strings"
	"testing"
)

func TestValidateRejectsShell(t *testing.T) {
	req := CreateRequest{DeviceID: "d", Image: "python:3.13", Command: []string{"bash", "-c", "rm -rf /"}}
	if err := normalizeAndValidate(&req); err == nil || !strings.Contains(err.Error(), "shell") {
		t.Fatalf("expected shell reject, got %v", err)
	}
}

func TestValidateAllowsScheduledWithoutDevice(t *testing.T) {
	req := CreateRequest{Image: "python:3.13"}
	if err := normalizeAndValidate(&req); err != nil {
		t.Fatal(err)
	}
}

func TestValidateDefaults(t *testing.T) {
	req := CreateRequest{Image: "Python:3.13"}
	if err := normalizeAndValidate(&req); err != nil {
		t.Fatal(err)
	}
	if req.CPU != 1 || req.MemoryMB != 512 || req.TimeoutSeconds != 300 || req.DiskMB != 1024 || req.Pids != 256 {
		t.Fatalf("%+v", req)
	}
	if req.Image != "python:3.13" {
		t.Fatalf("image %s", req.Image)
	}
}
