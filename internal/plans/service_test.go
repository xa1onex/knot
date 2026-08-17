package plans

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/knot-infra/knot/internal/audit"
	"github.com/knot-infra/knot/internal/auth"
	"github.com/knot-infra/knot/internal/store"
)

func testPlans(t *testing.T) (*Service, string) {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/knot.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	user, err := st.CreateUser(context.Background(), "admin@node.local", "x")
	if err != nil {
		t.Fatal(err)
	}
	svc := New(st, nil, nil, nil, nil, nil, nil, nil, nil, nil, &audit.Logger{Store: st})
	svc.Store = st
	return svc, user.ID
}

func TestCreatePlanDoesNotMutate(t *testing.T) {
	s, userID := testPlans(t)
	ctx := context.Background()
	before, err := s.Store.ListReleases(ctx, userID, "web-app")
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.Create(ctx, CreateRequest{
		UserID: userID, Actor: "ai-session:Deploy-Agent", Kind: auth.KindAI, CredID: "sess-1",
		Email: "admin@node.local", Intent: "Обновить production", Name: NameUpdate,
		Service: "web-app", Image: "app:v44", Hostname: "example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != store.PlanStatusReadyForApproval || p.RiskLevel != store.RiskCritical {
		t.Fatalf("status=%s risk=%s", p.Status, p.RiskLevel)
	}
	if len(p.Steps) != 5 || p.Steps[4].Name != "traffic.switch" {
		t.Fatalf("steps=%+v", p.Steps)
	}
	after, _ := s.Store.ListReleases(ctx, userID, "web-app")
	if len(before) != len(after) {
		t.Fatal("create must not create a release")
	}
}

func TestReadPlanNoApproval(t *testing.T) {
	s, userID := testPlans(t)
	p, err := s.Create(context.Background(), CreateRequest{
		UserID: userID, Actor: "ai", Kind: auth.KindAI, CredID: "s",
		Name: NameDiagnose, Service: "web-app",
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != store.PlanStatusReady || NeedsApproval(p.RiskLevel) {
		t.Fatalf("diagnose should be ready: %+v", p)
	}
}

func TestAICannotApprove(t *testing.T) {
	s, userID := testPlans(t)
	ctx := context.Background()
	p, err := s.Create(ctx, CreateRequest{
		UserID: userID, Actor: "ai", Kind: auth.KindAI, CredID: "s",
		Name: NameUpdate, Service: "web-app", Image: "app:v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Approve(ctx, ActorRequest{UserID: userID, Actor: "ai", Kind: auth.KindAI, Can: func(string) bool { return true }}, p.ID)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("AI approve: %v", err)
	}
	_, err = s.Execute(ctx, ActorRequest{UserID: userID, Actor: "ai", Kind: auth.KindAI, Can: func(string) bool { return true }}, p.ID)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("AI execute critical: %v", err)
	}
}

func TestCancelAndExpire(t *testing.T) {
	s, userID := testPlans(t)
	ctx := context.Background()
	p, err := s.Create(ctx, CreateRequest{
		UserID: userID, Actor: "ai", Kind: auth.KindAI, Name: NameUpdate, Service: "web-app", Image: "x",
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Cancel(ctx, userID, p.ID, "human")
	if err != nil || got.Status != store.PlanStatusCancelled {
		t.Fatalf("cancel: %+v %v", got, err)
	}
	short, err := s.Create(ctx, CreateRequest{
		UserID: userID, Actor: "ai", Kind: auth.KindAI, Name: NameUpdate, Service: "web-app", Image: "x",
		ExpiresIn: "1s",
	})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(1200 * time.Millisecond)
	_, err = s.Execute(ctx, ActorRequest{UserID: userID, Actor: "human", Kind: auth.KindUser, Can: func(string) bool { return true }}, short.ID)
	if !errors.Is(err, ErrExpired) {
		t.Fatalf("expired: %v", err)
	}
}

func TestExecuteChecksScopes(t *testing.T) {
	s, userID := testPlans(t)
	ctx := context.Background()
	p, err := s.Create(ctx, CreateRequest{
		UserID: userID, Actor: "human", Kind: auth.KindUser, Name: NameDiagnose, Service: "web-app",
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Execute(ctx, ActorRequest{UserID: userID, Actor: "human", Kind: auth.KindUser, Can: func(string) bool { return false }}, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.PlanStatusDenied {
		t.Fatalf("status=%s err=%s", got.Status, got.Error)
	}
	if got.Steps[0].Status != store.PlanStepDenied {
		t.Fatalf("step=%+v", got.Steps[0])
	}
}
