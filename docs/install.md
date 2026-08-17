# Install Node

Node is two kinds of machine, not two products.

| What you call it | Where | What gets installed | User-facing name |
|---|---|---|---|
| Panel + brain | VPS (public IP) | `knotd` + Web UI + database + TLS | **Main Node** |
| Computer that holds files / runs apps | Home PC, extra VPS, Pi, GPU box | `knot-agent` | **Device Node** |

There is **one** Main Node. There can be **many** Device Nodes. The home PC does **not** need a white IP: the agent opens an outbound connection to the VPS.

```text
Phone / laptop  →  https://your-panel
                         │
                    Main Node (VPS)
                      knotd + Web
                         ▲
            outbound     │     outbound
                         │
              Home PC         extra VPS
            knot-agent       knot-agent
```

## Quick start (like 3x-ui)

**Do not pipe into bash.** The wizard asks questions.

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/xa1onex/knot/main/scripts/install.sh)
```

If that URL 404s, the installer is not on GitHub yet. Copy this repo to the VPS and run it locally:

```bash
# on your laptop
rsync -az --exclude node_modules --exclude data --exclude web/node_modules \
  ./ root@YOUR_VPS:~/knot/

# on the VPS
cd ~/knot && bash scripts/install.sh
```

Or from this repo (already on the machine):

```bash
sudo bash scripts/install.sh    # Linux VPS Main Node
bash scripts/install.sh         # macOS Device Node as your user
```

### 1) Main Node (VPS)

The script asks:

1. Domain (or empty = use public IP + self-signed TLS)
2. Port (default `443`)
3. Admin email + password
4. Let's Encrypt vs self-signed

Then it installs Go/Node if needed, builds `knotd` + the web panel, writes `/etc/knot/knotd.env`, and starts the service.

When it finishes:

```text
Panel:    https://node.example.com
Login:    you@example.com
```

Open that URL from your phone. This is the UI. It stays on the VPS so it works even when the home PC is asleep.

### 2) Device Node (home PC)

In the panel: **Devices → Create token**.

On the PC run the same installer, choose **2) Device Node**, paste:

- Main Node URL (`https://node.example.com`)
- Join token
- Device name
- Folder for files (default `~/knot-storage`)

If the panel uses a **self-signed** certificate, say yes when asked (sets `KNOT_TLS_INSECURE=1` on the agent). Prefer Let's Encrypt on the VPS when you have a domain.

The agent is a background service (`knot-agent.service` on Linux, LaunchAgent on macOS). You should not need to keep a terminal open.

## Layout on disk (Linux, root install)

| Path | Role |
|---|---|
| `/usr/local/bin/knotd` | Main Node binary |
| `/usr/local/bin/knot-agent` | Device Node binary |
| `/usr/local/bin/knot` | Optional CLI |
| `/etc/knot/knotd.env` | Main Node config (mode `600`) |
| `/etc/knot/knot-agent.env` | Device Node config |
| `/var/lib/knot/web` | Built panel (served by `knotd`) |
| `/var/lib/knot/knot.db` | SQLite |
| `~/knot-storage` | Real files on a Device Node |

User install (no root) uses `~/.local/bin` and `~/.knot`.

## Non-interactive (automation)

```bash
KNOT_INSTALL_ROLE=main \
KNOT_DOMAIN=node.example.com \
KNOT_PORT=443 \
KNOT_BOOTSTRAP_ADMIN=you@example.com \
KNOT_BOOTSTRAP_PASSWORD='long-password' \
KNOT_TLS_MODE=letsencrypt \
bash scripts/install.sh

KNOT_INSTALL_ROLE=device \
KNOT_CONTROL_URL=https://node.example.com \
KNOT_REGISTRATION_TOKEN=knot_reg_… \
KNOT_DEVICE_NAME=home-pc \
bash scripts/install.sh
```

`KNOT_TLS_MODE` is `letsencrypt`, `selfsigned`, or `files` (then set `KNOT_TLS_CERT` / `KNOT_TLS_KEY`).

## Why the UI is not on the home PC

The PC may be behind NAT, change IP, or be powered off. The panel must be reachable whenever you open a browser. Main Node on the VPS is that always-on URL. Device Nodes still do the work (files, hosted apps) and **dial out**.

## Local development (not the installer)

```bash
make build
make run-cp
cd web && npm install && npm run dev
```

That binds loopback only (`admin` / `admin`). See [tls-and-remote.md](tls-and-remote.md).
