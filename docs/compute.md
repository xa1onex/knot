# Stage 8 — Compute

**Status:** **8.1–8.5 done.** Compute Registry + Jobs + Limits + Artifacts + Scheduler.

```text
Home PC
├── Services
│   ├── web-app
│   └── api
├── Storage
└── Compute
    ├── CPU
    ├── RAM
    ├── GPU   (or unavailable)
    ├── Disks
    ├── OS / Agent version
    ├── Labels
    └── Jobs (pinned to a node, or scheduled)
```

The registry is a **snapshot**, not a live source of truth. The agent sends inventory on heartbeat; Control Plane stores the last snapshot. The scheduler treats stale or offline snapshots as ineligible for new placement (they may still explain *capability* for queueing).

```text
Agent  →  heartbeat/telemetry  →  Control Plane  →  Compute Registry
```

## Stage 8.1 — Compute Registry (done)

No jobs. No scheduler. Same device identity as `/v1/devices` — not a parallel ID space.

### What is collected

| Field | Contents |
|-------|----------|
| CPU | logical cores, architecture, usage % (omitted if unmeasured) |
| RAM | total / available / used bytes, usage % |
| GPU | vendor, model, VRAM bytes, availability. **`null` if undetectable** — never invented |
| Disks | one entry per volume: mount, total, free, used |
| OS / Agent | already on the device; echoed on the compute record |

### Status

| `status` | Meaning |
|----------|---------|
| `available` | Agent online and snapshot younger than ~90s |
| `stale` | Online but snapshot missing or older than the freshness window |
| `offline` | Agent disconnected; **last snapshot is kept** |

### API

```http
GET /v1/compute/devices
GET /v1/compute/devices/{device_id}
```

Scope: **`compute.read`** only. There is no `compute.write` in 8.1.

```json
{
  "device_id": "...",
  "name": "Home PC",
  "os": "windows",
  "arch": "amd64",
  "agent_version": "0.8.1",
  "status": "available",
  "last_telemetry_at": "2026-08-17T10:00:00Z",
  "cpu": { "cores": 16, "architecture": "amd64", "usage_percent": 8.0 },
  "memory": { "total_bytes": 68719476736, "available_bytes": 21474836480, "used_bytes": 47244640256 },
  "gpu": [{ "vendor": "NVIDIA", "model": "RTX 4090", "vram_bytes": 12884901888 }],
  "disks": [
    { "mount": "C:", "total_bytes": 4398046511104, "free_bytes": 2576980377600, "used_bytes": 1821066133504 }
  ]
}
```

`gpu` is JSON `null` when the agent could not detect a GPU.

### CLI / MCP / Web

```bash
knot compute ls
knot compute show <device-id>
```

MCP: `compute.list` (optional `device_id`).

Web: Files shell shows a Compute card on the selected node; Settings → Compute lists all nodes.

### Criterion (tested)

- Heartbeat inventory appears as CPU / RAM / GPU / disks / OS / agent version
- GPU unknown → `null`
- Disconnect → snapshot remains, `status=offline`
- Reconnect (same device) → snapshot refreshes, `status=available`
- Credential without `compute.read` → 403

## Stage 8.2 — Compute Jobs (done)

One-shot container tasks on an **explicit `device_id`**. Node does not pick a node. No scheduler.

```text
VPS / CLI / MCP
       │
       │ "запусти job"
       ▼
     Node API
       │
       ▼
   выбранный Node (device_id)
       │
       ▼
   контейнер  (CPU / RAM / optional GPU)
       │
       ├── input  ← Storage
       └── output → Storage
       │
       ▼
     result
```

### JobSpec

Structured only — **no arbitrary shell**. `sh`/`bash`/`cmd`/`powershell` plus `-c`/`/c` is rejected (same rule as Deploy).

```json
{
  "device_id": "...",
  "image": "python:3.13",
  "command": ["python", "/input/main.py"],
  "env": {},
  "resources": { "cpu": 2, "memory_mb": 512, "gpu": 0 },
  "timeout_seconds": 600,
  "input_path": "in",
  "output_path": "jobs/{job_id}/"
}
```

States: `queued` → `running` → `succeeded` | `failed` | `timeout` | `canceled`.

Agent restart while a job is in-flight → `failed` (`agent disconnected`).

### Isolation and limits

Docker jobs run foreground (`docker run`, not `-d`): `--network none`, `--cap-drop ALL`, `no-new-privileges`, pids/memory/cpus, optional `--gpus N`. No published ports. Tests without Docker use `knot-fake-job:*` (and the same fake path for `python:3.13` when the daemon is not used).

### Artifacts

Input is copied from the node's existing `knot-storage` into `/input`. Output is copied from `/output` back into Storage (default `jobs/{job_id}/`). No new transfer channel.

### API / scopes

```http
GET  /v1/compute/jobs
POST /v1/compute/jobs
GET  /v1/compute/jobs/{id}
GET  /v1/compute/jobs/{id}/artifacts
POST /v1/compute/jobs/{id}/cancel
GET  /v1/compute/jobs/{id}/logs
```

- `compute.read` — list / get / logs / artifacts
- `compute.write` — create / cancel — **not** implied by `account.admin`

Env and logs are redacted like Deploy.

### CLI / MCP / Web

```bash
knot jobs ls [--device ID]
knot jobs run --device ID --image python:3.13 --cpu 2 --memory-mb 512 --wait
knot jobs show <id>
knot jobs artifacts <id>
knot jobs logs <id>
knot jobs cancel <id>
knot jobs wait <id>
```

MCP: `jobs.list` / `jobs.create` / `jobs.get` / `jobs.cancel` / `jobs.logs` / `jobs.artifacts`.

Web: Settings → Compute lists jobs under the registry.

### Criterion (tested)

- start → logs contain `hello` → `artifacts_committed` (`python:3.13`, CPU 2, RAM 512 MB)
- failure, timeout, cancel
- agent disconnect while running
- resource limits stored on the job
- secret redaction in env + logs
- container isolation flags (no network, no caps, no publish, not `-d`)
- artifact → Storage (`result.txt` under `jobs/{id}/`)
- missing `device_id` → validation
- credential without `compute.write` → 403 (admin does not imply write)

## Stage 8.3 — Resource Limits (done)

The Agent is the **last enforcement point**. Client and Control Plane validate ranges; the node still rejects anything above its local policy. Docker argv is also capped so a bypassed Check cannot raise quotas. GPU is never silently mapped to CPU.

```text
Client → CP validation → Agent policy → Docker
```

Default local policy (override with `KNOT_JOB_MAX_*`):

| Ceiling | Default |
|---------|---------|
| CPU | `GOMAXPROCS` / `NumCPU` as `--cpus` quota (not shares) |
| RAM | 8192 MiB + `--memory-swap` equal to memory (OOM, not host thrash) |
| GPU | only if NVIDIA runtime exists; `--gpus device=0,1,…` never `all` |
| Disk | 1024 MiB job default / 4 GiB node ceiling; `/tmp` tmpfs + `/output` watch |
| PIDs | 256 (range 16–4096) |
| Timeout | kill container, `docker rm -f`, status `timeout` |
| Concurrent jobs | 4 slots on the device — **not** a scheduler |

Reject reasons (status `rejected` unless the job already started):

| `reason` | When |
|----------|------|
| `policy_exceeded` | CPU / RAM / disk / PIDs / timeout above node ceiling (e.g. 64 GiB RAM vs 8 GiB policy) |
| `gpu_unavailable` | `gpu > 0` but no GPU, no runtime, or more GPUs than allowed |
| `slot_unavailable` | node already at `max concurrent` |
| `resource_exceeded` | running job hit OOM or disk cap → `failed` |

### Criterion (tested)

- CPU / RAM / PID / disk ceilings on Docker argv (`--cpus`, `--memory`+swap, `--pids-limit`, tmpfs)
- 64 GiB RAM vs 8 GiB node policy → `rejected` / `policy_exceeded`
- GPU requested, none/runtime missing → `rejected` / `gpu_unavailable` (no CPU fallback)
- GPU count above policy → `gpu_unavailable`; exact count allowed
- OOM / disk over `/output` → `failed` / `resource_exceeded`
- Timeout still kills and cleans up
- Second job while slot is full → `slot_unavailable`

## Stage 8.4 — Job Artifacts → Storage (done)

No new transfer channel. Jobs already used `/input` and `/output` in 8.2; 8.4 makes that an atomic Storage contract.

```text
Storage
   │  jobs/<job_id>/input/
   ▼
Compute Job   /input  (isolated workspace)
   │
   ▼
/output
   │  inspect limits → SHA-256 → rename
   ▼
Storage       jobs/<job_id>/output/
   │
   ▼
artifact → job → device
```

### Layout

| Role | Storage path | Container |
|------|----------------|-----------|
| Input | `jobs/{job_id}/input/` | `/input` (read-only) |
| Output (complete) | `jobs/{job_id}/output/` | written as `/output` |
| Output (in-flight) | `jobs/{job_id}/.knot.part/` | never listed as a finished artifact |

Optional create `input_path` is copied **into** `jobs/{job_id}/input/` first. The job never bind-mounts `C:/`, `~/knot-storage/`, or the storage root — only a temp workspace.

### Atomic commit

Container exit 0 is not enough. Agent:

1. Inspects `/output` (file count, dir count, depth, per-file size, total size)
2. Copies into `.knot.part` and hashes SHA-256
3. Renames `.knot.part` → `output/`
4. Reports `artifacts_committed` with metadata

Crash / timeout / cancel / disconnect leave no complete artifacts. Partial dirs are swept when the agent starts.

Terminal success status is **`artifacts_committed`** (not `succeeded`). Failed jobs clean workspace + partial output.

### Artifact metadata

Each result is a normal Storage object (`file_id`, path, size, sha256, complete) plus:

`artifact_id`, `job_id`, `file_id`, `path`, `name`, `size`, `sha256`, `mime_type`, `created_at`

```http
GET /v1/compute/jobs/{id}/artifacts
```

Also returned on `GET /v1/compute/jobs/{id}`. Readable through Storage list/stat/CLI/MCP.

### Limits (in addition to `disk_mb`)

| Limit | Default |
|-------|---------|
| Max single artifact | 256 MiB (`KNOT_JOB_MAX_ARTIFACT_BYTES`) |
| Max files | 256 |
| Max directories | 64 |
| Max path depth | 8 |
| Total size | job `disk_mb` |

Too many tiny files → `failed` / `artifact_limit`. Oversized file → same. Test policy uses a 1 MiB per-file cap.

### Criterion (tested)

- Input is visible only inside the job (`secret.txt` at storage root is not copied)
- Output appears under `jobs/{id}/output/` in Storage
- SHA-256 on artifacts matches file bytes and `storage.stat`
- Metadata stored (`artifact_id`, `file_id`, `job_id`, …)
- Too many files / oversized file → `artifact_limit`, no commit
- fail / timeout / cancel do not commit
- Agent start sweeps leftover `.knot.part`
- Artifacts via Storage API, `GET .../artifacts`, CLI `knot jobs artifacts`, MCP `jobs.artifacts`

## Stage 8.5 — Distributed Scheduler (done)

Do not pick a node by hand:

```bash
knot jobs run --image python:3.13 --cpu 8 --memory-mb 16384 --gpu required
```

```text
User
 ↓
Job requirements (CPU / RAM / GPU / disk / labels)
 ↓
Scheduler
 ↓
best Node
 ↓
existing Job path → /output → Storage artifacts
```

`device_id` is **optional**. Set it → **pinned** (8.2 behavior, `max_retries=0`). Omit it → **scheduled** (`max_retries` default 1).

No Kubernetes, bin packing, live migration, cluster manager, or cost model.

### Matching

Requirements on create (`resources` + `require` / `prefer`):

```json
{
  "cpu": 8,
  "memory_mb": 16384,
  "gpu": "required",
  "disk_mb": 20480
}
```

`gpu` may be an integer or `"required"` (count 1).

The scheduler compares **idle snapshot capacity** (capability) and **residual** after summing `assigned`+`running` reservations (availability). Example: Home 16 CPU / 64 GiB / RTX vs VPS 4 CPU / 8 GiB → GPU job goes to Home.

If every snapshotted node is incapable even at idle → `rejected` / `unsatisfiable`. If a capable node exists but is busy/offline, or an online node has no snapshot yet → `waiting_for_resource`.

### Labels

User labels (`PUT /v1/compute/devices/{id}/labels`, `knot compute labels`, MCP `compute.labels`) merge with derived keys: `gpu=true`, `os`, `windows`/`linux`/`darwin`, `arch`.

```text
require: { "gpu": "true" }     # hard filter
prefer:  { "location": "home" } # score bonus
```

### Queue and retry

```text
queued → waiting_for_resource → assigned → running → artifacts_committed
```

Busy GPU: job #2 waits; when #1 finishes or is canceled, resources free and #2 is assigned.

Node death: **no live migration**. Pinned jobs `failed` (disconnected). Scheduled jobs requeue if `attempts <= max_retries`, clear the old `device_id`, and may run on another node. Input is already in Storage (`jobs/<id>/input`); the new agent restores it.

### API / CLI / MCP

| | |
|--|--|
| Create | `POST /v1/compute/jobs` — `device_id` optional; `require`, `prefer`, `retry_max`; `resources.gpu` int or `"required"` |
| Labels | `PUT /v1/compute/devices/{id}/labels` (`compute.write`) |
| CLI | `knot jobs run --image …` (no `--device`); `--gpu required`; `--require k=v`; `--prefer k=v` |
| MCP | `jobs.create` without `device_id`; `compute.labels` |

Permissions unchanged: `compute.read` list/get; `compute.write` create, cancel, labels.

### Criterion (tested)

- GPU job (8 CPU / 16 GiB / GPU) → Home PC, not VPS → `artifacts_committed` in Storage
- 32 CPU + GPU → `rejected` / `unsatisfiable`
- Second GPU job while RTX is busy → `waiting_for_resource`; cancel first → second runs
- `require gpu=true` / `prefer location=cloud`
- Labels `compute.write`; `compute.read` alone → 403
- Pinned disconnect still **failed**
- Scheduled disconnect → retry on another node

Stage 8 Compute is closed: Registry + Jobs + Limits + Artifacts + Scheduler.

