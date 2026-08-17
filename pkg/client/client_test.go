package client

import "testing"

func TestProgressFromTransfer(t *testing.T) {
	p := ProgressFromTransfer(&Transfer{ID: "t1", Status: TransferTransferring, Size: 100, BytesReceived: 25})
	if p.Percent != 25 || p.BytesReceived != 25 {
		t.Fatalf("%+v", p)
	}
	p = ProgressFromTransfer(&Transfer{ID: "t1", Status: TransferCompleted, Size: 100, BytesReceived: 0})
	if p.Percent != 100 || p.BytesReceived != 100 {
		t.Fatalf("%+v", p)
	}
}

func TestAPIErrorHelpers(t *testing.T) {
	err := &APIError{Status: 507, Code: "quota_exceeded", Message: "file size"}
	if !IsQuotaExceeded(err) {
		t.Fatal("expected quota")
	}
	if IsUnauthorized(err) {
		t.Fatal("not unauthorized")
	}
}
