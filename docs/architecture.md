# Node architecture (codename: knot)

**Node** is the product name. This repository and CLI binaries use the internal codename **knot**.

## Stage map

| Product stage | Status in this repo |
|---|---|
| Stage 1 Core — Agent, Control Plane, Network foundation, Credentials, API, CLI, MCP foundation, Dashboard | Vertical slice implemented |
| Stage 1.5 — Hardening (auth, TLS, migrations, device challenge, SDK, tests) | Implemented |
| Stage 2 — Node Network (CP relay transfers VPS↔Home) | Implemented |
| Stage 2.5 — Direct Path (QUIC + STUN, relay fallback) | Implemented |
| Stage 3 Storage | Implemented (real FS + Transfer; no virtual FS) |
| Stage 3.5 — External Client / MCP | Implemented (thin MCP over Node API) |
| Stage 4.0 — Storage Engine | Implemented (file_id, resume, atomic, 256 MiB) |
| Stage 4.1 — Storage API 2.0 | Implemented (concurrency/integrity, backpressure, metadata, mv/copy, quotas, CLI) |
| Stage 5 — Node Clients Platform | Implemented / closed (see `docs/clients-platform.md`) |
| Stage 5.0 — Client SDK / API contract | Implemented (`pkg/client`, `sdk/js`, OpenAPI) |
| Stage 5.1 — Web reference client | Implemented (user Files shell; see `docs/client-shell.md`) |
| Stage 5.2 — Files UX | Implemented (All Files, DnD, queues, conflicts, preview) |
| Stage 5.3 — Client Apps | **Withdrawn** — no Electron/Expo; Web + CLI + agent + MCP |
| Stage 6 — Files & Sync | **Done** (6.1–6.5; see `docs/files-sync.md`) |
| Stage 6.1 — One-way Sync | Implemented / closed |
| Stage 6.2 — Offline & Two-way Sync | Implemented (6.2.1 + 6.2.2) |
| Stage 6.2.1 — Two-way + conflicts | Implemented (`mode=two_way`, sync_conflicts) |
| Stage 6.3 — Conflicts UX | Implemented (persistent conflicts UI + batch resolve) |
| Stage 6.4 — Thumbnails & previews | Implemented / closed |
| Stage 6.5 — Smart Files (metadata index + All Files search) | Implemented / closed |
| Stage 6+ Advanced AI integrations | External AI only; no AI inside Node |
| Stage 7 — Infrastructure / Hosting | **Done** (7.1–7.6; see `docs/hosting.md`) |
| Stage 7.1 — Service Registry | Implemented / closed |
| Stage 7.2 — Edge / Reverse Proxy | Implemented / closed |
| Stage 7.3 — Tunnel Reliability & Streaming | Implemented / closed |
| Stage 7.4 — Service Deployment | Implemented / closed |
| Stage 7.5 — End-to-End TLS | Implemented / closed |
| Stage 7.6 — Production Hardening | Implemented / closed |
| Stage 8 — Compute | **Done** (8.1–8.5; see `docs/compute.md`) |
| Stage 8.1 — Compute Registry | Implemented / closed |
| Stage 8.2 — Compute Jobs | Implemented / closed |
| Stage 8.3 — Resource Limits | Implemented / closed |
| Stage 8.4 — Job Artifacts | Implemented / closed |
| Stage 8.5 — Distributed Scheduler | Implemented / closed |
| Stage 9 — Deployment Platform 2.0 | **Done** (9.1–9.5) |
| Stage 9.1 — Environments + Secrets Vault | Implemented / closed |
| Stage 9.2 — Git → Build → Image | Implemented / closed |
| Stage 9.3 — Release Management + Health Gate | Implemented / closed |
| Stage 9.4 — Traffic Switch (Blue/Green) | Implemented / closed |
| Stage 9.5 — Log Aggregation | Implemented / closed |
| Stage 10 — External AI Operator | **In progress** (10.1) |
| Stage 10.1 — MCP Operational Context | Implemented / closed |

## Components

```text
Product roles:     Main Node (VPS)          Device Node (PC / extra VPS / Pi)
Binaries:          knotd + Web UI           knot-agent
                   ▲
Operators:  browser  ·  knot (CLI)  ·  knot-mcp (AI)
        │  REST /v1
        ▼
     knotd  (Control Plane)  — one public API, one permissions model
        ▲
        │ WebSocket (outbound from the device)
        │
   knot-agent
```

Install with `scripts/install.sh` (see [docs/install.md](install.md)). `make build` is the developer path.

- **Stage 2:** Control Plane may **relay** small transfers (`network.transfer`, max 16 MiB). Direct P2P is Stage 2.5.
- **Stage 3:** Storage is a thin API over Transfer/Transport + agent FS under `~/knot-storage` (`storage.read` / `storage.write`).
- **Stage 3.5:** `knot-mcp` is a thin MCP/stdio wrapper over the same Node API and permissions as CLI — no AI inside Node.
- **Stage 4.0:** Storage engine adds `file_id`, resumable uploads (`.knot.part`), atomic commit, and a 256 MiB storage size limit (network.transfer stays 16 MiB).
- **Stage 4.1:** Storage API 2.0 — ACK backpressure, extended metadata, `move`/`copy`, quotas (start + pre-commit), CLI `ls|stat|upload|download|mkdir|mv|rm|copy`. See `docs/storage-engine.md`.
- **Stage 5:** Clients Platform — shared SDK/contract, then **Web** as the operator UI. No native apps. See `docs/clients-platform.md`.
- **Stage 5.0:** Go (`pkg/client`) + TypeScript (`sdk/js`) SDKs, OpenAPI (`docs/openapi/v1.yaml`), progress via `bytes_received`, storage-scoped transfer list/abort. See `docs/client-sdk.md`.
- **Stage 5.1:** Web reference shell — Nodes + Files + Transfers UX over the SDK; multi-node drag via `POST /v1/storage/transfer`. See `docs/client-shell.md`.
- **Stage 5.2:** Files UX — All Files, Finder DnD (`PUT /v1/storage/content`), multi-select, context menu, queues, conflicts, preview.
- **Stage 5.3:** Native client apps (Electron / Expo) were built and then **withdrawn**. Node does not ship a desktop or mobile app. Humans use the Web UI or `knot`; machines run `knot-agent`; AI uses `knot-mcp`.
- **Stage 6:** Files & Sync — see `docs/files-sync.md`.
- **Stage 6.1:** One-way folder sync. **Closed.**
- **Stage 6.2.1:** Two-way sync + persistent conflicts (`keep_a` / `keep_b` / `keep_both`). **Done.**
- **Stage 6.2.2:** Offline change journal on agent + flush into two-way sync. **Done.**
- **Stage 6.3:** Conflicts UX (persistent, batch resolve, safe keep_both names). **Done.**
- **Stage 6.4:** Thumbnails & previews. **Done.**
- **Stage 6.5:** Smart Files — global metadata index, All Files search, `files.search` MCP. **Done.** Stage 6 foundation complete.
- **Stage 7:** Infrastructure / Hosting — see `docs/hosting.md`.
- **Stage 7.1:** Service Registry (metadata of workloads per node). **Done.**
- **Stage 7.2:** Public Edge reverse proxy — hostname → node loopback via the outbound agent tunnel. TLS terminates on the Edge. **Done.**
- **Stage 7.3:** Chunked Edge HTTP with ACK backpressure, in-flight abort on disconnect, reconnect recovery. **Done.**
- **Stage 7.4:** Structured Docker/OCI deploy on agent via `deploy_apply`; registry sync; auto-rollback; no shell on CP. **Done.**
- **Stage 7.5:** `edge_terminate` vs `origin_tls`; SNI passthrough via `edge_stream_*` byte tunnel; TLS ends on origin. **Done.**
- **Stage 7.6:** Production hardening — lockout, rate limits, audit redaction, readyz/metrics, CP backup, container isolation + resource caps, agent version, TLS SIGHUP reload. **Done.**
- **Stage 8.1:** Compute Registry — last heartbeat snapshot of CPU/RAM/GPU/disks per node. **Done.**
- **Stage 8.2:** Compute Jobs — one-shot containers. Explicit `device_id` pins a node; omit it for the 8.5 scheduler. Resource limits, logs, cancel/timeout, artifacts via Storage. **Done.**
- **Stage 8.3:** Agent-enforced job ceilings (CPU quota, RAM/OOM, GPU no-fallback, disk, PIDs, timeout kill, per-node concurrency). **Done.**
- **Stage 8.4:** Job artifacts as Storage objects — isolated `/input`, atomic `/output` commit, SHA-256 metadata, limits, no leftover partials. **Done.**
- **Stage 8.5:** Distributed scheduler — capability + residual reservations + labels; queue `waiting_for_resource`; retry elsewhere on node death (no live migration). **Done.**
- **Stage 9.1:** Environments + secrets vault — encrypted-at-rest values, pins on deploy, `secrets.read`/`secrets.write` distinct from `deploy.write`. **Done.**
- **Stage 9.2:** Git source → Dockerfile build on a pinned node → image tag/push → existing Deploy. `source.*` / `build.*` distinct from `deploy.write`. **Done.**
- **Stage 9.3:** Release object + health gate — create from image, deploy candidate, mark ready or restore the previous verified release. `release.write` ≠ `deploy.write` ≠ `release.activate`. No blue/green traffic switch (9.4). **Done.**
- **Stage 9.4:** Edge traffic binding — hostname → release selector (weights, instant rollback). `traffic.write` ≠ `release.write`. No new proxy layer. **Done.**
- **Stage 9.5:** Unified operational logs — one event model + `trace_id` across build → release → deploy → edge. `logs.read` / `logs.write` are explicit. Retention 30 days (`KNOT_LOG_RETENTION_DAYS`). Not an ELK clone. **Done.** Stage 9 closed.
- **Stage 10.1:** Read-only operational context (`GET /v1/ops/context`, MCP `ops.context`) — one snapshot for an external AI operator. No LLM inside Node. Sections gated by existing read scopes. **Done.**
- **Stage 10.2:** Composite workflows (`POST /v1/workflows/run`) — diagnose-service, deploy-release (no traffic switch), restore-backup. Each step is an existing primitive with its own scopes. No new AI permission layer. **Done.**
- **Stage 10.3:** Scoped AI sessions — temporary `kind=temporary_ai` credentials, subset of the creator's scopes, TTL, revoke. No admin token to MCP. **Done.**
- **Stage 10.4:** MCP audit — same `audit_events` table with actor_type / parent / session / MCP client / workflow / trace. `audit.read` is explicit (not implied by admin; not auto-granted to AI). **Done.**
- **Stage 10.5:** Plan / Execute with human approval — AI proposes (`POST /v1/plans`) without mutating; high/critical require `POST /v1/plans/{id}/approve` from a human (AI cannot self-approve). Each execute step has its own scope check, trace, and audit. **Done.**
- **Permissions** live in `pkg/permissions` and are checked for every API call.
- **MCP / CLI / Web** are thin clients of the same Node API.

## Vertical slice success path

1. Start `knotd`
2. Login (Dashboard or `knot login`) — bootstrap `admin@node.local` / `admin`
3. Create registration token
4. Run `knot-agent` with the token
5. Device shows **online** in Dashboard / `knot devices`
6. Scoped API credential can `GET /v1/devices`

## Data

SQLite by default (`KNOT_DB_PATH`). Schema is isolated in `internal/store` for a later Postgres backend.
