package store

import (
	"context"
	"database/sql"
	"time"
)

const (
	PlanStatusReadyForApproval = "ready_for_approval"
	PlanStatusReady            = "ready"
	PlanStatusApproved         = "approved"
	PlanStatusExecuting        = "executing"
	PlanStatusSucceeded        = "succeeded"
	PlanStatusFailed           = "failed"
	PlanStatusDenied           = "denied"
	PlanStatusCancelled        = "cancelled"
	PlanStatusExpired          = "expired"

	PlanStepPending   = "pending"
	PlanStepRunning   = "running"
	PlanStepSucceeded = "succeeded"
	PlanStepFailed    = "failed"
	PlanStepDenied    = "denied"
	PlanStepSkipped   = "skipped"

	RiskRead     = "read"
	RiskLow      = "low"
	RiskMedium   = "medium"
	RiskHigh     = "high"
	RiskCritical = "critical"
)

type Plan struct {
	ID          string
	UserID      string
	Name        string
	Title       string
	Intent      string
	CreatedBy   string
	AISessionID string
	Actor       string
	TraceID     string
	RiskLevel   string
	Status      string
	InputJSON   string
	ResultJSON  string
	Error       string
	ApprovedBy  string
	ApprovedAt  *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
	ExpiresAt   time.Time
	FinishedAt  *time.Time
	Steps       []PlanStep
}

type PlanStep struct {
	ID         string
	PlanID     string
	Seq        int
	Name       string
	Title      string
	Status     string
	Scope      string
	RiskLevel  string
	Error      string
	OutputJSON string
	TraceID    string
	StartedAt  *time.Time
	FinishedAt *time.Time
}

func (s *Store) CreatePlan(ctx context.Context, p *Plan, steps []PlanStep) error {
	if p.ID == "" {
		p.ID = NewID()
	}
	now := time.Now().UTC()
	p.CreatedAt = now
	p.UpdatedAt = now
	if p.Status == "" {
		p.Status = PlanStatusReadyForApproval
	}
	if p.ResultJSON == "" {
		p.ResultJSON = "{}"
	}
	if p.InputJSON == "" {
		p.InputJSON = "{}"
	}
	return s.withTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO plans (
  id, user_id, name, title, intent, created_by, ai_session_id, actor, trace_id,
  risk_level, status, input_json, result_json, error, approved_by, approved_at,
  created_at, updated_at, expires_at, finished_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			p.ID, p.UserID, p.Name, p.Title, p.Intent, p.CreatedBy, p.AISessionID, p.Actor, p.TraceID,
			p.RiskLevel, p.Status, p.InputJSON, p.ResultJSON, p.Error, p.ApprovedBy, nilTime(p.ApprovedAt),
			now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), p.ExpiresAt.Format(time.RFC3339Nano), nilTime(p.FinishedAt),
		); err != nil {
			return err
		}
		for i := range steps {
			st := &steps[i]
			if st.ID == "" {
				st.ID = NewID()
			}
			st.PlanID = p.ID
			if st.Status == "" {
				st.Status = PlanStepPending
			}
			if st.OutputJSON == "" {
				st.OutputJSON = "{}"
			}
			if _, err := tx.ExecContext(ctx, `
INSERT INTO plan_steps (
  id, plan_id, seq, name, title, status, scope, risk_level, error, output_json, trace_id, started_at, finished_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				st.ID, p.ID, st.Seq, st.Name, st.Title, st.Status, st.Scope, st.RiskLevel, st.Error,
				st.OutputJSON, st.TraceID, nilTime(st.StartedAt), nilTime(st.FinishedAt),
			); err != nil {
				return err
			}
		}
		p.Steps = steps
		return nil
	})
}

func (s *Store) UpdatePlan(ctx context.Context, p *Plan) error {
	p.UpdatedAt = time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
UPDATE plans SET
  status = ?, error = ?, result_json = ?, approved_by = ?, approved_at = ?,
  updated_at = ?, finished_at = ?, actor = ?
WHERE id = ? AND user_id = ?`,
		p.Status, p.Error, p.ResultJSON, p.ApprovedBy, nilTime(p.ApprovedAt),
		p.UpdatedAt.Format(time.RFC3339Nano), nilTime(p.FinishedAt), p.Actor,
		p.ID, p.UserID)
	return err
}

func (s *Store) UpdatePlanStep(ctx context.Context, st *PlanStep) error {
	if st.OutputJSON == "" {
		st.OutputJSON = "{}"
	}
	_, err := s.db.ExecContext(ctx, `
UPDATE plan_steps SET
  status = ?, error = ?, output_json = ?, trace_id = ?, started_at = ?, finished_at = ?
WHERE id = ?`,
		st.Status, st.Error, st.OutputJSON, st.TraceID, nilTime(st.StartedAt), nilTime(st.FinishedAt), st.ID)
	return err
}

func (s *Store) GetPlan(ctx context.Context, userID, id string) (*Plan, error) {
	p, err := scanPlan(s.db.QueryRowContext(ctx, planSelect+` WHERE id = ? AND user_id = ?`, id, userID))
	if err != nil {
		return nil, err
	}
	steps, err := s.listPlanSteps(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	p.Steps = steps
	return p, nil
}

func (s *Store) ListPlans(ctx context.Context, userID string, limit int) ([]Plan, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, planSelect+` WHERE user_id = ? ORDER BY created_at DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Plan
	for rows.Next() {
		p, err := scanPlan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	if out == nil {
		out = []Plan{}
	}
	return out, rows.Err()
}

const planSelect = `
SELECT id, user_id, name, title, intent, created_by, ai_session_id, actor, trace_id,
  risk_level, status, input_json, result_json, error, approved_by, approved_at,
  created_at, updated_at, expires_at, finished_at
FROM plans`

type planRow interface {
	Scan(dest ...any) error
}

func scanPlan(row planRow) (*Plan, error) {
	var p Plan
	var approved, created, updated, expires, finished sql.NullString
	err := row.Scan(
		&p.ID, &p.UserID, &p.Name, &p.Title, &p.Intent, &p.CreatedBy, &p.AISessionID, &p.Actor, &p.TraceID,
		&p.RiskLevel, &p.Status, &p.InputJSON, &p.ResultJSON, &p.Error, &p.ApprovedBy, &approved,
		&created, &updated, &expires, &finished,
	)
	if err != nil {
		return nil, err
	}
	p.CreatedAt, _ = time.Parse(time.RFC3339Nano, created.String)
	p.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated.String)
	p.ExpiresAt, _ = time.Parse(time.RFC3339Nano, expires.String)
	if approved.Valid && approved.String != "" {
		t, _ := time.Parse(time.RFC3339Nano, approved.String)
		p.ApprovedAt = &t
	}
	if finished.Valid && finished.String != "" {
		t, _ := time.Parse(time.RFC3339Nano, finished.String)
		p.FinishedAt = &t
	}
	return &p, nil
}

func (s *Store) listPlanSteps(ctx context.Context, planID string) ([]PlanStep, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, plan_id, seq, name, title, status, scope, risk_level, error, output_json, trace_id, started_at, finished_at
FROM plan_steps WHERE plan_id = ? ORDER BY seq`, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PlanStep
	for rows.Next() {
		var st PlanStep
		var started, finished sql.NullString
		if err := rows.Scan(
			&st.ID, &st.PlanID, &st.Seq, &st.Name, &st.Title, &st.Status, &st.Scope, &st.RiskLevel,
			&st.Error, &st.OutputJSON, &st.TraceID, &started, &finished,
		); err != nil {
			return nil, err
		}
		if started.Valid && started.String != "" {
			t, _ := time.Parse(time.RFC3339Nano, started.String)
			st.StartedAt = &t
		}
		if finished.Valid && finished.String != "" {
			t, _ := time.Parse(time.RFC3339Nano, finished.String)
			st.FinishedAt = &t
		}
		out = append(out, st)
	}
	if out == nil {
		out = []PlanStep{}
	}
	return out, rows.Err()
}
