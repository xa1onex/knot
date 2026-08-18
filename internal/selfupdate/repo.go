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
	ref, err := gitOne(ctx, sourceDir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		st.Error = err.Error()
		return st
	}
	latest, err := gitOne(ctx, sourceDir, "ls-remote", "origin", "refs/heads/"+ref)
	if err != nil {
		st.Error = err.Error()
		return st
	}
	parts := strings.Fields(latest)
	if len(parts) == 0 {
		st.Error = "empty remote HEAD"
		return st
	}
	st.CurrentRef = shortRef(cur)
	st.LatestRef = shortRef(parts[0])
	st.Available = strings.TrimSpace(cur) != strings.TrimSpace(parts[0])
	return st
}

func UpdateSource(ctx context.Context, sourceDir string) ([]string, error) {
	cmds := [][]string{
		{"fetch", "--prune", "origin"},
		{"reset", "--hard", "origin/HEAD"},
	}
	var logs []string
	for _, args := range cmds {
		out, err := gitRun(ctx, sourceDir, args...)
		if strings.TrimSpace(out) != "" {
			logs = append(logs, out)
		}
		if err != nil {
			return logs, err
		}
	}
	return logs, nil
}

func gitOne(ctx context.Context, dir string, args ...string) (string, error) {
	out, err := gitRun(ctx, dir, args...)
	return strings.TrimSpace(out), err
}

func gitRun(ctx context.Context, dir string, args ...string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "git", args...)
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
