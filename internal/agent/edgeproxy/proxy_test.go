package edgeproxy

import "testing"

func TestValidatePath(t *testing.T) {
	if err := validatePath("/ok"); err != nil {
		t.Fatal(err)
	}
	if err := validatePath("//evil.example/"); err == nil {
		t.Fatal("expected scheme-relative reject")
	}
	if err := validatePath("http://192.168.1.50/"); err == nil {
		t.Fatal("expected reject")
	}
}
