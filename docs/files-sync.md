# Stage 6 — Files & Sync

**Status:** **Done** — Stage 6 Files & Sync foundation complete (6.1–6.5). Next is Stage 7.

Physical bytes stay on the user’s own Node agents. Clients stay thin shells over one Public API.

## Substages

| Stage | Focus | Status |
|-------|--------|--------|
| **6.1** | **One-way Sync** | **Done** / closed |
| **6.2** | **Offline & Two-way Sync** | **Done** (6.2.1 + 6.2.2) |
| **6.2.1** | Two-way + basic conflict protection | **Done** |
| **6.2.2** | Offline change capture → sync on reconnect | **Done** |
| **6.3** | Conflicts UX (no merge engine) | **Done** |
| **6.4** | Thumbnails & previews | **Done** |
| **6.5** | Smart file layer | **Done** |

## Stage 6.1 — One-way Sync (closed)

`one_way`: A → B, incremental, delete mirror, resume. See `/v1/sync/jobs`, `internal/sync`.

## Stage 6.2.1 — Two-way Sync (done)

```text
Node A  ↔  Node B
```

Mode: `two_way`. Last-known state per path stores at least:

`file_id`, `path`, `size`, `mtime`, `sha256`, `created`, `deleted`, `last_synced` (+ conflict link).

### Rules

- **No silent overwrite.** Concurrent edits → persistent `sync_conflicts` row.
- Change detection: mtime/size candidate → **SHA-256** confirm.
- Create / modify / delete both directions; rename ≈ delete+create (same hash).
- Identical changes both sides → advance base, no transfer.
- Disconnect / crash: re-run resumes; open conflicts skipped until resolved.

### Conflict resolution

```text
keep_a | keep_b | keep_both
```

`keep_both`: A stays at `path` on both sides; B saved as a unique `stem.conflict-<device>-<YYYYMMDD-HHMM>.ext` (never overwrites). See **6.3**.

### API

```http
POST /v1/sync/jobs                 { mode: "two_way", ... }
GET  /v1/sync/jobs/{id}/conflicts
POST /v1/sync/conflicts/{id}/resolve  { resolution: "keep_a"|"keep_b"|"keep_both" }
```

Job may finish as `completed_with_conflicts`.

### CLI

```bash
knot sync create --mode two_way --from <A> --from-path projects --to <B> --to-path projects
knot sync run <job> && knot sync wait <job>
knot sync conflicts <job>
knot sync resolve <conflict-id> --resolution keep_both
```

### Criterion (tested)

```text
A ↔ B: create/modify/delete both ways ✓
concurrent modify → CONFLICT → keep_both ✓ (nothing silent lost)
```

## Stage 6.2.2 — Offline (done)

Capture local edits while a node cannot reach the Control Plane; drain on reconnect through **existing** two-way sync. Offline does **not** invent a second conflict system.

```text
MacBook
   │
   X────── Internet unavailable
   │
   ├── edit / create / delete / rename
          │
          ▼
      local queue (crash-safe)
          │
       internet
          │
          ▼
       Node Sync (two_way)
          │
          ▼
       Home PC
```

### Local change journal (agent)

Each change is a durable queue row:

`operation`, `path`, `file_id`, `old_state`, `new_state`, `timestamp`, `status`

Operations: `create` | `modify` | `delete` | `rename`.

Stored under the agent data dir (`~/.knot/agent/offline-queue.db` by default), SQLite + WAL — survives Agent restart.

### Queue states

```text
PENDING → SYNCING → DONE
PENDING → SYNCING → CONFLICT   (path also opens sync_conflicts on CP)
```

While Node is unreachable, entries stay `PENDING`. On reconnect the agent marks them `SYNCING`, asks CP to flush, then finishes as `DONE` or `CONFLICT`.

### Retry / backoff

Flush retries use exponential backoff: **1s → 2s → 4s → 8s …** capped at **30s** (same shape as agent WS reconnect). No 1 Hz hammering.

### Disk protection

`KNOT_OFFLINE_QUEUE_MAX_BYTES` (default **64 MiB**) caps journal payload bytes. Over limit → new entries rejected (`ErrDiskLimit`); existing rows kept.

### Reconnect flush

1. Agent scans storage vs baseline → enqueue any missed offline edits.
2. Agent sends `offline_pending` over WSS (or client calls `POST /v1/sync/flush`).
3. CP runs every `two_way` job that includes that device and **waits**.
4. Open conflicts from that run map to journal `CONFLICT`; other pending paths → `DONE`.
5. Two-way reconcile remains the source of truth (mtime/size → SHA-256).

### API / CLI

```http
POST /v1/sync/flush   { "device_id": "<id>" }   # user-auth; runs two_way jobs for device
GET  /v1/sync/flush/{device_id}               # last flush summary (optional status)
```

```bash
knot sync flush <device-id>
```

Agent-local inspection (on device): queue lives in agent data dir; statuses above.

### Criterion (tested)

```text
A offline → 5 local changes → Agent restart → queue intact
→ network restored → queue processed → B = A ✓

offline change on A + remote change on B → online
→ existing two-way CONFLICT (keep_a / keep_b / keep_both)
```

**Not in 6.2.2:** richer conflict UX (→ **6.3**).

## Stage 6.3 — Conflicts UX (done)

User-facing conflict management on top of the existing two-way conflict model. **No separate conflict system. No merge engine.**

```text
A changed  +  B changed  →  CONFLICT (persistent)
                              ↓
                         Conflicts UI
                              ↓
              Keep A / Keep B / Keep Both
                              ↓
                         status = resolved
                              ↓
                    re-run sync → nodes agree
```

### UI

- Open conflict count on Sync Job (`conflicts_open`)
- Per conflict: path, both device names, mtime, size, version hash
- Actions: Keep \<A\>, Keep \<B\>, Keep Both
- Batch resolve selected conflicts
- Text files: optional small line diff (preview)
- Binary (images/video/archives): choose A, B, or both — no merge
- Resolved history (status `resolved`)

### keep_both naming

Deterministic, collision-safe:

```text
config.conflict-macbook-20260817-2234.json
```

If that path already exists → `-2`, `-3`, … Never overwrites an existing file.

### API

```http
GET  /v1/sync/jobs/{id}/conflicts?open=true|false
POST /v1/sync/conflicts/{id}/resolve          { "resolution": "keep_a"|"keep_b"|"keep_both" }
POST /v1/sync/conflicts/batch-resolve        { "conflict_ids": [...], "resolution": "..." }
```

Conflict JSON includes `a_device_name`, `b_device_name`, `keep_both_suggested_name`.
Resolve is audited (`sync.conflict.resolve`) and triggers a sync re-run.

### Criterion (tested)

```text
concurrent edit → CONFLICT → UI/API keep_* → resolved → nodes consistent
CP restart after conflict → conflict still open (persistent state)
keep_both never overwrites an existing conflict copy
batch resolve ✓
```

**Not in 6.3:** full merge / three-way editor (later).

## Stage 6.4 — Thumbnails & previews (done)

Previews are a **UI function**, never a mutation of user storage.

```text
~/knot-storage/photos/image.jpg   ← original
~/.knot/previews/...              ← derived cache (safe to delete)
```

### Rules

- Original files remain unchanged.
- Preview cache is **separate** from `~/knot-storage`.
- Cache is not shown in All Files, not synced, and not counted against user file quota.
- Preview keys are derived from file identity/content: `path + sha256 + variant + size`.
- If the file changes, a new preview key is generated; old cache can be GC'd.

### Supported first pass

- Images: `jpeg`, `png`, `webp`, `gif` (first frame)
- Video: thumbnail from first frame
- PDF: first page
- Text / code: first N KB, bounded
- Other files: generic icon + existing metadata

### Bounded generation

```text
original file
    ↓
 size check
    ↓
 mime/type detect
    ↓
 bounded read / bounded decode
    ↓
 generate cached preview
```

No unbounded full-file reads for text previews. Large image/video/pdf inputs are rejected for inline preview instead of reading forever.

### Storage / API

- Agent cache: `KNOT_PREVIEW_DIR` or default `~/.knot/previews`
- Web/API: `GET /v1/storage/preview?device_id=<id>&path=<rel>&variant=thumb|preview`
- Existing `GET /v1/storage/content` remains the original-file path

### UX

- All Files / per-node file list shows thumbnails when available
- Open image/video/pdf → derived preview
- Open text/code → bounded text preview
- Open `zip/db/unknown` → generic preview only

### Criterion

```text
All Files/photos → thumbnails visible
open image → preview/original path
open PDF → first page
open zip/db/unknown → generic preview + metadata
delete preview cache → originals intact, previews regenerate
```

## Stage 6.5 — Smart Files (done)

All Files is one logical view of files on every Node. Control Plane indexes **metadata only** — it never copies bytes. This is not AI Search and not full-text search.

```text
query → metadata index → results
```

Node remains the source of truth. If a file disappears on a node, the next reindex drops it. Offline nodes keep the last snapshot (stale is OK).

### Index (`file_index`)

`file_id`, `device_id`, `path`, `name`, `size`, `mtime`, `sha256`, `mime_type`, `is_directory`

Refresh: `POST /v1/files/reindex`, agent connect, storage mutations (mkdir/delete/move/copy/put/transfer complete).

### Search

```http
GET /v1/files/search?q=logo&device_id=&type=image|video|pdf|text&folder=&min_size=&max_size=&modified_after=&modified_before=
```

Empty `q` + `folder` = browse direct children (union across nodes). `q` set = substring on name/path (optional folder prefix).

### Actions

From All Files / search: Open, Download, Move/Rename, Copy, Delete, Send to another Node — all reuse existing storage/transfer (`Direct → Relay`). No new transport.

### MCP

`files.search` is a thin tool over the same API so an external AI can ask “find the latest production backup on any node” without Node embedding AI.

### CLI

```bash
knot files search --q logo
knot files reindex
```

### Criterion (tested)

```text
Home PC  /projects/site/logo.png
VPS #3   /var/www/site/logo.svg  (+ /Documents/logo.pdf)
Search "logo" → all hits with node labels
site.zip → Send to other node via existing StorageTransfer
delete on node → reindex drops the row
```

## After Stage 6 → Stage 7 Infrastructure / Hosting

See `docs/hosting.md`. **7.1–7.6 done.** Stage 7 complete. Compute starts at **8.1** (`docs/compute.md`).

```text
Internet → VPS / Public Endpoint → Node → Home PC
```
