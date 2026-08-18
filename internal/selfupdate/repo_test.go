package selfupdate

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestSourceStatusDetachedHEAD(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	clone := filepath.Join(root, "clone")
	run(t, root, "git", "init", "--bare", origin)
	run(t, origin, "git", "symbolic-ref", "HEAD", "refs/heads/main")
	work := filepath.Join(root, "seed")
	run(t, root, "git", "clone", origin, work)
	if err := os.WriteFile(filepath.Join(work, "README"), []byte("node"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, work, "git", "add", ".")
	run(t, work, "git", "-c", "user.email=test@node.local", "-c", "user.name=test", "commit", "-m", "init")
	run(t, work, "git", "push", "-u", "origin", "HEAD:main")
	run(t, root, "git", "clone", origin, clone)
	run(t, clone, "git", "checkout", "--detach")

	st := SourceStatus(ctx, "knotd", clone, "", true)
	if st.Error != "" {
		t.Fatalf("status error: %s", st.Error)
	}
	if st.CurrentRef == "" || st.LatestRef == "" {
		t.Fatalf("expected refs, got current=%q latest=%q", st.CurrentRef, st.LatestRef)
	}
	if st.Available {
		t.Fatalf("expected up to date, current=%s latest=%s", st.CurrentRef, st.LatestRef)
	}
}

func run(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}
