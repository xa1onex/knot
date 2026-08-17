# Stage 5 — Node Clients Platform

Node is the infrastructure layer. There is **no** product app for macOS, Windows, iOS, or Android.

## Split

| Layer | Role | Binary / UI |
|-------|------|-------------|
| **Control plane** | API, identity, permissions | `knotd` (daemon) |
| **Agent** | Device ↔ Node: FS, transfer, deploy, jobs | `knot-agent` (daemon on the machine) |
| **CLI** | Human operator in a terminal | `knot` |
| **MCP** | External AI → same Node API | `knot-mcp` (stdio) |
| **Web** | Browser UI for files, nodes, settings, plans | `web/` |

```text
                  Node API  (/v1)
                       │
            ┌──────────┼──────────┐
            ▼          ▼          ▼
          Web        knot      knot-mcp
       (browser)     (CLI)     (AI)
                       │
                       ▼
                  knot-agent
              (Linux / macOS / Windows)
```

**No** per-OS backends. Every client:

```text
Web / CLI / MCP → Node API → Storage / Devices / Transfers → Agent → ~/knot-storage/
```

## Substages

| Stage | Focus | Status |
|-------|--------|--------|
| **5.0** | Client SDK / API contract | **Done** |
| **5.1** | Web reference shell | **Done** |
| **5.2** | Files UX (All Files, DnD, queues, conflicts, preview) | **Done** |
| **5.3** | Native client apps | **Withdrawn** |

Stage 5.3 (Electron desktop, Expo iOS/Android) duplicated the Web shell and is not maintained. Use a browser for UI. Use terminal binaries to operate and to run work on nodes.

## Next → Stage 6 Files & Sync

**6.1–6.5 closed.** See `docs/files-sync.md`.
