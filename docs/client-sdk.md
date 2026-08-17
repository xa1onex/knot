# Stage 5.0 — Client SDK / API contract

One public API. Shells are **Web**, **CLI** (`knot`), and **MCP** (`knot-mcp`) — not native apps.

```text
Web / CLI / MCP
   │
   ▼
Client SDK  (Go: pkg/client · TS: sdk/js)
   │
   ▼
Node API /v1
   │
   ├── Auth / Credentials / Scopes
   ├── Devices
   ├── Storage (path + file_id metadata)
   └── Transfers (progress + resume)
            │
            ▼
     Agent → ~/knot-storage/
```

## Rules

1. **No per-OS backend.** There is no “Mobile API” and no native app.
2. **No private byte path.** Uploads/downloads create Transfers; agents move bytes (Direct|Relay).
3. **Permissions on the server.** Scopes from session or API credential.
4. **Progress is polled.** `GET /v1/transfers/{id}` exposes `bytes_received` / `resume_offset` (updated from dest ACKs on relay).

## Domains

| Domain | Capability |
|--------|------------|
| Auth | login, refresh, logout, me |
| Devices | list, get, reg-token, revoke, overview |
| Credentials | list, create, rotate, revoke |
| Storage | list, stat, mkdir, move, copy, delete, upload (+resume), read/download, get file meta |
| Files | metadata search across nodes (`GET /v1/files/search`), reindex |
| Services | registry of workloads per node (`GET /v1/services/tree`, `POST /v1/services`) |
| Routes | public hostname → service (`GET/POST /v1/routes`); health `GET /v1/services/{id}/health` |
| Compute | registry snapshots + labels (`GET/PUT /v1/compute/devices`); one-shot jobs (`POST /v1/compute/jobs`, optional `device_id` for scheduler); artifacts (`GET /v1/compute/jobs/{id}/artifacts`) |
| Environments / Secrets | `GET/POST /v1/environments`; `GET/POST /v1/secrets` (metadata only; values never returned) |
| Sources / Builds | `GET/POST /v1/sources`; `GET/POST /v1/builds` (Dockerfile on a pinned node; image tag for Deploy) |
| Releases | `GET/POST /v1/releases`; `POST /v1/releases/{id}/deploy` (candidate + health gate); `POST /v1/releases/{id}/rollback` (`release.activate`) |
| Traffic | `GET /v1/routes/{id}/traffic`; `POST /v1/routes/{id}/switch`; `POST /v1/routes/{id}/rollback` (`traffic.write`) |
| Logs | `GET /v1/logs`; `GET /v1/logs/follow` (SSE); `POST /v1/logs` (`logs.write`). Scopes `logs.read` / `logs.write` |
| Ops context | `GET /v1/ops/context?service=` (read-only snapshot; existing read scopes) |
| Workflows | `GET /v1/workflows`, `POST /v1/workflows/run`, `GET /v1/workflows/{id}` (compose existing primitives; per-step scopes) |
| AI sessions | `POST/GET/DELETE /v1/ai/sessions` — temporary scoped credentials for MCP; token once |
| Audit | `GET /v1/audit`, `GET /v1/audit/ai`, `GET /v1/audit/trace/{id}` (`audit.read`; not implied by admin) |
| Plans | `POST/GET /v1/plans`, `POST /v1/plans/{id}/approve` (human only), `POST /v1/plans/{id}/execute`, `POST /v1/plans/{id}/cancel` |
| Transfers | list, get, abort, watch progress |

## Storage tree (UX convention)

Clients SHOULD present these roots (agents already ensure them):

```text
photos/   projects/   backups/   shared/
```

Path-based API is canonical; `file_id` is identity for resume/metadata.

## Upload + resume + progress

```text
POST /v1/storage/upload  { device_id, path, from_device_id, source_path, size, sha256, resume? }
  → transfer { id, file_id, resume_offset, bytes_received, status }
WatchTransfer / poll GET /v1/transfers/{id}
  → Progress { bytes_received, size, percent, status }
On interrupt: keep .knot.part.<file_id>
Resume: same upload with resume:true
```

## Error codes (clients must handle)

| Code | Meaning |
|------|---------|
| `unauthorized` / `invalid_credentials` / `token_expired` / `token_revoked` | Re-auth |
| `forbidden` | Missing scope |
| `not_found` | Missing resource |
| `validation_error` | Bad input / path |
| `conflict` | Device offline, etc. |
| `quota_exceeded` | Storage quota (HTTP 507) |
| `internal` | Server fault |

Go helpers: `client.IsQuotaExceeded`, `IsUnauthorized`, …  
TS helpers: `isQuotaExceeded`, `isUnauthorized`.

## Recommended scopes for app credentials

| Profile | Scopes |
|---------|--------|
| Storage viewer | `devices.read`, `storage.read` |
| Storage editor | `devices.read`, `storage.read`, `storage.write` |
| Full client | + `network.transfer`, `activity.read`, `devices.write` |

`storage.write` may abort storage transfers (`POST /v1/transfers/{id}/abort`).

## SDK locations

| Language | Path |
|----------|------|
| Go | `pkg/client` |
| TypeScript | `sdk/js` (`@node-infra/client`) |
| OpenAPI | `docs/openapi/v1.yaml` |

## Out of 5.0

UI shells (→ 5.1 Web), camera auto-sync / thumbnails (→ Stage 6), push/WebSocket progress (optional later; poll is enough for MVP).
