# Stage 4.1 — Storage API 2.0

Builds on Stage 4.0 (`file_id`, resume, atomic `.knot.part`).

## Streaming / backpressure

```text
Client → Transfer → chunk → Transport → Agent → disk
```

No entire-file buffer in RAM. Relay sender waits for dest ACK before the next chunk (window=1), so backpressure reaches the source.

## Atomic race semantics

- Concurrent uploads to the same path use **distinct `file_id`** and distinct part files (`path.knot.part.<file_id>`).
- Final rename is under a per-path lock: the on-disk file is fully A or fully B — never a byte mix.
- Incomplete uploads never appear as finished files (parts are hidden from `list`).

## Metadata

Path-based API unchanged. Entries/stat expose:

| Field | Notes |
|-------|--------|
| `file_id` | Identity (when known in CP) |
| `name` | Basename |
| `path` | Canonical relative path |
| `size` | Bytes |
| `mtime` | RFC3339 |
| `sha256` | When complete |
| `mime_type` | Detected from name/content |
| `is_directory` | Dir vs file |

## Ops

| Op | API | CLI |
|----|-----|-----|
| list | `GET /v1/storage/list` | `knot storage ls` |
| stat | `GET /v1/storage/stat` | `knot storage stat` |
| mkdir | `POST /v1/storage/mkdir` | `knot storage mkdir` |
| delete | `DELETE /v1/storage/object` | `knot storage rm` |
| move | `POST /v1/storage/move` | `knot storage mv` |
| copy | `POST /v1/storage/copy` | `knot storage copy` |
| upload | `POST /v1/storage/upload` (`resume`) | `knot storage upload [--resume]` |
| download | `GET /v1/storage/read` | `knot storage download` |

Copy is file→file (temp + rename). Directory copy is out of scope.

## Quotas

Config (global):

- `KNOT_STORAGE_MAX_TOTAL_BYTES`
- `KNOT_STORAGE_MAX_FILE_BYTES`
- `KNOT_STORAGE_MAX_FILES`

Per-credential overrides: `credentials.max_storage_bytes`, `max_file_bytes`, `max_files`.

Checked at **upload start** and again **before commit** (after transfer completes, before marking the file complete). Concurrent/resume paths cannot bypass limits by checking only at start.

## Out of scope

Thumbnails, sharing, gallery UX, versioning UI.
