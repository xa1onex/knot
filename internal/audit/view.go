package audit

import (
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/knot-infra/knot/internal/store"
)

// EventView is the structural answer to "who did this?".
func EventView(e store.AuditEvent) map[string]any {
	m := map[string]any{
		"id":            e.ID,
		"action":        e.Action,
		"actor_type":    e.ActorType,
		"actor":         e.Actor,
		"actor_id":      e.ActorID,
		"parent":        e.ParentActor,
		"time":          e.CreatedAt.UTC().Format(time.RFC3339),
		"target":        firstNonEmpty(e.Target, e.Resource),
		"result":        e.Result,
		"trace_id":      e.TraceID,
		"ai_session_id": e.AISessionID,
		"mcp_client":    e.MCPClient,
		"workflow_id":   e.WorkflowID,
		"plan_id":       e.PlanID,
		"detail":        e.Detail,
	}
	if e.Action == "traffic.switch" || e.Action == "traffic.rollback" {
		route, release := parseArrow(e.Detail)
		if route == "" {
			route = e.Resource
		}
		m["route"] = route
		if release != "" {
			m["release"] = release
		}
	}
	return m
}

type ActivityStep struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Status string `json:"status"`
}

type Activity struct {
	Time       string         `json:"time"`
	ActorType  string         `json:"actor_type"`
	Actor      string         `json:"actor"`
	Parent     string         `json:"parent"`
	SessionID  string         `json:"ai_session_id"`
	MCPClient  string         `json:"mcp_client,omitempty"`
	WorkflowID string         `json:"workflow_id,omitempty"`
	TraceID    string         `json:"trace_id,omitempty"`
	Ran        string         `json:"ran,omitempty"`
	Service    string         `json:"service,omitempty"`
	Target     string         `json:"target,omitempty"`
	Steps      []ActivityStep `json:"steps,omitempty"`
	Result     string         `json:"result"`
	Action     string         `json:"action,omitempty"`
}

func BuildAIActivity(events []store.AuditEvent, workflows map[string]*store.Workflow) []Activity {
	if workflows == nil {
		workflows = map[string]*store.Workflow{}
	}
	type group struct {
		first store.AuditEvent
		steps []ActivityStep
		ran   string
		fin   string
	}
	order := []string{}
	byWF := map[string]*group{}
	var standalone []store.AuditEvent
	// events are newest-first; walk oldest-first so steps stay in run order.
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		if e.WorkflowID == "" {
			standalone = append(standalone, e)
			continue
		}
		g, ok := byWF[e.WorkflowID]
		if !ok {
			g = &group{first: e}
			byWF[e.WorkflowID] = g
			order = append(order, e.WorkflowID)
		}
		switch e.Action {
		case "workflow.run":
			g.ran = e.Detail
		case "workflow.step":
			st := ActivityStep{Name: e.Detail, Status: strings.ToLower(e.Result), OK: e.Result == "SUCCESS"}
			if e.Result == "DENIED" {
				st.Status = "denied"
			}
			g.steps = append(g.steps, st)
		case "workflow.finish":
			g.fin = e.Result
		}
	}
	out := make([]Activity, 0, len(order)+len(standalone))
	for _, id := range order {
		g := byWF[id]
		a := Activity{
			Time:       g.first.CreatedAt.UTC().Format(time.RFC3339),
			ActorType:  g.first.ActorType,
			Actor:      g.first.Actor,
			Parent:     g.first.ParentActor,
			SessionID:  g.first.AISessionID,
			MCPClient:  g.first.MCPClient,
			WorkflowID: id,
			TraceID:    g.first.TraceID,
			Ran:        g.ran,
			Steps:      g.steps,
			Result:     strings.ToLower(g.fin),
		}
		if wf := workflows[id]; wf != nil {
			if a.Ran == "" {
				a.Ran = wf.Name
			}
			a.TraceID = firstNonEmpty(a.TraceID, wf.TraceID)
			a.Service = jsonString(wf.InputJSON, "service")
			a.Target = a.Service
			if cause := jsonString(wf.ResultJSON, "cause"); cause != "" {
				a.Result = cause
			} else if wf.Error != "" {
				a.Result = wf.Error
			} else if a.Result == "" {
				a.Result = wf.Status
			}
			if len(a.Steps) == 0 {
				for _, st := range wf.Steps {
					a.Steps = append(a.Steps, ActivityStep{
						Name: st.Name, Status: st.Status,
						OK: st.Status == store.WorkflowStepSucceeded,
					})
				}
			}
		}
		out = append(out, a)
	}
	// standalone newest-first to match search order
	for i := len(standalone) - 1; i >= 0; i-- {
		e := standalone[i]
		out = append(out, Activity{
			Time:      e.CreatedAt.UTC().Format(time.RFC3339),
			ActorType: e.ActorType,
			Actor:     e.Actor,
			Parent:    e.ParentActor,
			SessionID: e.AISessionID,
			MCPClient: e.MCPClient,
			TraceID:   e.TraceID,
			Action:    e.Action,
			Target:    firstNonEmpty(e.Target, e.Resource),
			Result:    e.Result,
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Time > out[j].Time })
	return out
}

func parseArrow(detail string) (left, right string) {
	parts := strings.SplitN(detail, " → ", 2)
	if len(parts) != 2 {
		parts = strings.SplitN(detail, " -> ", 2)
	}
	if len(parts) != 2 {
		return strings.TrimSpace(detail), ""
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
}

func jsonString(raw, key string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return ""
	}
	v, _ := m[key].(string)
	return v
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
