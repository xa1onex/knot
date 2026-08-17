package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/knot-infra/knot/internal/audit"
	"github.com/knot-infra/knot/internal/builds"
	"github.com/knot-infra/knot/internal/edge"
	"github.com/knot-infra/knot/internal/files"
	"github.com/knot-infra/knot/internal/jobs"
	"github.com/knot-infra/knot/internal/oplogs"
	"github.com/knot-infra/knot/internal/ops"
	"github.com/knot-infra/knot/internal/releases"
	"github.com/knot-infra/knot/internal/storage"
	"github.com/knot-infra/knot/internal/store"
	"github.com/knot-infra/knot/internal/traffic"
	"github.com/knot-infra/knot/pkg/permissions"
)

var (
	ErrNotFound   = errors.New("workflow not found")
	ErrValidation = errors.New("invalid workflow")
	ErrDenied     = errors.New("missing scope")
)

const (
	NameDiagnose = "diagnose-service"
	NameDeploy   = "deploy-release"
	NameRestore  = "restore-backup"
)

// Service runs catalogued compositions of existing Node primitives.
// It does not add mutation APIs or an AI permission layer.
type Service struct {
	Store    *store.Store
	Ops      *ops.Service
	Traffic  *traffic.Service
	Releases *releases.Service
	Builds   *builds.Service
	Jobs     *jobs.Service
	Files    *files.Service
	Storage  *storage.Service
	Edge     *edge.Proxy
	Logs     *oplogs.Service
	Audit    *audit.Logger
}

func New(
	st *store.Store,
	opsSvc *ops.Service,
	traf *traffic.Service,
	rel *releases.Service,
	blds *builds.Service,
	jobsSvc *jobs.Service,
	filesSvc *files.Service,
	stor *storage.Service,
	edgeProxy *edge.Proxy,
	logs *oplogs.Service,
	auditLog *audit.Logger,
) *Service {
	return &Service{
		Store: st, Ops: opsSvc, Traffic: traf, Releases: rel, Builds: blds,
		Jobs: jobsSvc, Files: filesSvc, Storage: stor, Edge: edgeProxy, Logs: logs, Audit: auditLog,
	}
}

type CatalogEntry struct {
	Name     string   `json:"name"`
	Title    string   `json:"title"`
	Steps    []string `json:"steps"`
	Mutating bool     `json:"mutating"`
}

type RunRequest struct {
	UserID       string
	Actor        string
	CredID       string
	Name         string
	Service      string
	DeviceID     string
	Image        string
	BuildID      string
	Port         int
	Hostname     string
	Environment  string
	Query        string
	Path         string
	FromDeviceID string
	ToDeviceID   string
	ToPath       string
	JobImage     string
	Can          func(scope string) bool
}

type runState struct {
	req     RunRequest
	wf      *store.Workflow
	view    *ops.Context
	traffic *traffic.Status
	current *store.Release
	latest  *store.Release
	created *store.Release
	backup  *store.FileIndexRow
	job     *store.ComputeJob
}

type stepSpec struct {
	Name     string
	Scopes   []string
	Any      bool
	Optional bool
	run      func(context.Context, *runState) (map[string]any, error)
}

type definition struct {
	Name     string
	Title    string
	Aliases  []string
	Mutating bool
	Steps    []stepSpec
}

func Catalog() []CatalogEntry {
	return []CatalogEntry{
		{Name: NameDiagnose, Title: "Diagnose service", Steps: []string{"ops.context", "traffic.status", "release.status", "logs.search", "health.check"}, Mutating: false},
		{Name: NameDeploy, Title: "Deploy release candidate", Steps: []string{"build.status", "release.create", "deploy", "health.gate"}, Mutating: true},
		{Name: NameRestore, Title: "Restore production backup", Steps: []string{"files.search", "storage.transfer", "jobs.create", "jobs.artifacts"}, Mutating: true},
	}
}

func (s *Service) definitions() []definition {
	return []definition{
		{
			Name: NameDiagnose, Title: "Diagnose service", Aliases: []string{"diagnose", "diagnostics"},
			Steps: []stepSpec{
				{Name: "ops.context", Any: true, Scopes: []string{permissions.ServicesRead, permissions.ReleaseRead, permissions.TrafficRead, permissions.DeployRead, permissions.LogsRead}, run: s.stepOpsContext},
				{Name: "traffic.status", Scopes: []string{permissions.TrafficRead}, run: s.stepTrafficStatus},
				{Name: "release.status", Scopes: []string{permissions.ReleaseRead}, run: s.stepReleaseStatus},
				{Name: "logs.search", Scopes: []string{permissions.LogsRead}, run: s.stepLogsSearch},
				{Name: "health.check", Scopes: []string{permissions.ServicesRead}, Optional: true, run: s.stepHealthCheck},
			},
		},
		{
			Name: NameDeploy, Title: "Deploy release candidate", Aliases: []string{"deploy", "prepare-release"}, Mutating: true,
			Steps: []stepSpec{
				{Name: "build.status", Scopes: []string{permissions.BuildRead}, Optional: true, run: s.stepBuildStatus},
				{Name: "release.create", Scopes: []string{permissions.ReleaseWrite}, run: s.stepReleaseCreate},
				{Name: "deploy", Scopes: []string{permissions.ReleaseWrite}, run: s.stepDeploy},
				{Name: "health.gate", Scopes: []string{permissions.ReleaseRead}, run: s.stepHealthGate},
			},
		},
		{
			Name: NameRestore, Title: "Restore production backup", Aliases: []string{"restore", "backup-restore"}, Mutating: true,
			Steps: []stepSpec{
				{Name: "files.search", Scopes: []string{permissions.StorageRead}, run: s.stepFilesSearch},
				{Name: "storage.transfer", Scopes: []string{permissions.StorageWrite}, Optional: true, run: s.stepStorageTransfer},
				{Name: "jobs.create", Scopes: []string{permissions.ComputeWrite}, Optional: true, run: s.stepJobCreate},
				{Name: "jobs.artifacts", Scopes: []string{permissions.ComputeRead}, Optional: true, run: s.stepJobArtifacts},
			},
		},
	}
}

func (s *Service) lookup(name string) (definition, error) {
	want := normalizeName(name)
	if want == "" {
		return definition{}, fmt.Errorf("%w: name required", ErrValidation)
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
	return definition{}, fmt.Errorf("%w: unknown workflow %s", ErrValidation, name)
}

func normalizeName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.ReplaceAll(name, "_", "-")
	return name
}

func RunScopes() []string {
	seen := map[string]bool{}
	var out []string
	for _, e := range []string{
		permissions.ServicesRead, permissions.ReleaseRead, permissions.TrafficRead, permissions.DeployRead, permissions.LogsRead,
		permissions.BuildRead, permissions.ReleaseWrite,
		permissions.StorageRead, permissions.StorageWrite, permissions.ComputeRead, permissions.ComputeWrite,
	} {
		if !seen[e] {
			seen[e] = true
			out = append(out, e)
		}
	}
	return out
}

func (s *Service) List(ctx context.Context, userID string) ([]store.Workflow, error) {
	return s.Store.ListWorkflows(ctx, userID, 50)
}

func (s *Service) Get(ctx context.Context, userID, id string) (*store.Workflow, error) {
	wf, err := s.Store.GetWorkflow(ctx, userID, strings.TrimSpace(id))
	if err != nil {
		if store.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return wf, nil
}

func (s *Service) Steps(ctx context.Context, userID, id string) ([]store.WorkflowStep, error) {
	wf, err := s.Get(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	return wf.Steps, nil
}

func (s *Service) Run(ctx context.Context, req RunRequest) (*store.Workflow, error) {
	def, err := s.lookup(req.Name)
	if err != nil {
		return nil, err
	}
	can := req.Can
	if can == nil {
		can = func(string) bool { return true }
	}
	req.Can = can
	req.Name = def.Name

	trace := oplogs.ResolveTrace(ctx, "")
	ctx = oplogs.WithTrace(ctx, trace)

	input, _ := json.Marshal(map[string]any{
		"service": req.Service, "device_id": req.DeviceID, "image": req.Image, "build_id": req.BuildID,
		"hostname": req.Hostname, "query": req.Query, "path": req.Path,
	})
	wf := &store.Workflow{
		UserID: req.UserID, Name: def.Name, Title: def.Title, Actor: req.Actor,
		Status: store.WorkflowStatusRunning, TraceID: trace, InputJSON: string(input),
	}
	steps := make([]store.WorkflowStep, 0, len(def.Steps))
	for i, spec := range def.Steps {
		steps = append(steps, store.WorkflowStep{
			Seq: i + 1, Name: spec.Name, Status: store.WorkflowStepPending,
			Scope: strings.Join(spec.Scopes, ","), TraceID: trace,
		})
	}
	if err := s.Store.CreateWorkflow(ctx, wf, steps); err != nil {
		return nil, err
	}
	ctx = audit.WithWorkflow(ctx, wf.ID)
	s.audit(ctx, req, "workflow.run", wf.ID, def.Name, "SUCCESS")
	s.emit(ctx, req, wf, nil, "workflow started "+def.Name, "info")

	st := &runState{req: req, wf: wf}
	for i := range wf.Steps {
		spec := def.Steps[i]
		step := &wf.Steps[i]
		if err := s.runStep(ctx, st, spec, step); err != nil {
			return s.finish(ctx, st, err)
		}
	}
	return s.finish(ctx, st, nil)
}

func (s *Service) runStep(ctx context.Context, st *runState, spec stepSpec, step *store.WorkflowStep) error {
	now := time.Now().UTC()
	step.Status = store.WorkflowStepRunning
	step.StartedAt = &now
	step.TraceID = st.wf.TraceID
	_ = s.Store.UpdateWorkflowStep(ctx, step)
	s.emit(ctx, st.req, st.wf, step, "workflow.step "+spec.Name, "info")

	if !scopeOK(st.req.Can, spec) {
		msg := "missing scope: " + strings.Join(spec.Scopes, ",")
		if spec.Optional {
			return s.completeStep(ctx, st, step, store.WorkflowStepSkipped, msg, map[string]any{"skipped": true, "reason": msg}, nil)
		}
		s.audit(ctx, st.req, "workflow.step", step.ID, spec.Name, "DENIED")
		_ = s.completeStep(ctx, st, step, store.WorkflowStepDenied, msg, nil, fmt.Errorf("%w: %s", ErrDenied, spec.Name))
		return fmt.Errorf("%w: %s", ErrDenied, spec.Name)
	}

	out, err := spec.run(ctx, st)
	if err != nil {
		s.audit(ctx, st.req, "workflow.step", step.ID, spec.Name, "FAILURE")
		_ = s.completeStep(ctx, st, step, store.WorkflowStepFailed, err.Error(), out, err)
		return err
	}
	s.audit(ctx, st.req, "workflow.step", step.ID, spec.Name, "SUCCESS")
	return s.completeStep(ctx, st, step, store.WorkflowStepSucceeded, "", out, nil)
}

func scopeOK(can func(string) bool, spec stepSpec) bool {
	if len(spec.Scopes) == 0 {
		return true
	}
	if spec.Any {
		for _, sc := range spec.Scopes {
			if can(sc) {
				return true
			}
		}
		return false
	}
	for _, sc := range spec.Scopes {
		if !can(sc) {
			return false
		}
	}
	return true
}

func (s *Service) completeStep(ctx context.Context, st *runState, step *store.WorkflowStep, status, errMsg string, out map[string]any, runErr error) error {
	now := time.Now().UTC()
	step.Status = status
	step.Error = errMsg
	step.FinishedAt = &now
	if out == nil {
		out = map[string]any{}
	}
	b, _ := json.Marshal(out)
	step.OutputJSON = string(b)
	_ = s.Store.UpdateWorkflowStep(ctx, step)
	if runErr != nil {
		s.emit(ctx, st.req, st.wf, step, "workflow.step "+step.Name+" "+status, "error")
	}
	return runErr
}

func (s *Service) finish(ctx context.Context, st *runState, runErr error) (*store.Workflow, error) {
	now := time.Now().UTC()
	st.wf.FinishedAt = &now
	switch {
	case runErr == nil:
		st.wf.Status = store.WorkflowStatusSucceeded
		st.wf.Error = ""
	case errors.Is(runErr, ErrDenied):
		st.wf.Status = store.WorkflowStatusDenied
		st.wf.Error = runErr.Error()
	default:
		st.wf.Status = store.WorkflowStatusFailed
		st.wf.Error = runErr.Error()
	}
	if st.wf.Name == NameDiagnose {
		st.wf.ResultJSON = mustJSON(diagnoseResult(st))
	} else if st.created != nil {
		st.wf.ResultJSON = mustJSON(map[string]any{
			"release_id": st.created.ID, "number": st.created.Number, "status": st.created.Status,
			"traffic_switched": false,
		})
	} else if st.backup != nil || st.job != nil {
		out := map[string]any{}
		if st.backup != nil {
			out["path"] = st.backup.Path
			out["device_id"] = st.backup.DeviceID
		}
		if st.job != nil {
			out["job_id"] = st.job.ID
			out["job_status"] = st.job.Status
		}
		st.wf.ResultJSON = mustJSON(out)
	}
	if err := s.Store.UpdateWorkflow(ctx, st.wf); err != nil {
		return st.wf, err
	}
	got, err := s.Store.GetWorkflow(ctx, st.wf.UserID, st.wf.ID)
	if err != nil {
		return st.wf, err
	}
	result := "SUCCESS"
	if runErr != nil {
		result = "FAILURE"
		if errors.Is(runErr, ErrDenied) {
			result = "DENIED"
		}
	}
	s.audit(ctx, st.req, "workflow.finish", got.ID, got.Name, result)
	s.emit(ctx, st.req, got, nil, "workflow "+got.Status+" "+got.Name, levelFor(got.Status))
	return got, nil
}

func (s *Service) audit(ctx context.Context, req RunRequest, action, resource, detail, result string) {
	if s.Audit == nil {
		return
	}
	s.Audit.Log(ctx, req.UserID, req.Actor, action, resource, detail, result)
}

func (s *Service) emit(ctx context.Context, req RunRequest, wf *store.Workflow, step *store.WorkflowStep, msg, level string) {
	if s.Logs == nil || wf == nil {
		return
	}
	meta := map[string]any{"workflow_id": wf.ID, "workflow": wf.Name, "actor": req.Actor}
	if step != nil {
		meta["step_id"] = step.ID
		meta["step"] = step.Name
	}
	s.Logs.Emit(ctx, oplogs.Event{
		UserID: req.UserID, Source: oplogs.SourceWorkflow, Level: level, Message: msg,
		TraceID: wf.TraceID, Service: req.Service, DeviceID: req.DeviceID, Metadata: meta,
	})
}

func levelFor(status string) string {
	if status == store.WorkflowStatusFailed || status == store.WorkflowStatusDenied {
		return "error"
	}
	return "info"
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}
