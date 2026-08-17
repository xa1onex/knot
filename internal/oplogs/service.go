package oplogs

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/knot-infra/knot/internal/store"
)

const (
	SourceAgent    = "agent"
	SourceDeploy   = "deploy"
	SourceBuild    = "build"
	SourceEdge     = "edge"
	SourceJob      = "job"
	SourceSystem   = "system"
	SourceAudit    = "audit"
	SourceRelease  = "release"
	SourceWorkflow = "workflow"
	SourcePlan     = "plan"

	DefaultRetentionDays = 30
)

type ctxKey int

const traceKey ctxKey = 1

func NewTraceID() string {
	return strings.ReplaceAll(uuid.NewString(), "-", "")[:16]
}

func WithTrace(ctx context.Context, id string) context.Context {
	id = strings.TrimSpace(id)
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, traceKey, id)
}

func TraceFrom(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(traceKey).(string)
	return v
}

func ResolveTrace(ctx context.Context, inherited string) string {
	if inherited = strings.TrimSpace(inherited); inherited != "" {
		return inherited
	}
	if t := TraceFrom(ctx); t != "" {
		return t
	}
	return NewTraceID()
}

func LevelFromStream(stream string) string {
	if strings.EqualFold(stream, "stderr") {
		return "error"
	}
	return "info"
}

func ValidSource(src string) bool {
	switch src {
	case SourceAgent, SourceDeploy, SourceBuild, SourceEdge, SourceJob, SourceSystem, SourceAudit, SourceRelease, SourceWorkflow, SourcePlan:
		return true
	default:
		return false
	}
}

type Event struct {
	ID           string
	Timestamp    time.Time
	Level        string
	Source       string
	Message      string
	TraceID      string
	UserID       string
	DeviceID     string
	ServiceID    string
	Service      string
	ReleaseID    string
	BuildID      string
	JobID        string
	DeploymentID string
	Metadata     map[string]any
}

type Query struct {
	Service      string
	ServiceID    string
	ReleaseID    string
	BuildID      string
	JobID        string
	DeploymentID string
	Source       string
	TraceID      string
	Level        string
	Q            string
	After        string
	Since        *time.Time
	Until        *time.Time
	Limit        int
}

type subscriber struct {
	userID string
	q      Query
	ch     chan Event
}

type Service struct {
	Store         *store.Store
	RetentionDays int

	mu   sync.Mutex
	subs map[chan Event]*subscriber
}

func New(st *store.Store, retentionDays int) *Service {
	if retentionDays < 1 {
		retentionDays = DefaultRetentionDays
	}
	return &Service{Store: st, RetentionDays: retentionDays, subs: make(map[chan Event]*subscriber)}
}

func (s *Service) Emit(ctx context.Context, ev Event) Event {
	if s == nil || s.Store == nil {
		return ev
	}
	if ev.UserID == "" || ev.Source == "" || ev.Message == "" {
		return ev
	}
	if !ValidSource(ev.Source) {
		ev.Source = SourceSystem
	}
	ev.Message = Redact(ev.Message)
	if ev.Level == "" {
		ev.Level = "info"
	}
	if ev.TraceID == "" {
		ev.TraceID = TraceFrom(ctx)
	}
	if ev.Timestamp.IsZero() {
		ev.Timestamp = time.Now().UTC()
	}
	if ev.ID == "" {
		ev.ID = store.NewID()
	}
	meta := "{}"
	if len(ev.Metadata) > 0 {
		if b, err := json.Marshal(ev.Metadata); err == nil {
			meta = string(b)
		}
	}
	row := &store.OpsLog{
		ID: ev.ID, UserID: ev.UserID, CreatedAt: ev.Timestamp, Level: ev.Level, Source: ev.Source,
		Message: ev.Message, TraceID: ev.TraceID, DeviceID: ev.DeviceID, ServiceID: ev.ServiceID,
		ServiceName: ev.Service, ReleaseID: ev.ReleaseID, BuildID: ev.BuildID, JobID: ev.JobID,
		DeploymentID: ev.DeploymentID, MetadataJSON: meta,
	}
	if err := s.Store.InsertOpsLog(ctx, row); err != nil {
		return ev
	}
	ev.ID = row.ID
	ev.Timestamp = row.CreatedAt
	s.broadcast(ev)
	return ev
}

func (s *Service) Query(ctx context.Context, userID string, q Query) ([]store.OpsLog, error) {
	if s == nil || s.Store == nil {
		return []store.OpsLog{}, nil
	}
	return s.Store.ListOpsLogs(ctx, store.OpsLogQuery{
		UserID: userID, Service: strings.TrimSpace(q.Service), ServiceID: strings.TrimSpace(q.ServiceID),
		ReleaseID: strings.TrimSpace(q.ReleaseID), BuildID: strings.TrimSpace(q.BuildID),
		JobID: strings.TrimSpace(q.JobID), DeploymentID: strings.TrimSpace(q.DeploymentID),
		Source: strings.TrimSpace(q.Source), TraceID: strings.TrimSpace(q.TraceID),
		Level: strings.TrimSpace(q.Level), Q: strings.TrimSpace(q.Q), AfterID: strings.TrimSpace(q.After),
		Since: q.Since, Until: q.Until, Limit: q.Limit,
	})
}

func (s *Service) Purge(ctx context.Context) (int64, error) {
	if s == nil || s.Store == nil {
		return 0, nil
	}
	days := s.RetentionDays
	if days < 1 {
		days = DefaultRetentionDays
	}
	cutoff := time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour)
	return s.Store.DeleteOpsLogsBefore(ctx, cutoff)
}

func (s *Service) StartSweeper(ctx context.Context) {
	if s == nil {
		return
	}
	_, _ = s.Purge(ctx)
	go func() {
		t := time.NewTicker(time.Hour)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				_, _ = s.Purge(context.Background())
			}
		}
	}()
}

func (s *Service) Subscribe(userID string, q Query) (<-chan Event, func()) {
	if s == nil {
		ch := make(chan Event)
		close(ch)
		return ch, func() {}
	}
	ch := make(chan Event, 32)
	sub := &subscriber{userID: userID, q: q, ch: ch}
	s.mu.Lock()
	s.subs[ch] = sub
	s.mu.Unlock()
	return ch, func() {
		s.mu.Lock()
		if _, ok := s.subs[ch]; ok {
			delete(s.subs, ch)
			close(ch)
		}
		s.mu.Unlock()
	}
}

func (s *Service) broadcast(ev Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sub := range s.subs {
		if sub.userID != ev.UserID || !matchEvent(ev, sub.q) {
			continue
		}
		select {
		case sub.ch <- ev:
		default:
		}
	}
}

func matchEvent(ev Event, q Query) bool {
	if q.Service != "" && !strings.EqualFold(ev.Service, q.Service) {
		return false
	}
	if q.ServiceID != "" && ev.ServiceID != q.ServiceID {
		return false
	}
	if q.ReleaseID != "" && ev.ReleaseID != q.ReleaseID {
		return false
	}
	if q.BuildID != "" && ev.BuildID != q.BuildID {
		return false
	}
	if q.JobID != "" && ev.JobID != q.JobID {
		return false
	}
	if q.DeploymentID != "" && ev.DeploymentID != q.DeploymentID {
		return false
	}
	if q.Source != "" && ev.Source != q.Source {
		return false
	}
	if q.TraceID != "" && ev.TraceID != q.TraceID {
		return false
	}
	if q.Level != "" && ev.Level != q.Level {
		return false
	}
	if q.Q != "" && !strings.Contains(strings.ToLower(ev.Message), strings.ToLower(q.Q)) {
		return false
	}
	return true
}

func FromStore(row store.OpsLog) Event {
	var meta map[string]any
	if row.MetadataJSON != "" && row.MetadataJSON != "{}" {
		_ = json.Unmarshal([]byte(row.MetadataJSON), &meta)
	}
	return Event{
		ID: row.ID, Timestamp: row.CreatedAt, Level: row.Level, Source: row.Source, Message: row.Message,
		TraceID: row.TraceID, UserID: row.UserID, DeviceID: row.DeviceID, ServiceID: row.ServiceID,
		Service: row.ServiceName, ReleaseID: row.ReleaseID, BuildID: row.BuildID, JobID: row.JobID,
		DeploymentID: row.DeploymentID, Metadata: meta,
	}
}
