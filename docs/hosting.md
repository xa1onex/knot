# Stage 7 — Infrastructure / Hosting

**Status:** **Done** — **7.1–7.6**. Hosting foundation is production-hardened. Compute: **8.1–8.5 done** (see `docs/compute.md`).

Public VPS looks like a normal server on the internet. Actual work can run on a Home PC behind NAT, with no inbound ports.

```text
Internet
   │
   ▼
example.com
   │
   ▼
VPS #3  (Public Edge)
   │
   │ Node tunnel (outbound from Home PC)
   ▼
Home PC
   │
   └── 127.0.0.1:3000  web-app
```

Control Plane still does not store the workload itself — same rule as files: it knows **where**, not the bytes/process. It also does **not** start the process (that is 7.4).

## Substages

| Stage | Focus | Status |
|-------|--------|--------|
| **7.1** | Service Registry | **Done** |
| **7.2** | Edge / Reverse Proxy | **Done** |
| **7.3** | Tunnel Reliability & Streaming | **Done** |
| **7.4** | Service Deployment | **Done** |
| **7.5** | End-to-End TLS | **Done** |
| **7.6** | Production Hardening | **Done** |

## Stage 7.1 — Service Registry (done)

Node records which services belong on which node:

```text
Home PC
 ├── web-app     http://127.0.0.1:3000
 ├── api         http://127.0.0.1:8080
 └── postgres    tcp://127.0.0.1:5432
```

This is **declared metadata**. Registering a service does not start a process, open a public port, or proxy traffic. Default bind is `127.0.0.1` so the node is not advertised as internet-reachable.

### API

```http
GET    /v1/services
GET    /v1/services/tree
POST   /v1/services          { device_id, name, kind, port, protocol?, bind? }
GET    /v1/services/{id}
PATCH  /v1/services/{id}
DELETE /v1/services/{id}
```

Scopes: `services.read` / `services.write`.

`kind`: `web` | `api` | `database` | `worker` | `other`  
`protocol`: `http` | `https` | `tcp` | `udp` (database defaults to `tcp`)  
`status` in 7.1: `registered`

### CLI

```bash
knot services tree
knot services add --device <id> --name web-app --kind web --port 3000
knot services ls --device <id>
knot services rm <id>
```

### MCP

`services.list` / `services.register` — thin over the same API. No deploy, no proxy.

### Criterion (tested)

```text
Home PC
 ├── web-app
 ├── api
 └── postgres
```

Duplicate name on the same node → conflict. Same name on another node is allowed. Read-only credential cannot register.

## Stage 7.2 — Edge / Reverse Proxy (done)

Public hostname is routed onto a registered HTTP service. Home PC does not need a public IP, port forwarding, or a persistent inbound port.

```text
Internet
  ↓ HTTPS
VPS Edge  (TLS termination on knotd)
  ↓ secure Node tunnel  (existing outbound agent WebSocket)
Home PC
  ↓ HTTP
127.0.0.1:3000  web-app
```

The Control Plane looks up `Host: example.com` → `service:web-app` → Home PC, then streams the HTTP request/response over the agent socket the Home PC already opened. The agent dials **only** `127.0.0.1:<port>`. Edge never dials `192.168.x.x`.

Chunked HTTP over the tunnel with ACK-based backpressure (Stage 7.3). End-to-end TLS to the origin is 7.5.

### Health

`GET /v1/services/{id}/health` distinguishes:

| Flag | Meaning |
|------|---------|
| `registered` | Row exists in the service registry |
| `agent_online` | Home PC agent WebSocket is connected |
| `tunnel_connected` | Same as agent online in 7.2 (the WS *is* the tunnel) |
| `backend_reachable` | Agent could TCP-connect `127.0.0.1:<port>` |
| `edge_device_name` | Declared Edge node (e.g. VPS #3); listener is still knotd |

### API

```http
GET    /v1/routes
POST   /v1/routes            { hostname, service_id, edge_device_id? }
DELETE /v1/routes/{id}
GET    /v1/services/{id}/health
```

HTTP/HTTPS services only. TCP (postgres) cannot be an HTTP Edge route.

A matching public `Host` sends **all** paths (including `/v1`) to the origin so the Node API is not exposed as that site.

### CLI

```bash
knot routes add --host example.com --service <id> --edge <vps-id>
knot routes ls
knot services health <id>
```

### MCP

`routes.list` / `routes.add` — thin over the same API.

### Criterion (tested)

```bash
curl https://example.com
# → body from Home PC 127.0.0.1:<port>
```

Home PC: no port forwarding, no public inbound port, no public IP.

If the Home agent disconnects (Wi-Fi / NAT) while the origin still listens on loopback, Edge returns **503** (tunnel offline) or **502** (in-flight abort). That proves Control Plane does not dial the backend itself.

## Stage 7.3 — Tunnel Reliability & Streaming (done)

Edge traffic is **chunked** in both directions. Control Plane does not buffer entire request/response bodies in RAM.

```text
Internet → Edge → [64 KiB chunks + ACK] → Agent → 127.0.0.1:<port>
                ← [64 KiB chunks + ACK] ←
```

### Protocol (agent WebSocket)

| Frame | Direction | Purpose |
|-------|-----------|---------|
| `edge_http_begin` | CP → Agent | Method, path, port, headers |
| `edge_http_body` | CP → Agent | Request body chunk |
| `edge_http_resp_head` | Agent → CP | Status + headers |
| `edge_http_resp_body` | Agent → CP | Response body chunk |
| `edge_http_ack` | both | Backpressure (max 2 in-flight per direction) |
| `edge_http_fail` | both | Abort in-flight stream |

In-flight HTTP requests are **closed** when the tunnel drops; clients retry. No attempt to resume arbitrary HTTP mid-flight.

### Limits (defaults)

| Limit | Value |
|-------|--------|
| Chunk size | 64 KiB |
| Max request body (cumulative) | 256 MiB |
| Max buffer per stream on CP | 256 KiB in-flight |
| Request timeout | 5 min |
| Idle timeout (between chunks) | 2 min |
| Concurrent streams / device | 32 |
| Concurrent streams / service | 16 |

### Reconnect

`knot-agent` already reconnects with exponential backoff (1s → 30s). While offline:

- `tunnel_connected` = false
- new Edge requests → **503 Service Unavailable**
- in-flight streams → **502** / connection reset
- after reconnect → traffic resumes; health probe restores `backend_reachable`

### Criterion (tested)

- Small HTTP response
- Large streaming response (512 KiB+ over tunnel)
- Upload request body (192 KiB+)
- Parallel requests (6+)
- Incremental origin response
- Agent disconnect / reconnect + tunnel recovery
- In-flight abort on tunnel drop

## Stage 7.4 — Service Deployment (done)

Node manages workload lifecycle on the agent via **structured declarations** — never arbitrary shell on `knotd`.

```text
POST /v1/deployments  (Control Plane)
        ↓
Deployment Service
        ↓ deploy_apply (WebSocket)
Agent → deployrunner → Docker / OCI (or knot-fake in tests)
        ↓
127.0.0.1:<port>
        ↑
Service Registry updated → Edge route → Internet
```

MVP runtime: **Docker/OCI** (`runtime: docker`). Source is a local **OCI image reference** (no Git integration in 7.4).

### Security model

- CP sends `deploy_apply` with `action` + `DeploySpec` (image, port, bind, env, health_path).
- Agent runner uses fixed CLI shapes only (`docker run …`); no `sh -c` from user input.
- Loopback bind enforced (`127.0.0.1`).
- Env keys matching `*secret*`, `*password*`, `*token*`, etc. are **redacted** in API env, logs, and audit.

Scopes: `deploy.read` / `deploy.write` — **not** implied by `account.admin` (same rule as `shell.execute`).

### API

```http
GET    /v1/deployments?device_id=&name=
POST   /v1/deployments          { device_id, name, image, port, runtime?, bind?, health_path?, env?, hostname?, edge_device_id? }
GET    /v1/deployments/{id}
POST   /v1/deployments/{id}/stop
POST   /v1/deployments/{id}/restart
POST   /v1/deployments/{id}/rollback
GET    /v1/deployments/{id}/logs?limit=
```

Successful deploy **registers or updates** the service registry entry (`name` → `127.0.0.1:port`). Optional `hostname` + `edge_device_id` create an Edge route in the same request.

Unhealthy deploy with a previous revision → **auto-rollback** to last healthy revision.

Agent disconnect marks active deployments `stopped` with `health_ok: false`.

### Protocol (agent WebSocket)

| Frame | Direction | Purpose |
|-------|-----------|---------|
| `deploy_apply` | CP → Agent | Structured apply / stop / restart / remove / logs |
| `deploy_apply_result` | Agent → CP | OK, container_id, health_ok, sanitized log lines |
| `deploy_log_line` | Agent → CP | Optional streaming log (sanitized) |

### Criterion (tested)

```text
deploy knot-fake:v1 → Home PC :port → health OK → registry → route → Edge HTTP 200
deploy v2-unhealthy → auto-rollback → v1 still serves
stop / restart / logs
agent disconnect → deployment marked stopped; Edge 503
secrets not in logs or API env
```

Next: **7.5** end-to-end TLS to origin.

## Stage 7.5 — End-to-End TLS (done)

Two TLS modes per Edge route (`tls_mode` on `POST /v1/routes`):

| Mode | Edge behaviour | Origin |
|------|----------------|--------|
| `edge_terminate` (default) | TLS terminates on knotd; HTTP streamed via 7.3 frames | `http://127.0.0.1:port` |
| `origin_tls` | SNI routing + **raw byte tunnel** (no HTTP decrypt on Edge) | `https://127.0.0.1:port` (TLS handshake with client) |

```text
Browser ──TLS──► VPS Edge ──encrypted bytes──► Node tunnel ──TLS──► Home PC origin
                 (SNI only)                    edge_stream_*          (cert = origin)
```

`origin_tls` uses a **separate protocol** from HTTP streaming (`edge_stream_open/data/ack`) — opaque TLS records, not HTTP parsing on Edge.

### API

```http
POST /v1/routes  { hostname, service_id, edge_device_id?, tls_mode?: "edge_terminate"|"origin_tls" }
```

`origin_tls` requires service `protocol: https` (or `tcp`). Passthrough listener: `KNOT_TLS_PASSTHROUGH_ADDR` (e.g. `:443`).

### Protocol (agent WebSocket, Stage 7.5)

| Frame | Direction | Purpose |
|-------|-----------|---------|
| `edge_stream_open` | CP → Agent | Dial loopback TCP, begin byte tunnel |
| `edge_stream_ready` | Agent → CP | Origin TCP connected |
| `edge_stream_data` | both | Opaque bytes (`direction`: up/down) |
| `edge_stream_ack` | both | Backpressure (same window as 7.3) |
| `edge_stream_close` / `edge_stream_fail` | both | Teardown |

### Criterion (tested)

- `origin_tls`: client cert from **origin**, not Edge; HTTP/1.1 body through tunnel; 256 KiB streamed response
- `edge_terminate`: unchanged (7.2/7.3)
- Origin down / agent disconnect → connection failure (no silent HTTP leak on Edge)
- Different hostnames → different origins

## Stage 7.6 — Production Hardening (done)

Not a new architecture. The existing path (Internet → Edge → tunnel → Home PC container) is now operated as production-grade:

| Area | What 7.6 does |
|------|----------------|
| Authentication | Login lockout: 8 failures → 15 minutes (`account_locked`) |
| Authorization | Unchanged model; `deploy.*` still not implied by `account.admin` |
| Secrets | Audit detail redacts `token=` / `password=` / similar; deploy env already redacted |
| Audit | Same activity log; secrets stripped before insert |
| Rate limits | Per-IP: 20 login/min, 300 `/v1/*`/min in production (`KNOT_TRUST_PROXY` to honor `X-Forwarded-For`) |
| Resource limits | Default container caps: 1 CPU, 512 MB, 256 pids |
| Logs | HTTP access log (method, path, status, duration, client IP) |
| Metrics | `GET /metrics` JSON: `http_total`, `http_denied`, `uptime_sec` |
| Health | `GET /healthz` liveness; `GET /readyz` database ping |
| Backup Control Plane | `POST /v1/ops/backup` (`account.admin`) → SQLite `VACUUM INTO` `<dbdir>/backups/` |
| Agent update | Agent reports `agent_version`; welcome carries `min_agent_version`; operator replaces the binary (no OTA) |
| Certificate/key rotation | API credential rotate (existing); TLS files reload on **SIGHUP** without restart |
| Container isolation | Docker: `--cap-drop ALL`, `no-new-privileges`, pids/memory/cpu limits, loopback publish |

### API

```http
GET  /healthz
GET  /readyz
GET  /metrics
POST /v1/ops/backup
```

### CLI

```bash
knot status          # includes /readyz
knot backup
knot deploy ls|create|show|stop|restart|rollback|logs
```

### Criterion (tested)

- `/readyz` + Control Plane SQLite backup file exists
- 8 failed logins then lockout even with the correct password
- Audit redacts `token=` values
- Docker run args include capability drop and default resource limits

OTA binary distribution, Prometheus text format, and Compute jobs (8.2+) are **out of 7.6**.

## Stage 9.1 — Environments + Secrets Vault (done)

Operational layer on existing Deploy. Not HashiCorp Vault, not Git/CI.

```text
Deploy --environment production
        ↓
Environment (vars + secret references)
        ↓
Vault decrypt (CP only)
        ↓
Agent container env
```

- Secrets: AES-256-GCM in SQLite; key in `KNOT_SECRETS_KEY` or `secrets.key` beside the DB (never in the database).
- API never returns values after create. Logs and audit do not contain plaintext.
- `deploy.write` does **not** imply `secrets.read`.
- Rollback restores the previous image **and** pinned secret versions.

```http
GET/POST /v1/environments
GET/PUT  /v1/environments/{id}
GET/POST /v1/secrets          (metadata only on GET)
GET/PUT  /v1/secrets/{id}     (PUT rotates; never returns value)
POST     /v1/deployments      { environment: "production", ... }
```

```bash
knot env create production --project web-app --var NODE_ENV=production --secret DATABASE_URL=<id>
knot secret create DATABASE_URL
knot deploy create --device ID --service web-app --image myapp:v42 --port 3000 --environment production
```

MCP: `env.list`, `secrets.list` (no values), `deploy.environment`.

Git, build, CI, traffic switch, and log aggregation are **out of 9.1**.

## Stage 9.2 — Git → Build → Image (done)

Source of truth for the app, not a CI product. Control Plane does not build.

```text
Git source
    ↓
Build (pinned node, Dockerfile)
    ↓
Image tag / push
    ↓
Deploy 7.4 + Environment 9.1
```

- Source: `type=git`, url, branch/tag, revision. Git tokens are vault references (`credential_secret_id` / `secret://name`) — never stored on the source object.
- Build: Dockerfile → `docker build` → tag → `docker push`. Pinned `device_id` like Jobs 8.2 (no scheduler yet).
- Images live in an **external** registry (`ghcr.io/user/app:v43`). Node does not host a registry in 9.2.
- Lifecycle: `queued` → `cloning` → `building` → `pushing` → `completed`. Failures: `failed_clone` | `failed_build` | `failed_push`. Disconnect → `failed`.
- Scopes: `source.read` / `source.write` / `build.read` / `build.write`. Not implied by `account.admin`. `build.write` does **not** imply `deploy.write`.

```http
GET/POST /v1/sources
GET      /v1/sources/{id}
GET/POST /v1/builds          { source_id, device_id, tag, dockerfile?, context? }
GET      /v1/builds/{id}
GET      /v1/builds/{id}/logs
```

```bash
knot source add --url repo.git --branch main [--secret git-token]
knot build run <source-id> --device ID --tag ghcr.io/user/app:v43
knot build logs <id>
knot build show <id>
```

MCP: `source.list`, `build.create`, `build.status`, `build.logs`. AI cannot push or change production except through existing permissions (`build.write` ≠ `deploy.write`).

Webhooks, auto-trigger on commit, branch protection, CI runners, Node image registry, and multi-stage pipelines are **out of 9.2**.

## Stage 9.3 — Release Management + Health Gate (done)

A release is the verified unit you roll forward or back — not “the previous container”. Control Plane orchestrates; Agent still executes via Deploy 7.4. Edge does **not** switch hostnames here (that is 9.4).

```text
Build / image
    ↓
Release (pins env + secret versions)
    ↓
Deploy candidate
    ↓
Health gate
    ↓
active  or  failed → restore previous release
```

Lifecycle: `created` → `deploying` → `testing` → `active`. Terminal: `failed` | `rolled_back` | `cancelled`.

- Create snapshots environment vars, `config_version` (environment `updated_at`), and secret **pins** (id + version). Values stay in the vault.
- `POST /v1/releases/{id}/deploy` applies that snapshot. Health: HTTP path, timeout, retries, expected status (default `GET /health` → 200).
- Health OK → `active` / `current`. Health fail → disable candidate, restore the previous **verified** release (image + env + pinned secret versions).
- `POST /v1/releases/{id}/rollback` is explicit restore of the previous verified release. Requires `release.activate`.
- Scopes: `release.read` / `release.write` / `release.activate`. Not implied by `account.admin`. `release.write` does **not** imply `deploy.write` or `release.activate`. `deploy.write` does **not** imply `release.activate`.

```http
GET/POST /v1/releases
GET      /v1/releases/{id}
POST     /v1/releases/{id}/deploy
POST     /v1/releases/{id}/rollback
GET      /v1/releases/{id}/logs
```

```bash
knot release create --service web-app --image myapp:v43 --environment production
knot release deploy <id>
knot release ls --service web-app
knot release rollback <id>
```

MCP: `release.list`, `release.status`, `release.rollback`. No `release.create` / `release.deploy`. Rollback requires `release.activate` and must not run unless the user explicitly asked.

Blue/green, hostname cutover, and traffic weights were **out of 9.3** and are implemented in Stage 9.4.

## Stage 9.4 — Traffic Switch / Blue-Green (done)

Edge already terminates (or passthroughs) hostname → tunnel → origin. 9.4 adds a **release selector** on the existing route — not a second proxy.

```text
example.com
    ↓
Route
    ↓
Release selector (weights)
    ↓
Service / origin
```

- Candidate release on a **different port** keeps blue running (0% traffic) until switch.
- `POST /v1/routes/{id}/switch` `{ release_id, weight }` — default weight 100 (remainder stays on previous). Failed / non-active releases cannot receive traffic.
- `POST /v1/routes/{id}/rollback` restores the previous binding (no rebuild, no redeploy).
- History is stored per route. `edge_terminate` and `origin_tls` use the same selector.
- Scopes: `traffic.read` / `traffic.write`. Not implied by `account.admin` or `release.write`.

```http
GET  /v1/routes/{id}/traffic
POST /v1/routes/{id}/switch
POST /v1/routes/{id}/rollback
```

`{id}` is route id or hostname.

```bash
knot traffic show example.com
knot traffic switch --route example.com --release <id>
knot traffic rollback example.com
```

MCP: `traffic.status` (`traffic.read`), `traffic.switch` (`traffic.write`, only when the user explicitly asked). No automatic cutover.

Canary percentages beyond 0/100, weighted sticky sessions, and multi-region are **out of 9.4**.

## Stage 9.5 — Log Aggregation (done)

Per-domain logs already existed (build, deploy, release, job). 9.5 fans them into one operational view without becoming ELK.

```text
              Logs

Build
  │
Deploy
  │
Release
  │
Edge
  │
Service
  │
Job
  │
Agent
```

- Event: `id`, `timestamp`, `level`, `source`, `message`, plus `device_id` / `service_id` / `release_id` / `job_id` / `trace_id`.
- Sources: `agent`, `deploy`, `build`, `edge`, `job`, `system`, `audit`, `release`.
- `trace_id` correlates build → release → container/deploy → edge 502.
- Storage: `ops_logs` with default **30 day** retention (`KNOT_LOG_RETENTION_DAYS`).
- Live tail: `GET /v1/logs/follow` (SSE) and CLI poll `after=`.
- Redaction: `PASSWORD=`, `TOKEN=`, `SECRET=` never stored in plaintext.
- Scopes: `logs.read` / `logs.write`. Not implied by `account.admin` or `deploy.write`.

```http
GET  /v1/logs
POST /v1/logs
GET  /v1/logs/follow
```

```bash
knot logs list
knot logs service web-app
knot logs follow --service web-app
knot logs release <id>
```

MCP: `logs.search`, `logs.tail`, `logs.service` (`logs.read` only). No ingest/delete tools.

Stage 9 is closed: environment → git/build → release/health → traffic switch → logs.

## Stage 10.1 — MCP Operational Context (done)

Node does **not** embed an LLM. External AI (ChatGPT / Claude / Gemini / Cursor) talks MCP → the same Node API.

10.1 adds one read-only snapshot so an operator sees a service as a whole, not `{ "service": "web-app" }`:

```text
Service: web-app
Current Release: #43
Environment: production
Node: Home PC
Status: healthy
Traffic: 100%
Last deploy: 2 hours ago
Recent errors: 3
```

```http
GET /v1/ops/context?service=web-app
```

Composed from existing primitives (registry, release, traffic, deploy, logs, health). No new mutation API.

Scopes are the existing read grants — **not** a new AI permission layer. A diagnostic credential (`release.read` + `traffic.read` + `logs.read`) sees those sections; `deploy.write` does not imply this endpoint.

```bash
knot ops context web-app
```

MCP: `ops.context` (read-only).

## Stage 10.2 — Composite workflows (done)

A workflow is a **composition of existing primitives**, not a new mutation API and not an LLM.

```text
Workflow
    ↓
files.search / ops.context / build.status / …
    ↓
each step checks its own scopes
    ↓
shared trace_id
```

Catalog:

| Name | Steps | Notes |
|------|--------|--------|
| `diagnose-service` | ops.context → traffic.status → release.status → logs.search → health.check | read-only; health skipped without `services.read` |
| `deploy-release` | build.status → release.create → deploy → health.gate | **never** `traffic.switch` |
| `restore-backup` | files.search → storage.transfer → jobs.create → jobs.artifacts | locates backup, optional job; **never** production traffic |

```http
POST /v1/workflows/run
GET  /v1/workflows
GET  /v1/workflows/{id}
GET  /v1/workflows/{id}/steps
```

A diagnostic credential (`release.read` + `traffic.read` + `logs.read`) can run `diagnose-service`. The same token cannot run `deploy-release` past `release.create` — the workflow does not mint write rights.

```bash
knot workflow run diagnose-service --service web-app
knot workflow show <id>
```

MCP: `workflow.list`, `workflow.run`, `workflow.status`.

## Stage 10.3 — Scoped AI sessions (done)

External AI must not receive an admin token or a long-lived personal key. A human mints a **temporary credential** (`kind=temporary_ai`) that is a subset of their own scopes:

```text
User
 ↓
POST /v1/ai/sessions
 ↓
knot_ai_… token (shown once)
 ↓
MCP
 ↓
Node
```

The session cannot expand rights (`traffic.read` does not become `traffic.write`). `account.admin` / `credentials.write` / `shell.execute` are never grantable. After `expires_at` or DELETE, MCP calls return 401. Audit actor is `ai-session:<name> parent:<email>`.

```bash
knot ai session create --scope logs.read --scope release.read --ttl 30m
knot ai session ls
knot ai session revoke <id>
```

MCP does **not** create sessions. `ai.session` only reads the current scoped session.

## Stage 10.4 — MCP audit (done)

AI actions are rows in the existing `audit_events` table (no second log). Each event records `actor_type` (`user` | `ai_session` | `system`), display `actor`, `parent_actor`, `ai_session_id`, `mcp_client`, `workflow_id`, and `trace_id`.

MCP calls send `Authorization` + `X-Knot-Trace` + `X-Knot-MCP-Client`. The Node API binds the AI session from the credential (not from a client-supplied id).

`audit.read` is explicit — not implied by `account.admin`, and not granted to AI sessions unless a human adds it. Otherwise an agent could read every operator's history.

```bash
knot audit search --action traffic.switch --json
knot audit ai
knot audit trace <trace_id>
```

MCP (read-only): `audit.search`, `audit.ai_activity`, `audit.trace`.

## Stage 10.5 — Plan / Execute with confirmation (done)

AI must not apply complex changes directly. It creates a **Plan** (intent + steps + risk). Creating a plan does not build images, deploy, or switch traffic.

| Risk | Examples | Approval |
|------|----------|----------|
| `read` | `ops.context`, `logs.search`, `diagnose-service` | Not required — `POST /v1/plans/{id}/execute` |
| `medium` | staging deploy | Required unless a human sets `auto_execute` |
| `high` | production deploy (no traffic switch) | Always a human |
| `critical` | `traffic.switch`, production update | Always a human |

`plan.approve` is forbidden for `kind=ai_session`. After approve the engine runs **one step at a time**: scope check → primitive → audit → next. Expired and cancelled plans cannot execute; cancel keeps history.

```bash
knot plan list
knot plan show <id>
knot plan approve <id>
knot plan cancel <id>
```

MCP: `plan.create`, `plan.status`, `plan.approve` (human token only), `plan.cancel`.

