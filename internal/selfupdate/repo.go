package selfupdate

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/knot-infra/knot/pkg/protocol"
)

func InferSourceDir(dataDir string) string {
	if v := strings.TrimSpace(os.Getenv("KNOT_SRC_DIR")); v != "" {
		return v
	}
	if dataDir != "" {
		return filepath.Join(filepath.Clean(dataDir), "src")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".knot", "src")
}

func SourceStatus(ctx context.Context, component, sourceDir, build string, canApply bool) protocol.UpdateComponentStatus {
	st := protocol.UpdateComponentStatus{
		Component:    component,
		CanApply:     canApply,
		SourceDir:    sourceDir,
		CurrentBuild: build,
	}
	if sourceDir == "" {
		st.Error = "source dir unknown"
		return st
	}
	if _, err := os.Stat(filepath.Join(sourceDir, ".git")); err != nil {
		st.Error = "source repo not found"
		return st
	}
	cur, err := gitOne(ctx, sourceDir, "rev-parse", "HEAD")
	if err != nil {
		st.Error = err.Error()
		return st
	}
	st.CurrentRef = shortRef(cur)
	latest, err := remoteLatestSHA(ctx, sourceDir)
	if err != nil {
		st.Error = err.Error()
		return st
	}
	st.LatestRef = shortRef(latest)
	st.Available = strings.TrimSpace(cur) != strings.TrimSpace(latest)
	return st
}

func UpdateSource(ctx context.Context, sourceDir string) ([]string, error) {
	var logs []string
	out, err := gitRun(ctx, sourceDir, "fetch", "--prune", "origin")
	if strings.TrimSpace(out) != "" {
		logs = append(logs, out)
	}
	if err != nil {
		return logs, err
	}
	_, _ = gitRun(ctx, sourceDir, "remote", "set-head", "origin", "-a")
	tracking := remoteTrackingRef(ctx, sourceDir)
	branch := strings.TrimPrefix(tracking, "origin/")
	out, err = gitRun(ctx, sourceDir, "checkout", "-B", branch, tracking)
	if strings.TrimSpace(out) != "" {
		logs = append(logs, out)
	}
	if err != nil {
		return logs, err
	}
	return logs, nil
}

func remoteLatestSHA(ctx context.Context, dir string) (string, error) {
	var specs []string
	if ref, err := gitOne(ctx, dir, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		if ref != "" && ref != "HEAD" {
			specs = append(specs, "refs/heads/"+ref)
		}
	}
	specs = append(specs, "HEAD", "refs/heads/main", "refs/heads/master")
	for _, spec := range specs {
		latest, err := gitOne(ctx, dir, "ls-remote", "origin", spec)
		if err != nil {
			return "", err
		}
		parts := strings.Fields(latest)
		if len(parts) > 0 {
			return parts[0], nil
		}
	}
	return "", fmt.Errorf("empty remote HEAD")
}

func remoteTrackingRef(ctx context.Context, dir string) string {
	ref, err := gitOne(ctx, dir, "symbolic-ref", "--short", "refs/remotes/origin/HEAD")
	if err == nil && strings.HasPrefix(ref, "origin/") {
		return ref
	}
	return "origin/main"
}

func gitOne(ctx context.Context, dir string, args ...string) (string, error) {
	out, err := gitRun(ctx, dir, args...)
	return strings.TrimSpace(out), err
}

func gitRun(ctx context.Context, dir string, args ...string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	full := append([]string{"-c", "safe.directory=" + dir}, args...)
	cmd := exec.CommandContext(cctx, "git", full...)
	cmd.Dir = dir
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(buf.String()))
	}
	return strings.TrimSpace(buf.String()), nil
}

func shortRef(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 12 {
		return s[:12]
	}
	return s
}
