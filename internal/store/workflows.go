package store

import (
	"context"
	"database/sql"
	"time"
)

const (
	WorkflowStatusPending   = "pending"
	WorkflowStatusRunning   = "running"
	WorkflowStatusSucceeded = "succeeded"
	WorkflowStatusFailed    = "failed"
	WorkflowStatusDenied    = "denied"

	WorkflowStepPending   = "pending"
	WorkflowStepRunning   = "running"
	WorkflowStepSucceeded = "succeeded"
	WorkflowStepFailed    = "failed"
	WorkflowStepDenied    = "denied"
	WorkflowStepSkipped   = "skipped"
)

type Workflow struct {
	ID         string
	UserID     string
	Name       string
	Title      string
	Actor      string
	Status     string
	TraceID    string
	Error      string
	ResultJSON string
	InputJSON  string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	FinishedAt *time.Time
	Steps      []WorkflowStep
}

type WorkflowStep struct {
	ID         string
	WorkflowID string
	Seq        int
	Name       string
	Status     string
	Scope      string
	Error      string
	OutputJSON string
	TraceID    string
	StartedAt  *time.Time
	FinishedAt *time.Time
}

func (s *Store) CreateWorkflow(ctx context.Context, wf *Workflow, steps []WorkflowStep) error {
	if wf.ID == "" {
		wf.ID = NewID()
	}
	now := time.Now().UTC()
	wf.CreatedAt = now
	wf.UpdatedAt = now
	if wf.Status == "" {
		wf.Status = WorkflowStatusPending
	}
	if wf.ResultJSON == "" {
		wf.ResultJSON = "{}"
	}
	if wf.InputJSON == "" {
		wf.InputJSON = "{}"
	}
	return s.withTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO workflows (
  id, user_id, name, title, actor, status, trace_id, error, result_json, input_json, created_at, updated_at, finished_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			wf.ID, wf.UserID, wf.Name, wf.Title, wf.Actor, wf.Status, wf.TraceID, wf.Error,
			wf.ResultJSON, wf.InputJSON, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), nil); err != nil {
			return err
		}
		for i := range steps {
			st := &steps[i]
			if st.ID == "" {
				st.ID = NewID()
			}
			st.WorkflowID = wf.ID
			if st.Status == "" {
				st.Status = WorkflowStepPending
			}
			if st.OutputJSON == "" {
				st.OutputJSON = "{}"
			}
			if st.Seq == 0 {
				st.Seq = i + 1
			}
			if _, err := tx.ExecContext(ctx, `
INSERT INTO workflow_steps (
  id, workflow_id, seq, name, status, scope, error, output_json, trace_id, started_at, finished_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				st.ID, wf.ID, st.Seq, st.Name, st.Status, st.Scope, st.Error, st.OutputJSON, st.TraceID, nil, nil); err != nil {
				return err
			}
		}
		wf.Steps = steps
		return nil
	})
}

func (s *Store) UpdateWorkflow(ctx context.Context, wf *Workflow) error {
	wf.UpdatedAt = time.Now().UTC()
	var finished any
	if wf.FinishedAt != nil {
		finished = wf.FinishedAt.UTC().Format(time.RFC3339Nano)
	}
	if wf.ResultJSON == "" {
		wf.ResultJSON = "{}"
	}
	_, err := s.db.ExecContext(ctx, `
UPDATE workflows SET status=?, error=?, result_json=?, updated_at=?, finished_at=? WHERE id=? AND user_id=?`,
		wf.Status, wf.Error, wf.ResultJSON, wf.UpdatedAt.Format(time.RFC3339Nano), finished, wf.ID, wf.UserID)
	return err
}

func (s *Store) UpdateWorkflowStep(ctx context.Context, st *WorkflowStep) error {
	if st.OutputJSON == "" {
		st.OutputJSON = "{}"
	}
	var started, finished any
	if st.StartedAt != nil {
		started = st.StartedAt.UTC().Format(time.RFC3339Nano)
	}
	if st.FinishedAt != nil {
		finished = st.FinishedAt.UTC().Format(time.RFC3339Nano)
	}
	_, err := s.db.ExecContext(ctx, `
UPDATE workflow_steps SET status=?, scope=?, error=?, output_json=?, trace_id=?, started_at=?, finished_at=?
WHERE id=? AND workflow_id=?`,
		st.Status, st.Scope, st.Error, st.OutputJSON, st.TraceID, started, finished, st.ID, st.WorkflowID)
	return err
}

func (s *Store) GetWorkflow(ctx context.Context, userID, id string) (*Workflow, error) {
	row := s.db.QueryRowContext(ctx, workflowSelect+` WHERE id = ? AND user_id = ?`, id, userID)
	wf, err := scanWorkflow(row)
	if err != nil {
		return nil, err
	}
	steps, err := s.ListWorkflowSteps(ctx, wf.ID)
	if err != nil {
		return nil, err
	}
	wf.Steps = steps
	return wf, nil
}

func (s *Store) ListWorkflows(ctx context.Context, userID string, limit int) ([]Workflow, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx, workflowSelect+` WHERE user_id = ? ORDER BY created_at DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Workflow
	for rows.Next() {
		wf, err := scanWorkflow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *wf)
	}
	if out == nil {
		out = []Workflow{}
	}
	return out, rows.Err()
}

func (s *Store) ListWorkflowSteps(ctx context.Context, workflowID string) ([]WorkflowStep, error) {
	rows, err := s.db.QueryContext(ctx, workflowStepSelect+` WHERE workflow_id = ? ORDER BY seq ASC`, workflowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WorkflowStep
	for rows.Next() {
		st, err := scanWorkflowStep(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *st)
	}
	if out == nil {
		out = []WorkflowStep{}
	}
	return out, rows.Err()
}

const workflowSelect = `
SELECT id, user_id, name, title, actor, status, trace_id, error, result_json, input_json, created_at, updated_at, finished_at
FROM workflows`

const workflowStepSelect = `
SELECT id, workflow_id, seq, name, status, scope, error, output_json, trace_id, started_at, finished_at
FROM workflow_steps`

type workflowScanner interface {
	Scan(dest ...any) error
}

func scanWorkflow(row workflowScanner) (*Workflow, error) {
	var wf Workflow
	var created, updated string
	var finished sql.NullString
	err := row.Scan(&wf.ID, &wf.UserID, &wf.Name, &wf.Title, &wf.Actor, &wf.Status, &wf.TraceID, &wf.Error,
		&wf.ResultJSON, &wf.InputJSON, &created, &updated, &finished)
	if err != nil {
		return nil, err
	}
	wf.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	wf.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	if finished.Valid && finished.String != "" {
		t, _ := time.Parse(time.RFC3339Nano, finished.String)
		wf.FinishedAt = &t
	}
	return &wf, nil
}

func scanWorkflowStep(row workflowScanner) (*WorkflowStep, error) {
	var st WorkflowStep
	var started, finished sql.NullString
	err := row.Scan(&st.ID, &st.WorkflowID, &st.Seq, &st.Name, &st.Status, &st.Scope, &st.Error,
		&st.OutputJSON, &st.TraceID, &started, &finished)
	if err != nil {
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
	return &st, nil
}
