package pathsafe_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/knot-infra/knot/internal/storage/pathsafe"
)

func TestCanonicalRelOK(t *testing.T) {
	cases := []struct{ in, want string }{
		{"photos/cat.jpg", "photos/cat.jpg"},
		{"photos\\cat.jpg", "photos/cat.jpg"},
		{"./photos/cat.jpg", "photos/cat.jpg"},
		{"shared", "shared"},
		{"", ""}, // empty is ErrEmpty — tested below
	}
	for _, c := range cases {
		if c.in == "" {
			continue
		}
		got, err := pathsafe.CanonicalRel(c.in)
		if err != nil {
			t.Fatalf("%q: %v", c.in, err)
		}
		if got != c.want {
			t.Fatalf("%q: got %q want %q", c.in, got, c.want)
		}
	}
	if _, err := pathsafe.CanonicalRel(""); err != pathsafe.ErrEmpty {
		t.Fatalf("empty: %v", err)
	}
}

func TestCanonicalRelEscape(t *testing.T) {
	bads := []string{
		"../Windows/System32",
		"..\\Windows\\System32",
		"/etc/passwd",
		"//evil/share",
		"C:/Windows/System32",
		"photos/../../etc/passwd",
		"photos/foo/../../../etc",
	}
	for _, b := range bads {
		if _, err := pathsafe.CanonicalRel(b); err == nil {
			t.Fatalf("expected reject %q", b)
		}
	}
}

func TestResolveUnderRoot(t *testing.T) {
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, "photos"), 0o700)
	full, err := pathsafe.ResolveUnderRoot(root, "photos/cat.jpg")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "photos", "cat.jpg")
	if full != want {
		t.Fatalf("got %q want %q", full, want)
	}
	if _, err := pathsafe.ResolveUnderRoot(root, "../outside"); err == nil {
		t.Fatal("expected escape")
	}
}

func TestResolveRejectsSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink privileges vary on windows CI")
	}
	root := t.TempDir()
	outside := t.TempDir()
	_ = os.WriteFile(filepath.Join(outside, "secret"), []byte("x"), 0o600)
	link := filepath.Join(root, "leak")
	if err := os.Symlink(outside, link); err != nil {
		t.Skip(err)
	}
	if _, err := pathsafe.ResolveUnderRoot(root, "leak/secret"); err == nil {
		t.Fatal("expected symlink escape reject")
	}
}
