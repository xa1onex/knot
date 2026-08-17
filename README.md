# Node (knot)

**Node** joins your VPS, home PCs, and servers into one private network: files, hosting, and jobs — with a browser panel.

Internal names: control plane `knotd`, device daemon `knot-agent`, CLI `knot`.

## Install (this is the product path)

On the **VPS** (Main Node — panel + brain):

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/xa1onex/knot/main/scripts/install.sh)
```

Choose **1) Main Node**. After it finishes you get a URL like `https://node.example.com`.

On a **home PC** or extra machine (Device Node — files live here):

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/xa1onex/knot/main/scripts/install.sh)
```

Choose **2) Device Node**. Paste the panel URL and a join token from **Devices** in the panel.

From a git checkout: `bash scripts/install.sh` or `make install`.

Do **not** `curl | bash` — the wizard needs the keyboard. Use `bash <(curl …)` as above.

Full walkthrough: [docs/install.md](docs/install.md).

Native desktop/mobile apps are **not** part of Node. You use the web panel, `knot-agent` on each machine, and optionally `knot` / `knot-mcp`.

## Local development

```bash
make build
make run-cp
cd web && npm install && npm run dev
```

Bootstrap (loopback only): `admin@node.local` / `admin`  
API: `http://127.0.0.1:8787` · Dashboard: `http://localhost:5173`

See [docs/tls-and-remote.md](docs/tls-and-remote.md) if you bind beyond loopback.

## Layout

See [docs/architecture.md](docs/architecture.md), [docs/install.md](docs/install.md), [docs/hosting.md](docs/hosting.md), [docs/compute.md](docs/compute.md), [docs/client-sdk.md](docs/client-sdk.md), [docs/clients-platform.md](docs/clients-platform.md), [docs/files-sync.md](docs/files-sync.md), [docs/tls-and-remote.md](docs/tls-and-remote.md), [docs/node-network.md](docs/node-network.md), and [docs/direct-path.md](docs/direct-path.md).
