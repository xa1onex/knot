package plans

import (
	"context"
	"fmt"
	"strings"

	"github.com/knot-infra/knot/internal/store"
	"github.com/knot-infra/knot/pkg/permissions"
)

const (
	NameDiagnose = "diagnose-service"
	NameDeploy   = "deploy-release"
	NameUpdate   = "update-production"
	NameRestore  = "restore-backup"
)

type CatalogEntry struct {
	Name     string   `json:"name"`
	Title    string   `json:"title"`
	Intent   string   `json:"intent"`
	Steps    []string `json:"steps"`
	Risk     string   `json:"risk_level"`
	Approval bool     `json:"requires_approval"`
}

type stepSpec struct {
	Name   string
	Title  string
	Scope  string
	Scopes []string
	Any    bool
	Risk   string
	run    func(*Service, context.Context, *runState) (map[string]any, error)
}

type definition struct {
	Name    string
	Title   string
	Aliases []string
	Steps   []stepSpec
	Risk    func(CreateRequest) string
}

func Catalog() []CatalogEntry {
	return []CatalogEntry{
		{Name: NameDiagnose, Title: "Diagnose service", Intent: "Read-only operational diagnosis", Steps: []string{"ops.context", "traffic.status", "release.status", "logs.search", "health.check"}, Risk: store.RiskRead, Approval: false},
		{Name: NameDeploy, Title: "Deploy release candidate", Intent: "Build/image → release → health gate (no traffic switch)", Steps: []string{"build.status", "release.create", "deploy", "health.gate"}, Risk: store.RiskHigh, Approval: true},
		{Name: NameUpdate, Title: "Update production", Intent: "New image, production deploy, switch traffic", Steps: []string{"build.status", "release.create", "deploy", "health.gate", "traffic.switch"}, Risk: store.RiskCritical, Approval: true},
		{Name: NameRestore, Title: "Restore production backup", Intent: "Copy a backup onto a node (no traffic switch)", Steps: []string{"files.search", "storage.transfer", "jobs.create", "jobs.artifacts"}, Risk: store.RiskHigh, Approval: true},
	}
}

func riskRank(r string) int {
	switch r {
	case store.RiskRead:
		return 0
	case store.RiskLow:
		return 1
	case store.RiskMedium:
		return 2
	case store.RiskHigh:
		return 3
	case store.RiskCritical:
		return 4
	default:
		return 0
	}
}

func NeedsApproval(risk string) bool {
	return riskRank(risk) >= riskRank(store.RiskMedium)
}

func isProduction(req CreateRequest) bool {
	blob := strings.ToLower(strings.Join([]string{req.Environment, req.Hostname, req.Intent, req.Name}, " "))
	return strings.Contains(blob, "production") || strings.Contains(blob, "prod.")
}

func (s *Service) lookup(name string) (definition, error) {
	want := normalizeName(name)
	if want == "" {
		return definition{}, fmt.Errorf("%w: name or intent required", ErrValidation)
	}
	for _, d := range s.definitions() {
		if d.Name == want {
			return d, nil
		}
		for _, a := range d.Aliases {
			if a == want {
				return d, nil
			}
		}
	}
	return definition{}, fmt.Errorf("%w: unknown plan %s", ErrValidation, name)
}

func normalizeName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.ReplaceAll(name, "_", "-")
	return name
}

func inferName(intent string) string {
	in := strings.ToLower(intent)
	switch {
	case strings.Contains(in, "restore") || strings.Contains(in, "backup"):
		return NameRestore
	case strings.Contains(in, "diagnos"):
		return NameDiagnose
	case strings.Contains(in, "staging"):
		return NameDeploy
	case strings.Contains(in, "production") || strings.Contains(in, "обнови"):
		return NameUpdate
	case strings.Contains(in, "deploy") || strings.Contains(in, "update"):
		if strings.Contains(in, "production") {
			return NameUpdate
		}
		return NameDeploy
	default:
		return ""
	}
}

func (s *Service) definitions() []definition {
	return []definition{
		{
			Name: NameDiagnose, Title: "Diagnose service", Aliases: []string{"diagnose", "diagnostics"},
			Risk: func(CreateRequest) string { return store.RiskRead },
			Steps: []stepSpec{
				{Name: "ops.context", Title: "Load operational context", Scope: permissions.ServicesRead, Scopes: []string{permissions.ServicesRead, permissions.ReleaseRead, permissions.TrafficRead, permissions.DeployRead, permissions.LogsRead}, Any: true, Risk: store.RiskRead, run: (*Service).runOpsContext},
				{Name: "traffic.status", Title: "Read traffic binding", Scope: permissions.TrafficRead, Risk: store.RiskRead, run: (*Service).runTrafficStatus},
				{Name: "release.status", Title: "Read release status", Scope: permissions.ReleaseRead, Risk: store.RiskRead, run: (*Service).runReleaseStatus},
				{Name: "logs.search", Title: "Search recent errors", Scope: permissions.LogsRead, Risk: store.RiskRead, run: (*Service).runLogsSearch},
				{Name: "health.check", Title: "Probe service health", Scope: permissions.ServicesRead, Risk: store.RiskRead, run: (*Service).runHealthCheck},
			},
		},
		{
			Name: NameDeploy, Title: "Deploy release candidate", Aliases: []string{"deploy", "deploy-staging", "prepare-release"},
			Risk: func(req CreateRequest) string {
				if isProduction(req) {
					return store.RiskHigh
				}
				return store.RiskMedium
			},
			Steps: []stepSpec{
				{Name: "build.status", Title: "Resolve image / build", Scope: permissions.BuildRead, Risk: store.RiskRead, run: (*Service).runBuildStatus},
				{Name: "release.create", Title: "Create release", Scope: permissions.ReleaseWrite, Risk: store.RiskMedium, run: (*Service).runReleaseCreate},
				{Name: "deploy", Title: "Deploy candidate", Scope: permissions.ReleaseWrite, Risk: store.RiskHigh, run: (*Service).runDeploy},
				{Name: "health.gate", Title: "Health gate", Scope: permissions.ReleaseRead, Risk: store.RiskRead, run: (*Service).runHealthGate},
			},
		},
		{
			Name: NameUpdate, Title: "Update production", Aliases: []string{"update-prod", "update", "production-update"},
			Risk: func(CreateRequest) string { return store.RiskCritical },
			Steps: []stepSpec{
				{Name: "build.status", Title: "Resolve image / build", Scope: permissions.BuildRead, Risk: store.RiskRead, run: (*Service).runBuildStatus},
				{Name: "release.create", Title: "Create release", Scope: permissions.ReleaseWrite, Risk: store.RiskHigh, run: (*Service).runReleaseCreate},
				{Name: "deploy", Title: "Deploy candidate", Scope: permissions.ReleaseWrite, Risk: store.RiskHigh, run: (*Service).runDeploy},
				{Name: "health.gate", Title: "Health gate", Scope: permissions.ReleaseRead, Risk: store.RiskRead, run: (*Service).runHealthGate},
				{Name: "traffic.switch", Title: "Switch production traffic 100%", Scope: permissions.TrafficWrite, Risk: store.RiskCritical, run: (*Service).runTrafficSwitch},
			},
		},
		{
			Name: NameRestore, Title: "Restore production backup", Aliases: []string{"restore", "backup-restore"},
			Risk: func(CreateRequest) string { return store.RiskHigh },
			Steps: []stepSpec{
				{Name: "files.search", Title: "Find backup object", Scope: permissions.StorageRead, Risk: store.RiskRead, run: (*Service).runFilesSearch},
				{Name: "storage.transfer", Title: "Copy backup between nodes", Scope: permissions.StorageWrite, Risk: store.RiskMedium, run: (*Service).runStorageTransfer},
				{Name: "jobs.create", Title: "Run restore job", Scope: permissions.ComputeWrite, Risk: store.RiskHigh, run: (*Service).runJobCreate},
				{Name: "jobs.artifacts", Title: "Read job artifacts", Scope: permissions.ComputeRead, Risk: store.RiskRead, run: (*Service).runJobArtifacts},
			},
		},
	}
}
