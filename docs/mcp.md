# Stage 3.5 — External Client / MCP

## Goal

Prove Node is an **infrastructure layer**, not an AI product: any external client (Claude Code, Cursor, ChatGPT tools, Gemini, scripts) can use Home Storage through the same API and permissions as the CLI.

```text
External Client
       │
   MCP / API  (thin)
       │
       ▼
    Node API
       │
       ▼
 Storage → Transfer → Direct|Relay → Agent → ~/knot-storage/
```

No AI inside Node. No Storage or transport logic inside MCP.

## Auth

Same as CLI: `KNOT_API_TOKEN` (API credential or session). Scopes:

| Tool | Scope |
|------|--------|
| `devices.list` | `devices.read` |
| `storage.list` / `storage.stat` / `storage.read` / `storage.download` / `files.search` | `storage.read` |
| `storage.upload` | `storage.write` |
| `services.list` | `services.read` |
| `services.register` | `services.write` |
| `routes.list` | `services.read` |
| `routes.add` | `services.write` |
| `compute.list` | `compute.read` |
| `compute.labels` | `compute.write` |
| `jobs.list` / `jobs.get` / `jobs.logs` / `jobs.artifacts` | `compute.read` |
| `jobs.create` / `jobs.cancel` | `compute.write` |
| `secrets.list` | `secrets.read` |
| `env.list` | `deploy.read` |
| `deploy.environment` | `deploy.write` |
| `source.list` | `source.read` |
| `build.status` / `build.logs` | `build.read` |
| `build.create` | `build.write` |
| `release.list` / `release.status` | `release.read` |
| `release.rollback` | `release.activate` |
| `traffic.status` | `traffic.read` |
| `traffic.switch` | `traffic.write` |
| `logs.search` / `logs.tail` / `logs.service` | `logs.read` |
| `ops.context` | any of `services.read`, `release.read`, `traffic.read`, `deploy.read`, `logs.read` (sections omitted if missing) |
| `workflow.list` / `workflow.status` | any of the scopes used by catalogued steps (same as run) |
| `workflow.run` | no new scope — each step checks its own grant; missing scope denies/skips that step and stops required ones |
| `ai.session` | current scoped AI session (read-only; humans mint via API/CLI) |
| `audit.search` / `audit.ai_activity` / `audit.trace` | `audit.read` (not granted to AI sessions by default) |
| `plan.create` / `plan.status` / `plan.cancel` | any valid user or AI session (create does not mutate) |
| `plan.approve` | human session or API credential — **403 for AI sessions** |

Waiting on upload/download polls `GET /v1/transfers/{id}`, which accepts `network.transfer` **or** `storage.read` / `storage.write` (so MCP credentials do not need a separate transfer scope).

Insufficient scope → **403**. Revoked credential → **401**.

## Tools

| Tool | Node API |
|------|----------|
| `devices.list` | `GET /v1/devices` |
| `storage.list` | `GET /v1/storage/list` |
| `storage.stat` | `GET /v1/storage/stat` |
| `storage.read` | `GET /v1/storage/read` (+ wait) |
| `storage.download` | alias of `storage.read` |
| `storage.upload` | `POST /v1/storage/upload` (+ wait) |
| `files.search` | `GET /v1/files/search` (metadata only; not AI / not full-text) |
| `services.list` | `GET /v1/services/tree` (or `/v1/services?device_id=`) |
| `services.register` | `POST /v1/services` |
| `routes.list` | `GET /v1/routes` |
| `routes.add` | `POST /v1/routes` |
| `compute.list` | `GET /v1/compute/devices` (or one device) |
| `compute.labels` | `PUT /v1/compute/devices/{id}/labels` |
| `jobs.list` | `GET /v1/compute/jobs` |
| `jobs.create` | `POST /v1/compute/jobs` (`device_id` optional; scheduler places if omitted; optional wait) |
| `jobs.get` | `GET /v1/compute/jobs/{id}` |
| `jobs.cancel` | `POST /v1/compute/jobs/{id}/cancel` |
| `jobs.logs` | `GET /v1/compute/jobs/{id}/logs` |
| `jobs.artifacts` | `GET /v1/compute/jobs/{id}/artifacts` |
| `secrets.list` | `GET /v1/secrets` (metadata only) |
| `env.list` | `GET /v1/environments` |
| `deploy.environment` | `POST /v1/deployments` with `environment` (Node injects secrets) |
| `source.list` | `GET /v1/sources` |
| `build.create` | `POST /v1/builds` (pinned `device_id`; optional wait) |
| `build.status` | `GET /v1/builds/{id}` |
| `build.logs` | `GET /v1/builds/{id}/logs` |
| `release.list` | `GET /v1/releases` |
| `release.status` | `GET /v1/releases/{id}` or latest for a service |
| `release.rollback` | `POST /v1/releases/{id}/rollback` (explicit user request only) |
| `traffic.status` | `GET /v1/routes/{id}/traffic` |
| `traffic.switch` | `POST /v1/routes/{id}/switch` (explicit user request only) |
| `logs.search` | `GET /v1/logs` |
| `logs.tail` | `GET /v1/logs` (latest / `after=`) |
| `logs.service` | `GET /v1/logs?service=` |
| `ops.context` | `GET /v1/ops/context` |
| `workflow.list` | `GET /v1/workflows` |
| `workflow.run` | `POST /v1/workflows/run` |
| `workflow.status` | `GET /v1/workflows/{id}` |
| `ai.session` | `GET /v1/ai/sessions/current` |
| `audit.search` | `GET /v1/audit` |
| `audit.ai_activity` | `GET /v1/audit/ai` |
| `audit.trace` | `GET /v1/audit/trace/{id}` |
| `plan.create` | `POST /v1/plans` |
| `plan.status` | `GET /v1/plans` or `GET /v1/plans/{id}` |
| `plan.approve` | `POST /v1/plans/{id}/approve` |
| `plan.cancel` | `POST /v1/plans/{id}/cancel` |

Bytes still move only via Transfer (Direct/Relay). MCP never opens files on the Home PC itself.

## Run

```bash
export KNOT_API_URL=http://127.0.0.1:8787
export KNOT_API_TOKEN=<credential with devices.read,storage.read,storage.write,services.read,services.write>

# MCP stdio (Cursor / Claude Code)
knot-mcp

# Script / debug
knot-mcp tools
knot-mcp call devices.list
knot-mcp call storage.list '{"device_id":"...","path":"shared"}'
knot-mcp call storage.stat '{"device_id":"...","path":"shared/test.txt"}'
knot-mcp call storage.upload '{"device_id":"<home>","path":"shared/f.txt","from_device_id":"<vps>","source_path":"f.txt","size":N,"sha256":"..."}'
knot-mcp call storage.download '{"device_id":"<home>","path":"shared/f.txt","to_device_id":"<vps>"}'
knot-mcp call files.search '{"q":"logo"}'
knot-mcp call services.list
knot-mcp call services.register '{"device_id":"<home>","name":"web-app","kind":"web","port":3000}'
knot-mcp call routes.add '{"hostname":"example.com","service_id":"<web-app>"}'
knot-mcp call compute.list
knot-mcp call compute.labels '{"device_id":"<home>","labels":{"location":"home"}}'
knot-mcp call jobs.create '{"image":"python:3.13","cpu":8,"memory_mb":16384,"gpu_required":true}'
knot-mcp call jobs.create '{"device_id":"<home>","image":"python:3.13","cpu":2,"memory_mb":512}'
knot-mcp call jobs.list
knot-mcp call jobs.logs '{"id":"<job-id>"}'
knot-mcp call jobs.artifacts '{"id":"<job-id>"}'
knot-mcp call env.list
knot-mcp call secrets.list
knot-mcp call deploy.environment '{"device_id":"<home>","name":"web-app","image":"myapp:v42","port":3000,"environment":"production"}'
knot-mcp call release.list '{"service":"web-app"}'
knot-mcp call release.status '{"service":"web-app"}'
knot-mcp call traffic.status '{"hostname":"example.com"}'
knot-mcp call logs.service '{"service":"web-app"}'
knot-mcp call logs.search '{"trace_id":"abc123"}'
knot-mcp call ops.context '{"service":"web-app"}'
knot-mcp call workflow.run '{"name":"diagnose-service","service":"web-app"}'
knot-mcp call workflow.status '{"id":"<workflow-id>"}'
knot-mcp call ai.session
knot-mcp call audit.ai_activity
knot-mcp call audit.search '{"action":"traffic.switch"}'
knot-mcp call audit.trace '{"trace_id":"<id>"}'
knot-mcp call plan.create '{"intent":"Обновить production","service":"web-app","image":"app:v44","hostname":"example.com"}'
knot-mcp call plan.status '{"id":"<plan-id>"}'
```

### Cursor example (`mcp.json`)

```json
{
  "mcpServers": {
    "node": {
      "command": "knot-mcp",
      "env": {
        "KNOT_API_URL": "http://127.0.0.1:8787",
        "KNOT_API_TOKEN": "<token>"
      }
    }
  }
}
```

## Criterion

External client can: list devices → list Home storage → stat/read file → upload → download → SHA-256 matches — without knowing NAT, QUIC, relay, or that files live on a Windows PC.
