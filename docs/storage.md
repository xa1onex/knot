# Stage 3 — Storage

## Rule

```text
Storage → Transfer → Transport → Agent → real FS
```

Storage has **no** private transport. Bytes move only through the existing Node Network (Direct preferred, Relay fallback). Control ops (`list` / `stat` / `mkdir` / `delete` / `move` / `copy`) use agent WSS request/response.

Stage 4.0/4.1 details (`file_id`, resume, quotas, backpressure): see [`storage-engine.md`](./storage-engine.md).

## Root

Default on each agent:

```text
~/knot-storage/
  photos/
  projects/
  backups/
  shared/
```

Override with `KNOT_STORAGE_DIR` or `knot-agent -storage-dir`.

## API

| Method | Path | Scope |
|--------|------|-------|
| `GET` | `/v1/storage/list?device_id=&path=` | `storage.read` |
| `GET` | `/v1/storage/stat?device_id=&path=` | `storage.read` |
| `GET` | `/v1/storage/read?device_id=&path=&to_device_id=` | `storage.read` |
| `POST` | `/v1/storage/upload` | `storage.write` |
| `POST` | `/v1/storage/mkdir` | `storage.write` |
| `POST` | `/v1/storage/move` | `storage.write` |
| `POST` | `/v1/storage/copy` | `storage.write` |
| `DELETE` | `/v1/storage/object?device_id=&path=` | `storage.write` |

Upload body:

```json
{
  "device_id": "<storage host>",
  "path": "shared/test.txt",
  "from_device_id": "<source agent>",
  "source_path": "test.txt",
  "size": 123,
  "sha256": "<hex>"
}
```

`read` / `upload` return a Transfer object; poll `/v1/transfers/{id}` (field `path` is `direct` \| `relay`).

Stage 4.0 adds `file_id`, `resume: true` on upload, and a 256 MiB storage size limit. See [storage-engine.md](./storage-engine.md).

## Path safety

Client paths are canonicalized and jailed under the storage root. Traversal (`../`), absolute paths, drive letters, UNC, and symlink escapes are rejected.

## CLI

```bash
knot storage list --device <id>
knot storage mkdir --device <id> --path shared/demo
knot storage upload --device <home> --path shared/test.txt --from <vps> --file test.txt --sha256 <hex> --size N
knot storage read --device <home> --path shared/test.txt --to <vps>
knot storage stat --device <home> --path shared/test.txt
knot storage delete --device <home> --path shared/test.txt
```

## Out of scope (later)

Virtual FS model, gallery, search, sharing, iPhone UI, multi-GB streaming.
