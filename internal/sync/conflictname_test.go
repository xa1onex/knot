package syncjob

import "testing"

func TestConflictCopyRelPath(t *testing.T) {
	got := ConflictCopyRelPath("project/config.json", "Home PC", "2026-08-17T22:31:00Z")
	want := "project/config.conflict-home-pc-20260817-2231.json"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestUniqueConflictCopyRelPathNoOverwrite(t *testing.T) {
	taken := map[string]bool{
		"config.conflict-macbook-20260817-2234.json": true,
	}
	got := UniqueConflictCopyRelPath("config.json", "MacBook", "2026-08-17T22:34:00Z", func(rel string) bool {
		return taken[rel]
	})
	if got != "config.conflict-macbook-20260817-2234-2.json" {
		t.Fatalf("got %q", got)
	}
	taken[got] = true
	got2 := UniqueConflictCopyRelPath("config.json", "MacBook", "2026-08-17T22:34:00Z", func(rel string) bool {
		return taken[rel]
	})
	if got2 != "config.conflict-macbook-20260817-2234-3.json" {
		t.Fatalf("got %q", got2)
	}
}
