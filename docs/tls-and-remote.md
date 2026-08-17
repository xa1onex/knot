# Control Plane: local vs remote (TLS)

For a VPS + home PC setup, use the installer ([docs/install.md](install.md)) instead of wiring env vars by hand.

## Local development

Default bind is **loopback only**:

```bash
KNOT_HTTP_ADDR=127.0.0.1:8787
KNOT_BOOTSTRAP_ADMIN=admin@node.local
KNOT_BOOTSTRAP_PASSWORD=admin   # allowed only on loopback
make run-cp
```

Plain HTTP on `127.0.0.1` is fine for Dashboard + local agent.

## Remote Control Plane (VPS ↔ Home PC)

Before testing agents behind NAT against a VPS Control Plane:

1. Bind publicly **only with TLS**:

```bash
KNOT_HTTP_ADDR=0.0.0.0:8787
KNOT_TLS_CERT=/etc/knot/tls/fullchain.pem
KNOT_TLS_KEY=/etc/knot/tls/privkey.pem
KNOT_PUBLIC_BASE_URL=https://node.example.com
KNOT_BOOTSTRAP_ADMIN=you@example.com
KNOT_BOOTSTRAP_PASSWORD='strong-password'
```

Without TLS, non-loopback bind fails unless `KNOT_ALLOW_INSECURE_BIND=1` (dev only).

2. Point agents at HTTPS (WSS):

```bash
knot-agent -control-url https://node.example.com
```

If the panel uses a self-signed certificate, set `KNOT_TLS_CA` to that cert on the agent, or `KNOT_TLS_INSECURE=1` (homelab only). The Device Node installer asks this.

3. Or terminate TLS at a reverse proxy (Caddy/nginx) and keep `knotd` on loopback behind it.
   Set `KNOT_TRUST_PROXY=1` only in that case so rate limits use `X-Forwarded-For`.

Replace cert/key files in place, then send **SIGHUP** to `knotd` to reload TLS without restart.

## Dev certs

```bash
./scripts/gen-dev-certs.sh ./certs
```

Docker Compose under `deployments/` expects certs + bootstrap env vars (no hardcoded `admin` password).
