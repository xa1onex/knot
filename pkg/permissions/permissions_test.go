package permissions_test

import (
	"testing"

	"github.com/knot-infra/knot/pkg/permissions"
)

func TestCheck(t *testing.T) {
	if !permissions.Check([]string{permissions.DevicesRead}, permissions.DevicesRead) {
		t.Fatal("expected devices.read")
	}
	if permissions.Check([]string{permissions.DevicesRead}, permissions.DevicesWrite) {
		t.Fatal("should deny write")
	}
	if !permissions.Check([]string{permissions.AccountAdmin}, permissions.DevicesWrite) {
		t.Fatal("admin implies write")
	}
	if permissions.Check([]string{permissions.AccountAdmin}, permissions.ShellExecute) {
		t.Fatal("shell must not be implied by admin")
	}
	if !permissions.Check([]string{permissions.ShellExecute}, permissions.ShellExecute) {
		t.Fatal("explicit shell should pass")
	}
	if permissions.Check([]string{permissions.AccountAdmin}, permissions.DeployWrite) {
		t.Fatal("deploy must not be implied by admin")
	}
	if permissions.Check([]string{permissions.AccountAdmin}, permissions.SecretsRead) {
		t.Fatal("secrets.read must not be implied by admin")
	}
	if permissions.Check([]string{permissions.AccountAdmin}, permissions.SecretsWrite) {
		t.Fatal("secrets.write must not be implied by admin")
	}
	if !permissions.Check([]string{permissions.SecretsWrite}, permissions.SecretsWrite) {
		t.Fatal("explicit secrets.write should pass")
	}
	if !permissions.Check([]string{permissions.AccountAdmin}, permissions.ComputeRead) {
		t.Fatal("admin implies compute.read")
	}
	if permissions.Check([]string{permissions.AccountAdmin}, permissions.ComputeWrite) {
		t.Fatal("compute.write must not be implied by admin")
	}
	if !permissions.Check([]string{permissions.ComputeWrite}, permissions.ComputeWrite) {
		t.Fatal("explicit compute.write should pass")
	}
	if !permissions.Check([]string{permissions.DeployWrite}, permissions.DeployWrite) {
		t.Fatal("explicit deploy.write should pass")
	}
	if permissions.Check([]string{permissions.AccountAdmin}, permissions.SourceWrite) {
		t.Fatal("source.write must not be implied by admin")
	}
	if permissions.Check([]string{permissions.AccountAdmin}, permissions.BuildWrite) {
		t.Fatal("build.write must not be implied by admin")
	}
	if permissions.Check([]string{permissions.BuildWrite}, permissions.DeployWrite) {
		t.Fatal("build.write must not imply deploy.write")
	}
	if permissions.Check([]string{permissions.DeployWrite}, permissions.BuildWrite) {
		t.Fatal("deploy.write must not imply build.write")
	}
	if !permissions.Check([]string{permissions.BuildWrite}, permissions.BuildWrite) {
		t.Fatal("explicit build.write should pass")
	}
	if permissions.Check([]string{permissions.AccountAdmin}, permissions.ReleaseWrite) {
		t.Fatal("release.write must not be implied by admin")
	}
	if permissions.Check([]string{permissions.AccountAdmin}, permissions.ReleaseRead) {
		t.Fatal("release.read must not be implied by admin")
	}
	if permissions.Check([]string{permissions.AccountAdmin}, permissions.ReleaseActivate) {
		t.Fatal("release.activate must not be implied by admin")
	}
	if permissions.Check([]string{permissions.DeployWrite}, permissions.ReleaseActivate) {
		t.Fatal("deploy.write must not imply release.activate")
	}
	if permissions.Check([]string{permissions.ReleaseWrite}, permissions.ReleaseActivate) {
		t.Fatal("release.write must not imply release.activate")
	}
	if permissions.Check([]string{permissions.ReleaseWrite}, permissions.DeployWrite) {
		t.Fatal("release.write must not imply deploy.write")
	}
	if !permissions.Check([]string{permissions.ReleaseActivate}, permissions.ReleaseActivate) {
		t.Fatal("explicit release.activate should pass")
	}
	if permissions.Check([]string{permissions.AccountAdmin}, permissions.TrafficWrite) {
		t.Fatal("traffic.write must not be implied by admin")
	}
	if permissions.Check([]string{permissions.ReleaseWrite}, permissions.TrafficWrite) {
		t.Fatal("release.write must not imply traffic.write")
	}
	if permissions.Check([]string{permissions.TrafficWrite}, permissions.ReleaseWrite) {
		t.Fatal("traffic.write must not imply release.write")
	}
	if !permissions.Check([]string{permissions.TrafficWrite}, permissions.TrafficWrite) {
		t.Fatal("explicit traffic.write should pass")
	}
	if permissions.Check([]string{permissions.AccountAdmin}, permissions.LogsRead) {
		t.Fatal("logs.read must not be implied by admin")
	}
	if permissions.Check([]string{permissions.AccountAdmin}, permissions.AuditRead) {
		t.Fatal("audit.read must not be implied by admin")
	}
	if !permissions.Check([]string{permissions.AuditRead}, permissions.AuditRead) {
		t.Fatal("explicit audit.read should pass")
	}
	if permissions.Check([]string{permissions.DeployWrite}, permissions.LogsRead) {
		t.Fatal("deploy.write must not imply logs.read")
	}
	if permissions.Check([]string{permissions.LogsWrite}, permissions.LogsRead) {
		t.Fatal("logs.write must not imply logs.read")
	}
	if !permissions.Check([]string{permissions.LogsRead}, permissions.LogsRead) {
		t.Fatal("explicit logs.read should pass")
	}
}

func TestFilterGrantableAISession(t *testing.T) {
	granted := []string{permissions.LogsRead, permissions.ReleaseRead, permissions.TrafficRead, permissions.CredentialsRW}
	ok, msg := permissions.FilterGrantable(granted, []string{permissions.LogsRead, permissions.TrafficWrite})
	if msg == "" {
		t.Fatalf("must not expand: %v", ok)
	}
	ok, msg = permissions.FilterGrantable(granted, []string{permissions.LogsRead, permissions.ReleaseRead})
	if msg != "" || len(ok) != 2 {
		t.Fatalf("subset: %v %q", ok, msg)
	}
	if _, msg := permissions.FilterGrantable(permissions.SessionScopes(), []string{permissions.AccountAdmin}); msg == "" {
		t.Fatal("account.admin must not be granted to AI")
	}
	ok, msg = permissions.FilterGrantable(permissions.SessionScopes(), []string{permissions.AuditRead})
	if msg != "" || len(ok) != 1 {
		t.Fatalf("audit.read may be granted explicitly: %v %q", ok, msg)
	}
}
