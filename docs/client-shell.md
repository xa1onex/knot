# Stage 5.2 — Files UX

Makes the Stage 5.1 shell feel like a real personal drive.

## Features

| Area | Behavior |
|------|----------|
| **All Files** | Virtual library across online nodes (path still relative per node) |
| **Finder / Explorer DnD** | Drop local files onto a node or the files pane → `PUT /v1/storage/content` |
| **Multi-select** | ⌘/Ctrl click; bulk delete via context menu |
| **Context menu** | Preview, download, send, copy, rename, delete |
| **Breadcrumbs** | Clickable path trail + root shortcuts |
| **Queues** | Upload / download / transfer with progress, cancel, retry hints |
| **Conflicts** | `name_conflict` → overwrite / keep both (rename) / cancel |
| **Preview** | Inline images & text (≤8 MiB via content API) |
| **Toasts** | Success / error / info notifications |
| **Multi-node drag** | File → another node uses `POST /v1/storage/transfer` |

## New API

```http
PUT /v1/storage/content?device_id=&path=&sha256=&overwrite=&conflict=
Content-Length: <size>
Body: raw octets

GET /v1/storage/content?device_id=&path=
→ file bytes (max 8 MiB) for preview/download
```

Agent protocol: `write_start` / `write_chunk` / `write_commit` / `write_abort` / `read`.

## Run

```bash
# terminal 1
make run-cp

# terminal 2
cd web && npm run dev
```

Open http://localhost:5173 — sign in, register agents, use **All Files** and drag from Finder onto a node.

## Out of 5.2

Folder sync / offline / thumbnails / smart All Files (→ **Stage 6**, `docs/files-sync.md`), streaming download of huge files to the browser (use Send to node for >8 MiB).
