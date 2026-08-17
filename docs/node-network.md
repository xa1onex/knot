# Stage 2 — Node Network transfers

## Idea

`knotd` coordinates and **relays** small files (max 16 MiB) between online agents.
No inbound ports on Home PC / VPS behind NAT.

```text
VPS agent  --WSS-->  knotd  <--WSS--  Home PC agent
                relay chunks
```

## Layout on each agent

```text
~/knot-inbox/          (or %USERPROFILE%\knot-inbox on Windows)
  outbox/              # files you may send (allowlisted)
  inbox/               # received files land here
```

Override with `-share-dir` / `KNOT_SHARE_DIR`.

## API

```http
POST /v1/transfers
Authorization: Bearer …
{
  "from_device_id": "<vps>",
  "to_device_id": "<home>",
  "filename": "note.txt",
  "source_path": "note.txt",
  "size": 123,
  "sha256": "<hex>"
}
```

Scope: `network.transfer`.

## CLI

```bash
# put file on source outbox, then:
knot transfer send --from $VPS_ID --to $HOME_ID \
  --file note.txt --path note.txt --size $SIZE --sha256 $SHA
knot transfer status <id>
```

## Windows agent

```bash
make build-windows
# copy bin/knot-agent.exe to the PC, then:
knot-agent.exe -control-url https://node.example.com -registration-token …
```

## Round-trip demo

1. Both agents online in Dashboard.
2. Place `hello.txt` in VPS `outbox/`.
3. Transfer VPS → Home; check Home `inbox/hello.txt`.
4. Copy to Home `outbox/` and transfer Home → VPS.

## Next

- Stage 2.5: direct path / NAT when possible, relay fallback  
- Stage 3: Storage API on top of the same network channel
