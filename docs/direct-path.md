# Stage 2.5 — Direct Path

## Rule

```text
Direct when possible
Relay fallback always
```

Transfer API unchanged (`POST /v1/transfers`). Status adds:

```json
{ "path": "direct" | "relay" }
```

## Architecture

```text
Transfer
   ↓
Transport
   ├── Direct (QUIC + STUN + Ed25519)
   └── Relay  (knotd WSS chunks)
```

Signaling (candidates) goes over existing agent WSS to `knotd`. Data plane prefers P2P QUIC.

## Config

| Env | Meaning |
|-----|---------|
| `KNOT_FORCE_RELAY=1` | Skip direct; deterministic relay |
| `KNOT_STUN_URLS` | Comma-separated STUN servers (default Google STUN) |
| `KNOT_DIRECT_TIMEOUT` | Direct negotiation budget (default `3s`) |

No permanent inbound port on Home PC: ephemeral UDP + hole punch / local candidates only.

## Identity

Direct sessions require mutual Ed25519 auth; peer public key must match the device registered in Control Plane.

## Dashboard / CLI

Same `knot transfer send …`. Inspect path via:

```bash
knot transfer status <id>
```
