// Package mcp is a thin MCP compatibility layer over the Node API.
// It contains no storage, transfer, or transport logic — only tool routing to pkg/client.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/knot-infra/knot/pkg/client"
)

const (
	ToolDevicesList       = "devices.list"
	ToolStorageList       = "storage.list"
	ToolStorageStat       = "storage.stat"
	ToolStorageRead       = "storage.read"
	ToolStorageUpload     = "storage.upload"
	ToolStorageDownload   = "storage.download"
	ToolFilesSearch       = "files.search"
	ToolServicesList      = "services.list"
	ToolServicesRegister  = "services.register"
	ToolRoutesList        = "routes.list"
	ToolRoutesAdd         = "routes.add"
	ToolComputeList       = "compute.list"
	ToolComputeLabels     = "compute.labels"
	ToolJobsList          = "jobs.list"
	ToolJobsCreate        = "jobs.create"
	ToolJobsGet           = "jobs.get"
	ToolJobsCancel        = "jobs.cancel"
	ToolJobsLogs          = "jobs.logs"
	ToolJobsArtifacts     = "jobs.artifacts"
	ToolSecretsList       = "secrets.list"
	ToolEnvList           = "env.list"
	ToolDeployEnvironment = "deploy.environment"
	ToolSourceList        = "source.list"
	ToolBuildCreate       = "build.create"
	ToolBuildStatus       = "build.status"
	ToolBuildLogs         = "build.logs"
	ToolReleaseList       = "release.list"
	ToolReleaseStatus     = "release.status"
	ToolReleaseRollback   = "release.rollback"
	ToolTrafficStatus     = "traffic.status"
	ToolTrafficSwitch     = "traffic.switch"
	ToolLogsSearch        = "logs.search"
	ToolLogsTail          = "logs.tail"
	ToolLogsService       = "logs.service"
	ToolOpsContext        = "ops.context"
	ToolWorkflowList      = "workflow.list"
	ToolWorkflowRun       = "workflow.run"
	ToolWorkflowStatus    = "workflow.status"
	ToolAISession         = "ai.session"
	ToolAuditSearch       = "audit.search"
	ToolAuditAIActivity   = "audit.ai_activity"
	ToolAuditTrace        = "audit.trace"
	ToolPlanCreate        = "plan.create"
	ToolPlanStatus        = "plan.status"
	ToolPlanApprove       = "plan.approve"
	ToolPlanCancel        = "plan.cancel"
)

// Server exposes MCP tools that call the same Node API as the CLI.
type Server struct {
	Client *client.Client
	// WaitTimeout for upload/download transfers (0 = 2m).
	WaitTimeout time.Duration
	// MCPClient is stored on audit events (X-Knot-MCP-Client). Default knot-mcp.
	MCPClient string
}

type ToolDesc struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

func (s *Server) Tools() []ToolDesc {
	return []ToolDesc{
		{
			Name:        ToolDevicesList,
			Description: "List Node devices (requires devices.read)",
			InputSchema: schema(map[string]any{}),
		},
		{
			Name:        ToolStorageList,
			Description: "List entries under Home PC knot-storage (requires storage.read)",
			InputSchema: schema(map[string]any{
				"device_id": prop("string", "Storage host device id"),
				"path":      prop("string", "Relative path under storage root (optional)"),
			}, "device_id"),
		},
		{
			Name:        ToolStorageStat,
			Description: "Stat a storage path; files include sha256 (requires storage.read)",
			InputSchema: schema(map[string]any{
				"device_id": prop("string", "Storage host device id"),
				"path":      prop("string", "Relative path"),
			}, "device_id", "path"),
		},
		{
			Name:        ToolStorageRead,
			Description: "Download a storage file to a destination agent via Transfer (GET /v1/storage/read; requires storage.read)",
			InputSchema: schema(map[string]any{
				"device_id":    prop("string", "Storage host (source) device id"),
				"path":         prop("string", "Path under knot-storage"),
				"to_device_id": prop("string", "Destination agent device id"),
				"wait":         prop("boolean", "Wait for transfer completion (default true)"),
			}, "device_id", "path", "to_device_id"),
		},
		{
			Name:        ToolStorageUpload,
			Description: "Upload from a source agent outbox into Home storage via Transfer (requires storage.write)",
			InputSchema: schema(map[string]any{
				"device_id":      prop("string", "Storage host (dest) device id"),
				"path":           prop("string", "Dest path under knot-storage"),
				"from_device_id": prop("string", "Source agent device id"),
				"source_path":    prop("string", "Relative path in source outbox"),
				"size":           prop("integer", "File size in bytes"),
				"sha256":         prop("string", "Hex SHA-256"),
				"wait":           prop("boolean", "Wait for transfer completion (default true)"),
			}, "device_id", "path", "from_device_id", "source_path", "size", "sha256"),
		},
		{
			Name:        ToolStorageDownload,
			Description: "Alias of storage.read — download via Transfer (requires storage.read)",
			InputSchema: schema(map[string]any{
				"device_id":    prop("string", "Storage host (source) device id"),
				"path":         prop("string", "Path under knot-storage"),
				"to_device_id": prop("string", "Destination agent device id"),
				"wait":         prop("boolean", "Wait for transfer completion (default true)"),
			}, "device_id", "path", "to_device_id"),
		},
		{
			Name:        ToolFilesSearch,
			Description: "Search file metadata across all Node devices (name/path only; not full-text or AI). Requires storage.read.",
			InputSchema: schema(map[string]any{
				"q":               prop("string", "Substring match on name or path"),
				"device_id":       prop("string", "Limit to one node"),
				"type":            prop("string", "image | video | pdf | text | mime prefix"),
				"folder":          prop("string", "Browse this folder, or prefix when q is set"),
				"min_size":        prop("integer", "Minimum size in bytes"),
				"max_size":        prop("integer", "Maximum size in bytes"),
				"modified_after":  prop("string", "mtime lower bound"),
				"modified_before": prop("string", "mtime upper bound"),
			}),
		},
		{
			Name:        ToolServicesList,
			Description: "List registered services grouped by Node (metadata registry; requires services.read)",
			InputSchema: schema(map[string]any{
				"device_id": prop("string", "Limit to one node"),
			}),
		},
		{
			Name:        ToolServicesRegister,
			Description: "Register a service on a Node (does not start a process; requires services.write)",
			InputSchema: schema(map[string]any{
				"device_id": prop("string", "Host node id"),
				"name":      prop("string", "Slug, e.g. web-app"),
				"kind":      prop("string", "web | api | database | worker | other"),
				"port":      prop("integer", "Local port on the node"),
				"protocol":  prop("string", "http | https | tcp | udp"),
				"bind":      prop("string", "Local bind address (default 127.0.0.1)"),
			}, "device_id", "name", "port"),
		},
		{
			Name:        ToolRoutesList,
			Description: "List public Edge hostnames mapped to services (requires services.read)",
			InputSchema: schema(map[string]any{}),
		},
		{
			Name:        ToolRoutesAdd,
			Description: "Route a public hostname through the Edge onto a registered HTTP service (requires services.write). Does not open inbound ports on the home node.",
			InputSchema: schema(map[string]any{
				"hostname":       prop("string", "Public hostname, e.g. example.com"),
				"service_id":     prop("string", "Registered HTTP service id"),
				"edge_device_id": prop("string", "Optional VPS/edge node id (metadata)"),
			}, "hostname", "service_id"),
		},
		{
			Name:        ToolComputeList,
			Description: "List Compute Registry snapshots per Node (CPU/RAM/GPU/disks/labels). Snapshot, not live. Requires compute.read. Does not start jobs.",
			InputSchema: schema(map[string]any{
				"device_id": prop("string", "Optional: one device id"),
			}),
		},
		{
			Name:        ToolComputeLabels,
			Description: "Replace user labels on a compute node (merged with derived gpu/os/arch labels). Requires compute.write.",
			InputSchema: schema(map[string]any{
				"device_id": prop("string", "Device id"),
				"labels":    map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}, "description": "User labels, e.g. {\"location\":\"home\",\"trusted\":\"true\"}"},
			}, "device_id"),
		},
		{
			Name:        ToolJobsList,
			Description: "List one-shot compute jobs. Requires compute.read. Omit device_id to see all placements.",
			InputSchema: schema(map[string]any{
				"device_id": prop("string", "Optional: limit to one node"),
			}),
		},
		{
			Name:        ToolJobsCreate,
			Description: "Start a one-shot container job. Optional device_id pins the node; omit it to let the scheduler pick from the Compute Registry. Structured JobSpec only — no shell. Requires compute.write.",
			InputSchema: schema(map[string]any{
				"device_id":       prop("string", "Optional pin to one node; omit for scheduler placement"),
				"image":           prop("string", "OCI image, e.g. python:3.13 or knot-fake-job:ok"),
				"command":         map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Argv, not a shell string"},
				"cpu":             prop("number", "CPU quota (also a scheduler requirement)"),
				"memory_mb":       prop("integer", "RAM limit in MiB"),
				"gpu":             prop("integer", "GPU count; 1+ requires a GPU node (or pass gpu_required)"),
				"gpu_required":    prop("boolean", "Require a GPU node (same as gpu=1 if gpu is 0)"),
				"disk_mb":         prop("integer", "Output/workspace disk cap in MiB"),
				"pids":            prop("integer", "PID limit (default 256)"),
				"timeout_seconds": prop("integer", "Timeout (default 300, max 3600)"),
				"input_path":      prop("string", "Optional Storage path copied into jobs/{job_id}/input then /input"),
				"require":         map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}, "description": "Required device labels, e.g. {\"gpu\":\"true\"}"},
				"prefer":          map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}, "description": "Preferred device labels, e.g. {\"location\":\"home\"}"},
				"wait":            prop("boolean", "Wait until artifacts are committed (default false)"),
			}, "image"),
		},
		{
			Name:        ToolJobsGet,
			Description: "Get a compute job by id (status, exit code, resources, output_path). Requires compute.read.",
			InputSchema: schema(map[string]any{
				"id": prop("string", "Job id"),
			}, "id"),
		},
		{
			Name:        ToolJobsCancel,
			Description: "Cancel a running compute job. Requires compute.write.",
			InputSchema: schema(map[string]any{
				"id": prop("string", "Job id"),
			}, "id"),
		},
		{
			Name:        ToolJobsLogs,
			Description: "Get sanitized stdout/stderr lines for a compute job. Requires compute.read.",
			InputSchema: schema(map[string]any{
				"id":    prop("string", "Job id"),
				"limit": prop("integer", "Max lines (default 200)"),
			}, "id"),
		},
		{
			Name:        ToolJobsArtifacts,
			Description: "List committed job artifacts (Storage objects with sha256). Requires compute.read. Empty until status is artifacts_committed.",
			InputSchema: schema(map[string]any{
				"id": prop("string", "Job id"),
			}, "id"),
		},
		{
			Name:        ToolSecretsList,
			Description: "List secret metadata (id, name, version). Never returns secret values. Requires secrets.read.",
			InputSchema: schema(map[string]any{}),
		},
		{
			Name:        ToolEnvList,
			Description: "List application environments (vars + secret references, not values). Requires deploy.read.",
			InputSchema: schema(map[string]any{
				"project": prop("string", "Optional project/service slug"),
			}),
		},
		{
			Name:        ToolDeployEnvironment,
			Description: "Deploy a service using a named environment. Node injects secrets; do not pass secret values. Requires deploy.write.",
			InputSchema: schema(map[string]any{
				"device_id":      prop("string", "Target node"),
				"name":           prop("string", "Service name"),
				"image":          prop("string", "OCI image"),
				"port":           prop("integer", "Container listen port"),
				"environment":    prop("string", "Environment name, e.g. production"),
				"project":        prop("string", "Optional project if environments are scoped"),
				"health_path":    prop("string", "Health path (default /health)"),
				"hostname":       prop("string", "Optional public hostname"),
				"edge_device_id": prop("string", "Optional edge node for the hostname"),
			}, "device_id", "name", "image", "port", "environment"),
		},
		{
			Name:        ToolSourceList,
			Description: "List Git application sources (url/branch/revision). Tokens are vault references only. Requires source.read.",
			InputSchema: schema(map[string]any{}),
		},
		{
			Name:        ToolBuildCreate,
			Description: "Start a Dockerfile build on a pinned node. Does not deploy or change production. Requires build.write.",
			InputSchema: schema(map[string]any{
				"source_id":  prop("string", "Application source id"),
				"device_id":  prop("string", "Build node (pinned, like jobs)"),
				"tag":        prop("string", "Image tag to build and push, e.g. ghcr.io/user/app:v43"),
				"dockerfile": prop("string", "Dockerfile path relative to the repo (default Dockerfile)"),
				"context":    prop("string", "Build context relative to the repo (default .)"),
				"wait":       prop("boolean", "Wait for the build to finish (default false)"),
			}, "source_id", "device_id", "tag"),
		},
		{
			Name:        ToolBuildStatus,
			Description: "Get a build's status, image tag, and error. Requires build.read.",
			InputSchema: schema(map[string]any{
				"id": prop("string", "Build id"),
			}, "id"),
		},
		{
			Name:        ToolBuildLogs,
			Description: "Get sanitized build logs. Secrets are redacted. Requires build.read.",
			InputSchema: schema(map[string]any{
				"id":    prop("string", "Build id"),
				"limit": prop("integer", "Max lines (default 200)"),
			}, "id"),
		},
		{
			Name:        ToolReleaseList,
			Description: "List application releases (image, env pins, health status). Requires release.read. Does not deploy or switch traffic.",
			InputSchema: schema(map[string]any{
				"service": prop("string", "Optional service name"),
			}),
		},
		{
			Name:        ToolReleaseStatus,
			Description: "Get a release by id, or the latest release for a service (includes failed candidates; current is a field). Requires release.read. Does not roll back.",
			InputSchema: schema(map[string]any{
				"id":      prop("string", "Release id"),
				"service": prop("string", "Service name to inspect the latest release"),
			}),
		},
		{
			Name:        ToolReleaseRollback,
			Description: "Restore the previous verified release. Requires release.activate. Do not call unless the user explicitly asked to roll back.",
			InputSchema: schema(map[string]any{
				"id": prop("string", "Current release id to undo"),
			}, "id"),
		},
		{
			Name:        ToolTrafficStatus,
			Description: "Show Edge traffic binding for a hostname (active release, weights, history). Requires traffic.read. Does not switch traffic.",
			InputSchema: schema(map[string]any{
				"route":    prop("string", "Route id or hostname, e.g. example.com"),
				"hostname": prop("string", "Hostname alias of route"),
			}),
		},
		{
			Name:        ToolTrafficSwitch,
			Description: "Cut over Edge traffic to a health-passed release. Requires traffic.write. Do not call unless the user explicitly asked to switch production traffic.",
			InputSchema: schema(map[string]any{
				"route":      prop("string", "Route id or hostname"),
				"hostname":   prop("string", "Hostname alias of route"),
				"release_id": prop("string", "Active release to receive traffic"),
				"weight":     prop("integer", "Percent for this release (default 100; remainder stays on previous)"),
			}, "release_id"),
		},
		{
			Name:        ToolLogsSearch,
			Description: "Search unified operational logs (build, deploy, release, edge, job, agent, audit). Requires logs.read. Secrets are redacted. Does not mutate anything.",
			InputSchema: schema(map[string]any{
				"service":    prop("string", "Service name, e.g. web-app"),
				"release_id": prop("string", "Release id"),
				"build_id":   prop("string", "Build id"),
				"job_id":     prop("string", "Job id"),
				"source":     prop("string", "agent | deploy | build | edge | job | system | audit | release"),
				"trace_id":   prop("string", "Correlation id across build → release → edge"),
				"level":      prop("string", "info | error | warn"),
				"q":          prop("string", "Substring match on message"),
				"limit":      prop("integer", "Max lines (default 200)"),
			}),
		},
		{
			Name:        ToolLogsTail,
			Description: "Latest operational log lines (same filters as logs.search). Requires logs.read. Use after a known id to poll for new lines.",
			InputSchema: schema(map[string]any{
				"service":    prop("string", "Service name"),
				"release_id": prop("string", "Release id"),
				"source":     prop("string", "Log source"),
				"trace_id":   prop("string", "Correlation id"),
				"after":      prop("string", "Return lines after this log id"),
				"limit":      prop("integer", "Max lines (default 50)"),
			}),
		},
		{
			Name:        ToolLogsService,
			Description: "Operational logs for one service (deploy, release, edge, container). Requires logs.read.",
			InputSchema: schema(map[string]any{
				"service":    prop("string", "Service name, e.g. web-app"),
				"release_id": prop("string", "Optional release id"),
				"source":     prop("string", "Optional source filter"),
				"limit":      prop("integer", "Max lines (default 200)"),
			}, "service"),
		},
		{
			Name:        ToolOpsContext,
			Description: "Read-only operational snapshot for a service: current/latest release, environment, node, health, traffic weight, last deploy, recent errors, trace_id. Composes existing primitives. Does not mutate. Sections are included only when the credential has the matching read scope (services.read, release.read, traffic.read, deploy.read, logs.read). Do not use an admin token.",
			InputSchema: schema(map[string]any{
				"service":   prop("string", "Service name or id, e.g. web-app"),
				"device_id": prop("string", "Optional node id if the name exists on more than one device"),
				"name":      prop("string", "Alias of service"),
			}, "service"),
		},
		{
			Name:        ToolWorkflowList,
			Description: "List catalogued composite workflows and recent runs. A workflow is a composition of existing Node API primitives, not a new permission. Each step still checks its own scopes.",
			InputSchema: schema(map[string]any{}),
		},
		{
			Name:        ToolWorkflowRun,
			Description: "Run a catalogued workflow (diagnose-service, deploy-release, restore-backup). Does not grant new rights: each step checks its own scopes and stops on denial. deploy-release never switches traffic. restore-backup never deploys to production.",
			InputSchema: schema(map[string]any{
				"name":           prop("string", "diagnose-service | deploy-release | restore-backup"),
				"service":        prop("string", "Service name (diagnose / deploy)"),
				"device_id":      prop("string", "Target node id"),
				"image":          prop("string", "Image for deploy-release or restore job"),
				"build_id":       prop("string", "Optional completed build id"),
				"port":           prop("integer", "Origin port for deploy-release"),
				"hostname":       prop("string", "Hostname for diagnose / deploy"),
				"query":          prop("string", "files.search query for restore-backup (default backup)"),
				"path":           prop("string", "Explicit backup path"),
				"from_device_id": prop("string", "Source node for restore"),
				"to_device_id":   prop("string", "Destination node for restore transfer"),
				"to_path":        prop("string", "Destination path"),
				"job_image":      prop("string", "Compute image to unpack/restore the backup"),
			}, "name"),
		},
		{
			Name:        ToolWorkflowStatus,
			Description: "Get a workflow run and its steps (id, status, trace_id, per-step scopes and results).",
			InputSchema: schema(map[string]any{
				"id": prop("string", "Workflow run id"),
			}, "id"),
		},
		{
			Name:        ToolAISession,
			Description: "Read the current scoped AI session (actor/parent, scopes, expiry). Does not create or revoke sessions — a human must mint the credential and pass it to MCP.",
			InputSchema: schema(map[string]any{}),
		},
		{
			Name:        ToolAuditSearch,
			Description: "Search the Node audit log (requires audit.read). Returns who did what, including AI sessions, parent user, MCP client, workflow, and trace.",
			InputSchema: schema(map[string]any{
				"action":        prop("string", "Exact action, e.g. traffic.switch"),
				"actor_type":    prop("string", "user | ai_session | system"),
				"ai_session_id": prop("string", "Filter to one AI session"),
				"workflow_id":   prop("string", "Filter to one workflow run"),
				"trace_id":      prop("string", "Filter to one trace"),
				"mcp_client":    prop("string", "Filter by MCP client name"),
				"q":             prop("string", "Free-text match on action/actor/target"),
				"limit":         prop("integer", "Max events (default 50)"),
			}),
		},
		{
			Name:        ToolAuditAIActivity,
			Description: "AI Activity view: what AI sessions ran, on whose behalf, which workflow, and the result (requires audit.read). Not granted to AI sessions by default.",
			InputSchema: schema(map[string]any{
				"ai_session_id": prop("string", "Optional session id; omit for all AI agents today"),
				"limit":         prop("integer", "Max events to fold into activities"),
			}),
		},
		{
			Name:        ToolAuditTrace,
			Description: "List every audit event sharing a trace_id (requires audit.read).",
			InputSchema: schema(map[string]any{
				"trace_id": prop("string", "Trace id from an MCP call or workflow"),
			}, "trace_id"),
		},
		{
			Name:        ToolPlanCreate,
			Description: "Create a Plan (proposal only — does not mutate Node). High/critical plans need a human to call plan.approve. Infer name from intent or pass name=update-production|deploy-release|diagnose-service|restore-backup.",
			InputSchema: schema(map[string]any{
				"intent":      prop("string", "Natural-language intent, e.g. Обновить production"),
				"name":        prop("string", "Catalog name: diagnose-service, deploy-release, update-production, restore-backup"),
				"service":     prop("string", "Service name"),
				"device_id":   prop("string", "Target node"),
				"image":       prop("string", "Image tag"),
				"build_id":    prop("string", "Completed build id"),
				"port":        prop("integer", "Origin port"),
				"hostname":    prop("string", "Public hostname / route"),
				"environment": prop("string", "Environment name"),
				"ttl_minutes": prop("integer", "Proposal TTL (default 30)"),
				"expires_in":  prop("string", "TTL duration, e.g. 30m"),
			}),
		},
		{
			Name:        ToolPlanStatus,
			Description: "List plans or get one by id, including steps, risk_level, and status.",
			InputSchema: schema(map[string]any{
				"id": prop("string", "Plan id; omit to list"),
			}),
		},
		{
			Name:        ToolPlanApprove,
			Description: "Human/approval-credential only. Approves a plan and executes it step-by-step. AI sessions receive 403.",
			InputSchema: schema(map[string]any{
				"id": prop("string", "Plan id"),
			}, "id"),
		},
		{
			Name:        ToolPlanCancel,
			Description: "Cancel a plan that has not started executing. History is kept.",
			InputSchema: schema(map[string]any{
				"id": prop("string", "Plan id"),
			}, "id"),
		},
	}
}

func prop(typ, desc string) map[string]any {
	return map[string]any{"type": typ, "description": desc}
}

func schema(props map[string]any, required ...string) map[string]any {
	s := map[string]any{
		"type":       "object",
		"properties": props,
	}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

// Call invokes a tool by name. args is a JSON object.
func (s *Server) Call(ctx context.Context, name string, args map[string]any) (any, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("mcp: client not configured")
	}
	ctx = s.withCallContext(ctx)
	switch name {
	case ToolDevicesList:
		devs, err := s.Client.ListDevices(ctx)
		if err != nil {
			return nil, err
		}
		return map[string]any{"devices": devs}, nil
	case ToolStorageList:
		deviceID, _ := strArg(args, "device_id")
		path, _ := strArg(args, "path")
		ents, err := s.Client.StorageList(ctx, deviceID, path)
		if err != nil {
			return nil, err
		}
		return map[string]any{"entries": ents}, nil
	case ToolStorageStat:
		deviceID, err := requireStr(args, "device_id")
		if err != nil {
			return nil, err
		}
		path, err := requireStr(args, "path")
		if err != nil {
			return nil, err
		}
		st, err := s.Client.StorageStat(ctx, deviceID, path)
		if err != nil {
			return nil, err
		}
		return st, nil
	case ToolStorageUpload:
		return s.upload(ctx, args)
	case ToolStorageRead, ToolStorageDownload:
		return s.download(ctx, args)
	case ToolFilesSearch:
		return s.searchFiles(ctx, args)
	case ToolServicesList:
		return s.listServices(ctx, args)
	case ToolServicesRegister:
		return s.registerService(ctx, args)
	case ToolRoutesList:
		list, err := s.Client.ListRoutes(ctx)
		if err != nil {
			return nil, err
		}
		return map[string]any{"routes": list}, nil
	case ToolRoutesAdd:
		return s.addRoute(ctx, args)
	case ToolComputeList:
		if id, _ := args["device_id"].(string); id != "" {
			d, err := s.Client.GetComputeDevice(ctx, id)
			if err != nil {
				return nil, err
			}
			return d, nil
		}
		list, err := s.Client.ListComputeDevices(ctx)
		if err != nil {
			return nil, err
		}
		return map[string]any{"devices": list}, nil
	case ToolComputeLabels:
		deviceID, err := requireStr(args, "device_id")
		if err != nil {
			return nil, err
		}
		return s.Client.SetComputeLabels(ctx, deviceID, strMapArg(args, "labels"))
	case ToolJobsList:
		deviceID, _ := strArg(args, "device_id")
		list, err := s.Client.ListJobs(ctx, deviceID)
		if err != nil {
			return nil, err
		}
		return map[string]any{"jobs": list}, nil
	case ToolJobsCreate:
		return s.createJob(ctx, args)
	case ToolJobsGet:
		id, err := requireStr(args, "id")
		if err != nil {
			return nil, err
		}
		return s.Client.GetJob(ctx, id)
	case ToolJobsCancel:
		id, err := requireStr(args, "id")
		if err != nil {
			return nil, err
		}
		return s.Client.CancelJob(ctx, id)
	case ToolJobsLogs:
		id, err := requireStr(args, "id")
		if err != nil {
			return nil, err
		}
		limit := int(optInt64(args, "limit"))
		logs, err := s.Client.JobLogs(ctx, id, limit)
		if err != nil {
			return nil, err
		}
		return map[string]any{"logs": logs}, nil
	case ToolJobsArtifacts:
		id, err := requireStr(args, "id")
		if err != nil {
			return nil, err
		}
		arts, err := s.Client.JobArtifacts(ctx, id)
		if err != nil {
			return nil, err
		}
		return map[string]any{"artifacts": arts}, nil
	case ToolSecretsList:
		list, err := s.Client.ListSecrets(ctx)
		if err != nil {
			return nil, err
		}
		return map[string]any{"secrets": list}, nil
	case ToolEnvList:
		project, _ := strArg(args, "project")
		list, err := s.Client.ListEnvironments(ctx, project)
		if err != nil {
			return nil, err
		}
		return map[string]any{"environments": list}, nil
	case ToolDeployEnvironment:
		return s.deployEnvironment(ctx, args)
	case ToolSourceList:
		list, err := s.Client.ListSources(ctx)
		if err != nil {
			return nil, err
		}
		return map[string]any{"sources": list}, nil
	case ToolBuildCreate:
		return s.createBuild(ctx, args)
	case ToolBuildStatus:
		id, err := requireStr(args, "id")
		if err != nil {
			return nil, err
		}
		return s.Client.GetBuild(ctx, id)
	case ToolBuildLogs:
		id, err := requireStr(args, "id")
		if err != nil {
			return nil, err
		}
		limit := int(optInt64(args, "limit"))
		logs, err := s.Client.BuildLogs(ctx, id, limit)
		if err != nil {
			return nil, err
		}
		return map[string]any{"logs": logs}, nil
	case ToolReleaseList:
		service, _ := strArg(args, "service")
		list, err := s.Client.ListReleases(ctx, service)
		if err != nil {
			return nil, err
		}
		return map[string]any{"releases": list}, nil
	case ToolReleaseStatus:
		return s.releaseStatus(ctx, args)
	case ToolReleaseRollback:
		id, err := requireStr(args, "id")
		if err != nil {
			return nil, err
		}
		return s.Client.RollbackRelease(ctx, id)
	case ToolTrafficStatus:
		return s.trafficStatus(ctx, args)
	case ToolTrafficSwitch:
		return s.trafficSwitch(ctx, args)
	case ToolLogsSearch, ToolLogsTail:
		return s.logsSearch(ctx, args, name == ToolLogsTail)
	case ToolLogsService:
		return s.logsService(ctx, args)
	case ToolOpsContext:
		return s.opsContext(ctx, args)
	case ToolWorkflowList:
		return s.workflowList(ctx)
	case ToolWorkflowRun:
		return s.workflowRun(ctx, args)
	case ToolWorkflowStatus:
		return s.workflowStatus(ctx, args)
	case ToolAISession:
		return s.Client.CurrentAISession(ctx)
	case ToolAuditSearch:
		return s.auditSearch(ctx, args)
	case ToolAuditAIActivity:
		return s.auditAIActivity(ctx, args)
	case ToolAuditTrace:
		return s.auditTrace(ctx, args)
	case ToolPlanCreate:
		return s.planCreate(ctx, args)
	case ToolPlanStatus:
		return s.planStatus(ctx, args)
	case ToolPlanApprove:
		return s.planApprove(ctx, args)
	case ToolPlanCancel:
		return s.planCancel(ctx, args)
	default:
		return nil, fmt.Errorf("unknown tool %q", name)
	}
}

func (s *Server) withCallContext(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	name := s.MCPClient
	if name == "" {
		name = "knot-mcp"
	}
	ctx = client.WithMCPClient(ctx, name)
	if client.TraceIDFrom(ctx) == "" {
		ctx = client.WithTraceID(ctx, client.NewTraceID())
	}
	return ctx
}

func (s *Server) auditSearch(ctx context.Context, args map[string]any) (any, error) {
	q := client.AuditQuery{
		Action:      strArgOr(args, "action"),
		ActorType:   strArgOr(args, "actor_type"),
		AISessionID: strArgOr(args, "ai_session_id"),
		WorkflowID:  strArgOr(args, "workflow_id"),
		TraceID:     strArgOr(args, "trace_id"),
		MCPClient:   strArgOr(args, "mcp_client"),
		Q:           strArgOr(args, "q"),
		Limit:       int(optInt64(args, "limit")),
	}
	events, err := s.Client.SearchAudit(ctx, q)
	if err != nil {
		return nil, err
	}
	return map[string]any{"events": events}, nil
}

func (s *Server) auditAIActivity(ctx context.Context, args map[string]any) (any, error) {
	q := client.AuditQuery{
		AISessionID: strArgOr(args, "ai_session_id"),
		Limit:       int(optInt64(args, "limit")),
	}
	list, err := s.Client.AIActivity(ctx, q)
	if err != nil {
		return nil, err
	}
	return map[string]any{"activities": list}, nil
}

func (s *Server) auditTrace(ctx context.Context, args map[string]any) (any, error) {
	id, err := requireStr(args, "trace_id")
	if err != nil {
		return nil, err
	}
	events, err := s.Client.AuditTrace(ctx, id)
	if err != nil {
		return nil, err
	}
	return map[string]any{"trace_id": id, "events": events}, nil
}

func (s *Server) planCreate(ctx context.Context, args map[string]any) (any, error) {
	req := client.CreatePlanRequest{
		Intent: strArgOr(args, "intent"), Name: strArgOr(args, "name"),
		Service: strArgOr(args, "service"), DeviceID: strArgOr(args, "device_id"),
		Image: strArgOr(args, "image"), BuildID: strArgOr(args, "build_id"),
		Hostname: strArgOr(args, "hostname"), Environment: strArgOr(args, "environment"),
		Query: strArgOr(args, "query"), Path: strArgOr(args, "path"),
		FromDeviceID: strArgOr(args, "from_device_id"), ToDeviceID: strArgOr(args, "to_device_id"),
		ToPath: strArgOr(args, "to_path"), JobImage: strArgOr(args, "job_image"),
		ExpiresIn: strArgOr(args, "expires_in"), Port: int(optInt64(args, "port")),
		TTLMinutes: int(optInt64(args, "ttl_minutes")),
	}
	return s.Client.CreatePlan(ctx, req)
}

func (s *Server) planStatus(ctx context.Context, args map[string]any) (any, error) {
	if id := strArgOr(args, "id"); id != "" {
		return s.Client.GetPlan(ctx, id)
	}
	return s.Client.ListPlans(ctx)
}

func (s *Server) planApprove(ctx context.Context, args map[string]any) (any, error) {
	id, err := requireStr(args, "id")
	if err != nil {
		return nil, err
	}
	return s.Client.ApprovePlan(ctx, id)
}

func (s *Server) planCancel(ctx context.Context, args map[string]any) (any, error) {
	id, err := requireStr(args, "id")
	if err != nil {
		return nil, err
	}
	return s.Client.CancelPlan(ctx, id)
}

func (s *Server) upload(ctx context.Context, args map[string]any) (any, error) {
	deviceID, err := requireStr(args, "device_id")
	if err != nil {
		return nil, err
	}
	path, err := requireStr(args, "path")
	if err != nil {
		return nil, err
	}
	from, err := requireStr(args, "from_device_id")
	if err != nil {
		return nil, err
	}
	src, err := requireStr(args, "source_path")
	if err != nil {
		return nil, err
	}
	sum, err := requireStr(args, "sha256")
	if err != nil {
		return nil, err
	}
	size, err := intArg(args, "size")
	if err != nil {
		return nil, err
	}
	t, err := s.Client.StorageUploadOpts(ctx, deviceID, path, from, src, size, sum, boolArg(args, "resume", false))
	if err != nil {
		return nil, err
	}
	if boolArg(args, "wait", true) {
		return s.wait(ctx, t.ID)
	}
	return t, nil
}

func (s *Server) download(ctx context.Context, args map[string]any) (any, error) {
	deviceID, err := requireStr(args, "device_id")
	if err != nil {
		return nil, err
	}
	path, err := requireStr(args, "path")
	if err != nil {
		return nil, err
	}
	to, err := requireStr(args, "to_device_id")
	if err != nil {
		return nil, err
	}
	t, err := s.Client.StorageRead(ctx, deviceID, path, to)
	if err != nil {
		return nil, err
	}
	if boolArg(args, "wait", true) {
		return s.wait(ctx, t.ID)
	}
	return t, nil
}

func (s *Server) searchFiles(ctx context.Context, args map[string]any) (any, error) {
	q := client.FileSearchQuery{}
	if v, ok := strArg(args, "q"); ok {
		q.Query = v
	}
	if v, ok := strArg(args, "device_id"); ok {
		q.DeviceID = v
	}
	if v, ok := strArg(args, "type"); ok {
		q.Type = v
	}
	if v, ok := strArg(args, "folder"); ok {
		q.Folder = v
	}
	if v, ok := strArg(args, "modified_after"); ok {
		q.ModifiedAfter = v
	}
	if v, ok := strArg(args, "modified_before"); ok {
		q.ModifiedBefore = v
	}
	q.MinSize = optInt64(args, "min_size")
	q.MaxSize = optInt64(args, "max_size")
	hits, err := s.Client.FilesSearch(ctx, q)
	if err != nil {
		return nil, err
	}
	return map[string]any{"files": hits}, nil
}

func (s *Server) listServices(ctx context.Context, args map[string]any) (any, error) {
	deviceID, _ := strArg(args, "device_id")
	if deviceID == "" {
		nodes, err := s.Client.ServicesTree(ctx)
		if err != nil {
			return nil, err
		}
		return map[string]any{"nodes": nodes}, nil
	}
	list, err := s.Client.ListServices(ctx, deviceID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"services": list}, nil
}

func (s *Server) registerService(ctx context.Context, args map[string]any) (any, error) {
	deviceID, err := requireStr(args, "device_id")
	if err != nil {
		return nil, err
	}
	name, err := requireStr(args, "name")
	if err != nil {
		return nil, err
	}
	port, err := intArg(args, "port")
	if err != nil {
		return nil, err
	}
	kind, _ := strArg(args, "kind")
	proto, _ := strArg(args, "protocol")
	bind, _ := strArg(args, "bind")
	return s.Client.RegisterService(ctx, client.RegisterServiceRequest{
		DeviceID: deviceID,
		Name:     name,
		Kind:     kind,
		Protocol: proto,
		Port:     int(port),
		Bind:     bind,
	})
}

func (s *Server) deployEnvironment(ctx context.Context, args map[string]any) (any, error) {
	deviceID, err := requireStr(args, "device_id")
	if err != nil {
		return nil, err
	}
	name, err := requireStr(args, "name")
	if err != nil {
		return nil, err
	}
	image, err := requireStr(args, "image")
	if err != nil {
		return nil, err
	}
	port, err := intArg(args, "port")
	if err != nil {
		return nil, err
	}
	environment, err := requireStr(args, "environment")
	if err != nil {
		return nil, err
	}
	health, _ := strArg(args, "health_path")
	hostname, _ := strArg(args, "hostname")
	edgeID, _ := strArg(args, "edge_device_id")
	project, _ := strArg(args, "project")
	return s.Client.CreateDeployment(ctx, client.CreateDeploymentRequest{
		DeviceID: deviceID, Name: name, Image: image, Port: int(port),
		HealthPath: health, Hostname: hostname, EdgeDeviceID: edgeID,
		Environment: environment, Project: project,
	})
}

func (s *Server) createBuild(ctx context.Context, args map[string]any) (any, error) {
	sourceID, err := requireStr(args, "source_id")
	if err != nil {
		return nil, err
	}
	deviceID, err := requireStr(args, "device_id")
	if err != nil {
		return nil, err
	}
	tag, err := requireStr(args, "tag")
	if err != nil {
		return nil, err
	}
	dockerfile, _ := strArg(args, "dockerfile")
	contextDir, _ := strArg(args, "context")
	b, err := s.Client.CreateBuild(ctx, client.CreateBuildRequest{
		SourceID: sourceID, DeviceID: deviceID, Tag: tag,
		Dockerfile: dockerfile, Context: contextDir,
	})
	if err != nil {
		return nil, err
	}
	if boolArg(args, "wait", false) {
		timeout := s.WaitTimeout
		if timeout <= 0 {
			timeout = 10 * time.Minute
		}
		wctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		return s.Client.WaitBuild(wctx, b.ID, 100*time.Millisecond)
	}
	return b, nil
}

func (s *Server) releaseStatus(ctx context.Context, args map[string]any) (any, error) {
	if id, ok := strArg(args, "id"); ok && id != "" {
		return s.Client.GetRelease(ctx, id)
	}
	service, _ := strArg(args, "service")
	if service == "" {
		return nil, fmt.Errorf("id or service required")
	}
	list, err := s.Client.ListReleases(ctx, service)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, fmt.Errorf("no releases")
	}
	rel := list[0]
	return map[string]any{
		"id":                 rel.ID,
		"number":             rel.Number,
		"service":            rel.Service,
		"image":              rel.Image,
		"status":             rel.Status,
		"current":            rel.Current,
		"error":              rel.Error,
		"prev_release_id":    rel.PrevReleaseID,
		"rollback_available": rel.Current && rel.PrevReleaseID != "",
	}, nil
}

func routeArg(args map[string]any) (string, error) {
	if v, ok := strArg(args, "route"); ok && v != "" {
		return v, nil
	}
	if v, ok := strArg(args, "hostname"); ok && v != "" {
		return v, nil
	}
	return "", fmt.Errorf("route or hostname required")
}

func (s *Server) trafficStatus(ctx context.Context, args map[string]any) (any, error) {
	route, err := routeArg(args)
	if err != nil {
		return nil, err
	}
	return s.Client.GetRouteTraffic(ctx, route)
}

func (s *Server) trafficSwitch(ctx context.Context, args map[string]any) (any, error) {
	route, err := routeArg(args)
	if err != nil {
		return nil, err
	}
	rel, err := requireStr(args, "release_id")
	if err != nil {
		return nil, err
	}
	weight := int(optInt64(args, "weight"))
	return s.Client.SwitchRouteTraffic(ctx, route, rel, weight)
}

func (s *Server) opsContext(ctx context.Context, args map[string]any) (any, error) {
	svc, _ := strArg(args, "service")
	if svc == "" {
		svc, _ = strArg(args, "name")
	}
	if svc == "" {
		return nil, fmt.Errorf("missing required argument %q", "service")
	}
	deviceID, _ := strArg(args, "device_id")
	return s.Client.OpsContext(ctx, svc, deviceID)
}

func (s *Server) workflowList(ctx context.Context) (any, error) {
	return s.Client.ListWorkflows(ctx)
}

func (s *Server) workflowRun(ctx context.Context, args map[string]any) (any, error) {
	name, err := requireStr(args, "name")
	if err != nil {
		return nil, err
	}
	req := client.RunWorkflowRequest{Name: name}
	req.Service, _ = strArg(args, "service")
	req.DeviceID, _ = strArg(args, "device_id")
	req.Image, _ = strArg(args, "image")
	req.BuildID, _ = strArg(args, "build_id")
	req.Hostname, _ = strArg(args, "hostname")
	req.Environment, _ = strArg(args, "environment")
	req.Query, _ = strArg(args, "query")
	req.Path, _ = strArg(args, "path")
	req.FromDeviceID, _ = strArg(args, "from_device_id")
	req.ToDeviceID, _ = strArg(args, "to_device_id")
	req.ToPath, _ = strArg(args, "to_path")
	req.JobImage, _ = strArg(args, "job_image")
	req.Port = int(optInt64(args, "port"))
	return s.Client.RunWorkflow(ctx, req)
}

func (s *Server) workflowStatus(ctx context.Context, args map[string]any) (any, error) {
	id, err := requireStr(args, "id")
	if err != nil {
		return nil, err
	}
	return s.Client.GetWorkflow(ctx, id)
}

func logsQueryFromArgs(args map[string]any) client.ListLogsQuery {
	q := client.ListLogsQuery{}
	q.Service, _ = strArg(args, "service")
	q.ReleaseID, _ = strArg(args, "release_id")
	q.BuildID, _ = strArg(args, "build_id")
	q.JobID, _ = strArg(args, "job_id")
	q.Source, _ = strArg(args, "source")
	q.TraceID, _ = strArg(args, "trace_id")
	q.Level, _ = strArg(args, "level")
	q.Q, _ = strArg(args, "q")
	q.After, _ = strArg(args, "after")
	q.Limit = int(optInt64(args, "limit"))
	return q
}

func (s *Server) logsSearch(ctx context.Context, args map[string]any, tail bool) (any, error) {
	q := logsQueryFromArgs(args)
	if tail && q.Limit <= 0 {
		q.Limit = 50
	}
	logs, err := s.Client.ListLogs(ctx, q)
	if err != nil {
		return nil, err
	}
	return map[string]any{"logs": logs}, nil
}

func (s *Server) logsService(ctx context.Context, args map[string]any) (any, error) {
	name, err := requireStr(args, "service")
	if err != nil {
		return nil, err
	}
	q := logsQueryFromArgs(args)
	q.Service = name
	logs, err := s.Client.ListLogs(ctx, q)
	if err != nil {
		return nil, err
	}
	return map[string]any{"service": name, "logs": logs}, nil
}

func (s *Server) addRoute(ctx context.Context, args map[string]any) (any, error) {
	host, err := requireStr(args, "hostname")
	if err != nil {
		return nil, err
	}
	svc, err := requireStr(args, "service_id")
	if err != nil {
		return nil, err
	}
	edgeID, _ := strArg(args, "edge_device_id")
	return s.Client.CreateRoute(ctx, client.CreateRouteRequest{
		Hostname:     host,
		ServiceID:    svc,
		EdgeDeviceID: edgeID,
	})
}

func (s *Server) createJob(ctx context.Context, args map[string]any) (any, error) {
	deviceID, _ := strArg(args, "device_id")
	image, err := requireStr(args, "image")
	if err != nil {
		return nil, err
	}
	gpu := int(optInt64(args, "gpu"))
	if gpu == 0 && boolArg(args, "gpu_required", false) {
		gpu = 1
	}
	job, err := s.Client.CreateJob(ctx, client.CreateJobRequest{
		DeviceID: deviceID,
		Image:    image,
		Command:  strSliceArg(args, "command"),
		Resources: client.JobResources{
			CPU:      floatArg(args, "cpu"),
			MemoryMB: optInt64(args, "memory_mb"),
			GPU:      gpu,
			DiskMB:   optInt64(args, "disk_mb"),
			Pids:     optInt64(args, "pids"),
		},
		TimeoutSeconds: int(optInt64(args, "timeout_seconds")),
		InputPath:      strArgOr(args, "input_path"),
		OutputPath:     strArgOr(args, "output_path"),
		Require:        strMapArg(args, "require"),
		Prefer:         strMapArg(args, "prefer"),
	})
	if err != nil {
		return nil, err
	}
	if boolArg(args, "wait", false) {
		timeout := s.WaitTimeout
		if timeout <= 0 {
			timeout = 10 * time.Minute
		}
		wctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		return s.Client.WaitJob(wctx, job.ID, 100*time.Millisecond)
	}
	return job, nil
}

func strArgOr(args map[string]any, key string) string {
	v, _ := strArg(args, key)
	return v
}

func strSliceArg(args map[string]any, key string) []string {
	if args == nil {
		return nil
	}
	raw, ok := args[key]
	if !ok || raw == nil {
		return nil
	}
	switch t := raw.(type) {
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, v := range t {
			s := fmt.Sprint(v)
			if s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func strMapArg(args map[string]any, key string) map[string]string {
	if args == nil {
		return nil
	}
	raw, ok := args[key]
	if !ok || raw == nil {
		return nil
	}
	switch t := raw.(type) {
	case map[string]string:
		return t
	case map[string]any:
		out := map[string]string{}
		for k, v := range t {
			out[k] = fmt.Sprint(v)
		}
		return out
	default:
		return nil
	}
}

func floatArg(args map[string]any, key string) float64 {
	if args == nil {
		return 0
	}
	v, ok := args[key]
	if !ok || v == nil {
		return 0
	}
	switch t := v.(type) {
	case float64:
		return t
	case int:
		return float64(t)
	case int64:
		return float64(t)
	case json.Number:
		f, _ := t.Float64()
		return f
	case string:
		f, _ := strconv.ParseFloat(t, 64)
		return f
	default:
		return 0
	}
}

func (s *Server) wait(ctx context.Context, id string) (*client.Transfer, error) {
	timeout := s.WaitTimeout
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	wctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return s.Client.WaitTransfer(wctx, id, 100*time.Millisecond)
}

func strArg(args map[string]any, key string) (string, bool) {
	if args == nil {
		return "", false
	}
	v, ok := args[key]
	if !ok || v == nil {
		return "", false
	}
	switch t := v.(type) {
	case string:
		return t, true
	default:
		return fmt.Sprint(t), true
	}
}

func requireStr(args map[string]any, key string) (string, error) {
	v, ok := strArg(args, key)
	if !ok || v == "" {
		return "", fmt.Errorf("missing required argument %q", key)
	}
	return v, nil
}

func intArg(args map[string]any, key string) (int64, error) {
	v, ok := args[key]
	if !ok || v == nil {
		return 0, fmt.Errorf("missing required argument %q", key)
	}
	switch t := v.(type) {
	case float64:
		return int64(t), nil
	case int64:
		return t, nil
	case int:
		return int64(t), nil
	case json.Number:
		return t.Int64()
	case string:
		return strconv.ParseInt(t, 10, 64)
	default:
		return 0, fmt.Errorf("invalid %s", key)
	}
}

func optInt64(args map[string]any, key string) int64 {
	if args == nil {
		return 0
	}
	if _, ok := args[key]; !ok || args[key] == nil {
		return 0
	}
	n, err := intArg(args, key)
	if err != nil {
		return 0
	}
	return n
}

func boolArg(args map[string]any, key string, def bool) bool {
	v, ok := args[key]
	if !ok || v == nil {
		return def
	}
	b, ok := v.(bool)
	if !ok {
		return def
	}
	return b
}
