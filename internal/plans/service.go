package plans

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/knot-infra/knot/internal/aisessions"
	"github.com/knot-infra/knot/internal/audit"
	"github.com/knot-infra/knot/internal/auth"
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
)

var (
	ErrNotFound   = errors.New("plan not found")
	ErrValidation = errors.New("invalid plan")
	ErrForbidden  = errors.New("forbidden")
	ErrExpired    = errors.New("plan expired")
	ErrDenied     = errors.New("missing scope")
	ErrState      = errors.New("plan cannot be executed in this state")
)

const (
	DefaultTTL = 30 * time.Minute
	MaxTTL     = 7 * 24 * time.Hour
	MinTTL     = time.Second
)

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

type CreateRequest struct {
	UserID       string
	Actor        string
	Kind         auth.IdentityKind
	CredID       string
	Email        string
	Intent       string
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
	TTLMinutes   int
	ExpiresIn    string
	AutoExecute  bool
}

type ActorRequest struct {
	UserID string
	Actor  string
	Kind   auth.IdentityKind
	CredID string
	Email  string
	Can    func(scope string) bool
}

type runState struct {
	req     ActorRequest
	input   CreateRequest
	plan    *store.Plan
	def     definition
	view    *ops.Context
	traffic *traffic.Status
	current *store.Release
	latest  *store.Release
	created *store.Release
	backup  *store.FileIndexRow
	job     *store.ComputeJob
}

func (s *Service) List(ctx context.Context, userID string) ([]store.Plan, error) {
	list, err := s.Store.ListPlans(ctx, userID, 50)
	if err != nil {
		return nil, err
	}
	for i := range list {
		s.expireIfNeeded(ctx, &list[i])
	}
	return list, nil
}

func (s *Service) Get(ctx context.Context, userID, id string) (*store.Plan, error) {
	p, err := s.Store.GetPlan(ctx, userID, strings.TrimSpace(id))
	if err != nil {
		if store.IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	s.expireIfNeeded(ctx, p)
	return p, nil
}

func (s *Service) Create(ctx context.Context, req CreateRequest) (*store.Plan, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = inferName(req.Intent)
	}
	def, err := s.lookup(name)
	if err != nil {
		return nil, err
	}
	req.Name = def.Name
	ttl, err := parseTTL(req.TTLMinutes, req.ExpiresIn)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrValidation, err.Error())
	}
	risk := def.Risk(req)
	status := store.PlanStatusReadyForApproval
	if !NeedsApproval(risk) {
		status = store.PlanStatusReady
	} else if req.AutoExecute && riskRank(risk) <= riskRank(store.RiskMedium) && req.Kind != auth.KindAI {
		status = store.PlanStatusReady
	}
	intent := strings.TrimSpace(req.Intent)
	if intent == "" {
		intent = def.Title
	}
	trace := oplogs.ResolveTrace(ctx, "")
	input, _ := json.Marshal(map[string]any{
		"service": req.Service, "device_id": req.DeviceID, "image": req.Image, "build_id": req.BuildID,
		"hostname": req.Hostname, "environment": req.Environment, "port": req.Port,
		"query": req.Query, "path": req.Path, "intent": intent,
	})
	sessionID := ""
	if req.Kind == auth.KindAI {
		sessionID = req.CredID
	}
	createdBy := strings.TrimSpace(req.Email)
	if createdBy == "" {
		createdBy = req.Actor
	}
	p := &store.Plan{
		UserID: req.UserID, Name: def.Name, Title: def.Title, Intent: intent,
		CreatedBy: createdBy, AISessionID: sessionID, Actor: req.Actor,
		TraceID: trace, RiskLevel: risk, Status: status, InputJSON: string(input),
		ExpiresAt: time.Now().UTC().Add(ttl),
	}
	steps := make([]store.PlanStep, 0, len(def.Steps))
	for i, spec := range def.Steps {
		scope := spec.Scope
		if spec.Any && len(spec.Scopes) > 0 {
			scope = strings.Join(spec.Scopes, ",")
		}
		steps = append(steps, store.PlanStep{
			Seq: i + 1, Name: spec.Name, Title: spec.Title, Status: store.PlanStepPending,
			Scope: scope, RiskLevel: spec.Risk,
		})
	}
	if err := s.Store.CreatePlan(ctx, p, steps); err != nil {
		return nil, err
	}
	ctx = audit.WithPlan(ctx, p.ID)
	s.audit(ctx, req.UserID, req.Actor, "plan.create", p.ID, def.Name+" "+risk, "SUCCESS")
	return p, nil
}

func (s *Service) Cancel(ctx context.Context, userID, id, actor string) (*store.Plan, error) {
	p, err := s.Get(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	switch p.Status {
	case store.PlanStatusSucceeded, store.PlanStatusFailed, store.PlanStatusDenied,
		store.PlanStatusCancelled, store.PlanStatusExpired, store.PlanStatusExecuting:
		return nil, fmt.Errorf("%w: cannot cancel %s plan", ErrState, p.Status)
	}
	p.Status = store.PlanStatusCancelled
	now := time.Now().UTC()
	p.FinishedAt = &now
	if err := s.Store.UpdatePlan(ctx, p); err != nil {
		return nil, err
	}
	ctx = audit.WithPlan(ctx, p.ID)
	s.audit(ctx, userID, actor, "plan.cancel", p.ID, p.Name, "SUCCESS")
	return p, nil
}

func (s *Service) Approve(ctx context.Context, actor ActorRequest, id string) (*store.Plan, error) {
	if actor.Kind == auth.KindAI {
		return nil, fmt.Errorf("%w: AI session cannot approve a plan", ErrForbidden)
	}
	p, err := s.Get(ctx, actor.UserID, id)
	if err != nil {
		return nil, err
	}
	if p.Status == store.PlanStatusExpired {
		return nil, ErrExpired
	}
	if p.Status != store.PlanStatusReadyForApproval && p.Status != store.PlanStatusReady {
		return nil, fmt.Errorf("%w: status is %s", ErrState, p.Status)
	}
	if time.Now().UTC().After(p.ExpiresAt) {
		s.markExpired(ctx, p)
		return nil, ErrExpired
	}
	now := time.Now().UTC()
	p.Status = store.PlanStatusApproved
	p.ApprovedBy = strings.TrimSpace(actor.Email)
	if p.ApprovedBy == "" {
		p.ApprovedBy = actor.Actor
	}
	p.ApprovedAt = &now
	if err := s.Store.UpdatePlan(ctx, p); err != nil {
		return nil, err
	}
	ctx = audit.WithPlan(ctx, p.ID)
	s.audit(ctx, actor.UserID, actor.Actor, "plan.approve", p.ID, p.Name, "SUCCESS")
	return s.Execute(ctx, actor, p.ID)
}

func (s *Service) Execute(ctx context.Context, actor ActorRequest, id string) (*store.Plan, error) {
	p, err := s.Get(ctx, actor.UserID, id)
	if err != nil {
		return nil, err
	}
	if p.Status == store.PlanStatusExpired || time.Now().UTC().After(p.ExpiresAt) {
		s.markExpired(ctx, p)
		return nil, ErrExpired
	}
	if actor.Kind == auth.KindAI && NeedsApproval(p.RiskLevel) {
		return nil, fmt.Errorf("%w: AI cannot execute a %s plan", ErrForbidden, p.RiskLevel)
	}
	switch p.Status {
	case store.PlanStatusReady, store.PlanStatusApproved:
	default:
		return nil, fmt.Errorf("%w: status is %s", ErrState, p.Status)
	}
	def, err := s.lookup(p.Name)
	if err != nil {
		return nil, err
	}
	if actor.Can == nil {
		actor.Can = func(string) bool { return false }
	}
	p.Status = store.PlanStatusExecuting
	if err := s.Store.UpdatePlan(ctx, p); err != nil {
		return nil, err
	}
	ctx = audit.WithPlan(ctx, p.ID)
	ctx = oplogs.WithTrace(ctx, p.TraceID)
	s.audit(ctx, actor.UserID, actor.Actor, "plan.execute", p.ID, p.Name, "SUCCESS")

	st := &runState{req: actor, plan: p, def: def, input: inputFromPlan(p)}
	for i := range p.Steps {
		spec := def.Steps[i]
		step := &p.Steps[i]
		if err := s.runStep(ctx, st, spec, step); err != nil {
			return s.finish(ctx, st, err)
		}
	}
	return s.finish(ctx, st, nil)
}

func (s *Service) runStep(ctx context.Context, st *runState, spec stepSpec, step *store.PlanStep) error {
	now := time.Now().UTC()
	stepTrace := oplogs.NewTraceID()
	step.Status = store.PlanStepRunning
	step.StartedAt = &now
	step.TraceID = stepTrace
	_ = s.Store.UpdatePlanStep(ctx, step)
	stepCtx := oplogs.WithTrace(ctx, stepTrace)
	stepCtx = audit.WithPlan(stepCtx, st.plan.ID)

	if !scopeOK(st.req.Can, spec) {
		msg := "missing scope: " + spec.Scope
		s.audit(stepCtx, st.req.UserID, st.req.Actor, "plan.step", step.ID, spec.Name, "DENIED")
		_ = s.completeStep(ctx, step, store.PlanStepDenied, msg, nil)
		return fmt.Errorf("%w: %s", ErrDenied, spec.Name)
	}
	out, err := spec.run(s, stepCtx, st)
	if err != nil {
		s.audit(stepCtx, st.req.UserID, st.req.Actor, "plan.step", step.ID, spec.Name, "FAILURE")
		_ = s.completeStep(ctx, step, store.PlanStepFailed, err.Error(), out)
		return err
	}
	s.audit(stepCtx, st.req.UserID, st.req.Actor, "plan.step", step.ID, spec.Name, "SUCCESS")
	return s.completeStep(ctx, step, store.PlanStepSucceeded, "", out)
}

func scopeOK(can func(string) bool, spec stepSpec) bool {
	if spec.Any {
		for _, sc := range spec.Scopes {
			if can(sc) {
				return true
			}
		}
		return false
	}
	if spec.Scope == "" {
		return true
	}
	return can(spec.Scope)
}

func (s *Service) completeStep(ctx context.Context, step *store.PlanStep, status, errMsg string, out map[string]any) error {
	now := time.Now().UTC()
	step.Status = status
	step.Error = errMsg
	step.FinishedAt = &now
	if out == nil {
		out = map[string]any{}
	}
	b, _ := json.Marshal(out)
	step.OutputJSON = string(b)
	return s.Store.UpdatePlanStep(ctx, step)
}

func (s *Service) finish(ctx context.Context, st *runState, runErr error) (*store.Plan, error) {
	now := time.Now().UTC()
	st.plan.FinishedAt = &now
	switch {
	case runErr == nil:
		st.plan.Status = store.PlanStatusSucceeded
		st.plan.Error = ""
	case errors.Is(runErr, ErrDenied):
		st.plan.Status = store.PlanStatusDenied
		st.plan.Error = runErr.Error()
	default:
		st.plan.Status = store.PlanStatusFailed
		st.plan.Error = runErr.Error()
	}
	result := map[string]any{"status": st.plan.Status, "name": st.plan.Name}
	if st.created != nil {
		result["release_id"] = st.created.ID
		result["number"] = st.created.Number
		result["release_status"] = st.created.Status
	}
	if st.traffic != nil {
		result["hostname"] = st.traffic.Hostname
		result["active_release_id"] = st.traffic.ActiveReleaseID
	}
	b, _ := json.Marshal(result)
	st.plan.ResultJSON = string(b)
	if err := s.Store.UpdatePlan(ctx, st.plan); err != nil {
		return st.plan, err
	}
	got, err := s.Store.GetPlan(ctx, st.plan.UserID, st.plan.ID)
	if err != nil {
		return st.plan, err
	}
	out := "SUCCESS"
	if runErr != nil {
		out = "FAILURE"
		if errors.Is(runErr, ErrDenied) {
			out = "DENIED"
		}
	}
	s.audit(ctx, st.req.UserID, st.req.Actor, "plan.finish", got.ID, got.Name, out)
	return got, nil
}

func (s *Service) expireIfNeeded(ctx context.Context, p *store.Plan) {
	if p == nil {
		return
	}
	switch p.Status {
	case store.PlanStatusReady, store.PlanStatusReadyForApproval, store.PlanStatusApproved:
		if time.Now().UTC().After(p.ExpiresAt) {
			s.markExpired(ctx, p)
		}
	}
}

func (s *Service) markExpired(ctx context.Context, p *store.Plan) {
	if p.Status == store.PlanStatusExpired {
		return
	}
	p.Status = store.PlanStatusExpired
	now := time.Now().UTC()
	p.FinishedAt = &now
	_ = s.Store.UpdatePlan(ctx, p)
	s.audit(ctx, p.UserID, "system", "plan.expire", p.ID, p.Name, "SUCCESS")
}

func (s *Service) audit(ctx context.Context, userID, actor, action, resource, detail, result string) {
	if s.Audit == nil {
		return
	}
	s.Audit.Log(ctx, userID, actor, action, resource, detail, result)
}

func parseTTL(minutes int, expiresIn string) (time.Duration, error) {
	return aisessions.ParseTTL(minutes, expiresIn)
}

func inputFromPlan(p *store.Plan) CreateRequest {
	var m map[string]any
	_ = json.Unmarshal([]byte(p.InputJSON), &m)
	str := func(k string) string {
		v, _ := m[k].(string)
		return v
	}
	port := 0
	switch v := m["port"].(type) {
	case float64:
		port = int(v)
	}
	return CreateRequest{
		Service: str("service"), DeviceID: str("device_id"), Image: str("image"),
		BuildID: str("build_id"), Hostname: str("hostname"), Environment: str("environment"),
		Query: str("query"), Path: str("path"), Intent: str("intent"), Port: port,
	}
}
