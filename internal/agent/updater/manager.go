package updater

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/knot-infra/knot/internal/selfupdate"
	"github.com/knot-infra/knot/pkg/protocol"
)

type Manager struct {
	Send     func(v any) error
	DataDir  string
	SourceDir string
}

func NewManager(dataDir string, send func(v any) error) *Manager {
	src := selfupdate.InferSourceDir(dataDir)
	return &Manager{Send: send, DataDir: dataDir, SourceDir: src}
}

func (m *Manager) Handle(raw []byte) {
	var env struct{ Type string `json:"type"` }
	if json.Unmarshal(raw, &env) != nil {
		return
	}
	switch env.Type {
	case protocol.TypeUpdateCheck:
		var req protocol.UpdateCheck
		if json.Unmarshal(raw, &req) == nil {
			go m.check(req)
		}
	case protocol.TypeUpdateApply:
		var req protocol.UpdateApply
		if json.Unmarshal(raw, &req) == nil {
			go m.apply(req)
		}
	}
}

func (m *Manager) check(req protocol.UpdateCheck) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	st := selfupdateStatus(ctx, m.SourceDir)
	_ = m.Send(protocol.UpdateCheckResult{
		Type: protocol.TypeUpdateCheckResult, RequestID: req.RequestID,
		OK: st.Error == "", Status: st, Error: st.Error,
	})
}

func (m *Manager) apply(req protocol.UpdateApply) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	st := selfupdateStatus(ctx, m.SourceDir)
	res := protocol.UpdateApplyResult{
		Type: protocol.TypeUpdateApplyResult, RequestID: req.RequestID, Status: st,
	}
	if st.Error != "" {
		res.Error = st.Error
		_ = m.Send(res)
		return
	}
	if !st.Available && !req.Force {
		res.OK = true
		_ = m.Send(res)
		return
	}
	logs, err := selfupdate.UpdateSource(ctx, m.SourceDir)
	if err == nil {
		err = buildAgent(ctx, m.SourceDir)
	}
	if err == nil {
		go scheduleRestart()
	}
	st = selfupdateStatus(context.Background(), m.SourceDir)
	res.Status = st
	res.LogLines = logs
	res.OK = err == nil
	if err != nil {
		res.Error = err.Error()
	}
	_ = m.Send(res)
}

func buildAgent(ctx context.Context, sourceDir string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	tmp := exe + ".new"
	goBin := "go"
	if _, err := exec.LookPath(goBin); err != nil {
		home, _ := os.UserHomeDir()
		for _, candidate := range []string{
			filepath.Join(home, ".local", "go", "bin", "go"),
			"/usr/local/go/bin/go",
		} {
			if _, statErr := os.Stat(candidate); statErr == nil {
				goBin = candidate
				break
			}
		}
	}
	cmd := exec.CommandContext(ctx, goBin, "build", "-buildvcs=false", "-o", tmp, "./cmd/knot-agent")
	cmd.Dir = sourceDir
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOFLAGS=-buildvcs=false")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("go build: %s", strings.TrimSpace(string(out)))
	}
	if err := os.Rename(tmp, exe); err != nil {
		return err
	}
	return nil
}

func scheduleRestart() {
	time.Sleep(1 * time.Second)
	if runtime.GOOS == "darwin" {
		if u, err := user.Current(); err == nil {
			if uid, err := strconv.Atoi(u.Uid); err == nil {
				_ = exec.Command("launchctl", "kickstart", "-k", fmt.Sprintf("gui/%d/com.node.knot-agent", uid)).Run()
				return
			}
		}
	}
	_ = exec.Command("sh", "-c", "systemctl restart knot-agent || true").Run()
}

func selfupdateStatus(ctx context.Context, sourceDir string) protocol.UpdateComponentStatus {
	return selfupdate.SourceStatus(ctx, "knot-agent", sourceDir, protocol.AgentVersion, true)
}

