package protocol

import "testing"

func TestVersionOlder(t *testing.T) {
	if VersionOlder("0.7.6", "0.7.6") {
		t.Fatal("equal is not older")
	}
	if !VersionOlder("0.7.5", "0.7.6") {
		t.Fatal("0.7.5 should be older")
	}
	if VersionOlder("0.8.0", "0.7.6") {
		t.Fatal("0.8.0 should not be older")
	}
	if !VersionOlder("", "0.7.6") {
		t.Fatal("empty have is older")
	}
	if VersionOlder("0.7.6", "") {
		t.Fatal("empty min is not a floor")
	}
}
