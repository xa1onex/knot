#!/usr/bin/env bash
# Node installer — Main Node (VPS panel) or Device Node (home PC / extra machine).
# Interactive:  bash scripts/install.sh
# From the net: bash <(curl -fsSL https://raw.githubusercontent.com/xa1onex/knot/main/scripts/install.sh)
# Do not pipe into bash (curl | bash) — the wizard needs the keyboard.
set -euo pipefail

NODE_GO_VERSION="${NODE_GO_VERSION:-1.25.0}"
NODE_NODE_VERSION="${NODE_NODE_VERSION:-20.18.1}"
KNOT_REPO="${KNOT_REPO:-https://github.com/xa1onex/knot.git}"
KNOT_REF="${KNOT_REF:-main}"

RED=$'\033[31m'
GRN=$'\033[32m'
YLW=$'\033[33m'
CYN=$'\033[36m'
BLD=$'\033[1m'
RST=$'\033[0m'

log()  { printf '%s\n' "$*"; }
ok()   { printf '%s✓%s %s\n' "$GRN" "$RST" "$*"; }
warn() { printf '%s!%s %s\n' "$YLW" "$RST" "$*"; }
err()  { printf '%s✗%s %s\n' "$RED" "$RST" "$*" >&2; }
die()  { err "$*"; exit 1; }

need_tty() {
  if [ -n "${KNOT_INSTALL_ROLE:-}" ]; then
    return 0
  fi
  if [ ! -t 0 ]; then
    die "This installer asks questions. Do not pipe it into bash.

  bash <(curl -fsSL https://raw.githubusercontent.com/xa1onex/knot/main/scripts/install.sh)

Or clone the repo and run:  bash scripts/install.sh"
  fi
}

# ask VAR "prompt" "default"  — skips if VAR already set in the environment.
ask() {
  local __var="$1" __prompt="$2" __def="${3:-}" __val=""
  if [ -n "${!__var:-}" ]; then
    return 0
  fi
  if [ -n "$__def" ]; then
    printf '%s [%s]: ' "$__prompt" "$__def"
  else
    printf '%s: ' "$__prompt"
  fi
  IFS= read -r __val || true
  if [ -z "$__val" ]; then
    __val="$__def"
  fi
  printf -v "$__var" '%s' "$__val"
}

ask_secret() {
  local __var="$1" __prompt="$2" __val="" __val2=""
  if [ -n "${!__var:-}" ]; then
    return 0
  fi
  while true; do
    printf '%s: ' "$__prompt"
    IFS= read -r -s __val || true
    printf '\n'
    printf 'Repeat password: '
    IFS= read -r -s __val2 || true
    printf '\n'
    if [ -z "$__val" ]; then
      warn "Password cannot be empty."
      continue
    fi
    if [ "$__val" != "$__val2" ]; then
      warn "Passwords do not match."
      continue
    fi
    if [ "${#__val}" -lt 8 ]; then
      warn "Use at least 8 characters."
      continue
    fi
    printf -v "$__var" '%s' "$__val"
    return 0
  done
}

ask_yn() {
  local __var="$1" __prompt="$2" __def="${3:-n}" __val=""
  if [ -n "${!__var:-}" ]; then
    return 0
  fi
  printf '%s [%s]: ' "$__prompt" "$__def"
  IFS= read -r __val || true
  if [ -z "$__val" ]; then
    __val="$__def"
  fi
  case "$__val" in
    y|Y|yes|YES) printf -v "$__var" '%s' "y" ;;
    *)           printf -v "$__var" '%s' "n" ;;
  esac
}

have() { command -v "$1" >/dev/null 2>&1; }

os_id() {
  case "$(uname -s)" in
    Linux)  echo linux ;;
    Darwin) echo darwin ;;
    *)      echo other ;;
  esac
}

cpu_id() {
  case "$(uname -m)" in
    x86_64|amd64) echo amd64 ;;
    aarch64|arm64) echo arm64 ;;
    *) die "Unsupported CPU: $(uname -m)" ;;
  esac
}

is_root() { [ "$(id -u)" -eq 0 ]; }

detect_src() {
  local here self
  self="${BASH_SOURCE[0]:-}"
  if [ -n "$self" ] && [ -f "$self" ]; then
    here="$(cd "$(dirname "$self")/.." && pwd)"
    if [ -f "$here/go.mod" ] && [ -d "$here/cmd/knotd" ]; then
      echo "$here"
      return 0
    fi
  fi
  if [ -f "$PWD/go.mod" ] && [ -d "$PWD/cmd/knotd" ]; then
    echo "$PWD"
    return 0
  fi
  echo ""
}

ensure_dirs() {
  mkdir -p "$KNOT_PREFIX/bin" "$KNOT_CONF" "$KNOT_DATA" "$KNOT_DATA/web" "$KNOT_CONF/tls"
}

install_build_deps_linux() {
  if have apt-get; then
    export DEBIAN_FRONTEND=noninteractive
    apt-get update -y
    apt-get install -y --no-install-recommends ca-certificates curl git tar xz-utils python3 openssl
  elif have dnf; then
    dnf install -y ca-certificates curl git tar xz python3 openssl
  elif have yum; then
    yum install -y ca-certificates curl git tar xz python3 openssl
  else
    warn "Install curl git python3 openssl yourself if a later step fails."
  fi
}

ensure_go() {
  if have go; then
    ok "Go $(go version | awk '{print $3}')"
    return 0
  fi
  local os arch tarball tmp
  os="$(os_id)"
  arch="$(cpu_id)"
  [ "$os" = "other" ] && die "Install Go ${NODE_GO_VERSION}+ from https://go.dev/dl/"
  tarball="go${NODE_GO_VERSION}.${os}-${arch}.tar.gz"
  tmp="$(mktemp -d)"
  log "Installing Go ${NODE_GO_VERSION}…"
  curl -fsSL "https://go.dev/dl/${tarball}" -o "$tmp/go.tgz"
  if is_root; then
    rm -rf /usr/local/go
    tar -C /usr/local -xzf "$tmp/go.tgz"
    ln -sfn /usr/local/go/bin/go /usr/local/bin/go
    ln -sfn /usr/local/go/bin/gofmt /usr/local/bin/gofmt
    export PATH="/usr/local/go/bin:/usr/local/bin:$PATH"
  else
    mkdir -p "$HOME/.local"
    rm -rf "$HOME/.local/go"
    tar -C "$HOME/.local" -xzf "$tmp/go.tgz"
    mkdir -p "$HOME/.local/bin"
    ln -sfn "$HOME/.local/go/bin/go" "$HOME/.local/bin/go"
    export PATH="$HOME/.local/go/bin:$HOME/.local/bin:$PATH"
  fi
  rm -rf "$tmp"
  have go || die "Go install failed"
  ok "Go $(go version | awk '{print $3}')"
}

ensure_node() {
  if have node && have npm; then
    ok "Node $(node -v)"
    return 0
  fi
  local os arch nodeos tmp
  os="$(os_id)"
  arch="$(cpu_id)"
  case "$os-$arch" in
    linux-amd64)  nodeos="linux-x64" ;;
    linux-arm64)  nodeos="linux-arm64" ;;
    darwin-amd64) nodeos="darwin-x64" ;;
    darwin-arm64) nodeos="darwin-arm64" ;;
    *) die "Install Node.js 20+ from https://nodejs.org/" ;;
  esac
  tmp="$(mktemp -d)"
  log "Installing Node.js v${NODE_NODE_VERSION}…"
  curl -fsSL "https://nodejs.org/dist/v${NODE_NODE_VERSION}/node-v${NODE_NODE_VERSION}-${nodeos}.tar.xz" -o "$tmp/node.txz"
  if is_root; then
    mkdir -p /usr/local/lib/nodejs
    tar -C /usr/local/lib/nodejs --strip-components=1 -xJf "$tmp/node.txz"
    ln -sfn /usr/local/lib/nodejs/bin/node /usr/local/bin/node
    ln -sfn /usr/local/lib/nodejs/bin/npm /usr/local/bin/npm
    ln -sfn /usr/local/lib/nodejs/bin/npx /usr/local/bin/npx
    export PATH="/usr/local/bin:$PATH"
  else
    mkdir -p "$HOME/.local"
    tar -C "$HOME/.local" --strip-components=1 -xJf "$tmp/node.txz"
    export PATH="$HOME/.local/bin:$PATH"
  fi
  rm -rf "$tmp"
  have node || die "Node.js install failed"
  ok "Node $(node -v)"
}

ensure_src() {
  if [ -n "${KNOT_SRC:-}" ] && [ -f "${KNOT_SRC}/go.mod" ]; then
    return 0
  fi
  KNOT_SRC="$(detect_src)"
  if [ -n "$KNOT_SRC" ]; then
    ok "Source: $KNOT_SRC"
    return 0
  fi
  KNOT_SRC="${KNOT_DATA}/src"
  log "Cloning Node from ${KNOT_REPO} (${KNOT_REF})…"
  mkdir -p "$(dirname "$KNOT_SRC")"
  if [ -d "$KNOT_SRC/.git" ]; then
    git -C "$KNOT_SRC" fetch --depth 1 origin "$KNOT_REF"
    git -C "$KNOT_SRC" checkout -q FETCH_HEAD
  else
    rm -rf "$KNOT_SRC"
    git clone --depth 1 --branch "$KNOT_REF" "$KNOT_REPO" "$KNOT_SRC" || \
      die "Could not clone ${KNOT_REPO}. Set KNOT_SRC=/path/to/knot or copy the repo here."
  fi
  ok "Source: $KNOT_SRC"
}

build_bins() {
  local what="$1" # all | agent
  log "Building Node binaries…"
  mkdir -p "$KNOT_SRC/bin"
  (
    cd "$KNOT_SRC"
    export CGO_ENABLED=0
    export PATH="${PATH}"
    if [ "$what" = "all" ]; then
      go build -o bin/knotd ./cmd/knotd
      go build -o bin/knot ./cmd/knot
      go build -o bin/knot-mcp ./cmd/knot-mcp
    fi
    go build -o bin/knot-agent ./cmd/knot-agent
  )
  if [ "$what" = "all" ]; then
    install -m 0755 "$KNOT_SRC/bin/knotd" "$KNOT_PREFIX/bin/knotd"
    install -m 0755 "$KNOT_SRC/bin/knot" "$KNOT_PREFIX/bin/knot"
    install -m 0755 "$KNOT_SRC/bin/knot-mcp" "$KNOT_PREFIX/bin/knot-mcp"
  fi
  install -m 0755 "$KNOT_SRC/bin/knot-agent" "$KNOT_PREFIX/bin/knot-agent"
  ok "Binaries in $KNOT_PREFIX/bin"
}

build_web() {
  log "Building web panel…"
  (
    cd "$KNOT_SRC/web"
    npm install --no-fund --no-audit
    npm run build
  )
  rm -rf "$KNOT_DATA/web"
  mkdir -p "$KNOT_DATA/web"
  cp -R "$KNOT_SRC/web/dist/." "$KNOT_DATA/web/"
  ok "Web panel → $KNOT_DATA/web"
}

ensure_knot_user() {
  if [ "$(os_id)" != "linux" ] || ! is_root; then
    return 0
  fi
  if id knot >/dev/null 2>&1; then
    return 0
  fi
  if have useradd; then
    useradd --system --home-dir "$KNOT_DATA" --shell /usr/sbin/nologin knot || \
      useradd --system --home-dir "$KNOT_DATA" --shell /bin/false knot
  fi
  ok "System user: knot"
}

chown_data() {
  if [ "$(os_id)" = "linux" ] && is_root && id knot >/dev/null 2>&1; then
    chown -R knot:knot "$KNOT_DATA" "$KNOT_CONF" || true
    chmod 700 "$KNOT_CONF" "$KNOT_DATA"
    chmod 600 "$KNOT_CONF"/*.env 2>/dev/null || true
  fi
}

write_main_env() {
  umask 077
  cat > "$KNOT_CONF/knotd.env" <<EOF
KNOT_HTTP_ADDR=${KNOT_HTTP_ADDR}
KNOT_DB_PATH=${KNOT_DATA}/knot.db
KNOT_STATIC_DIR=${KNOT_DATA}/web
KNOT_TLS_CERT=${KNOT_TLS_CERT}
KNOT_TLS_KEY=${KNOT_TLS_KEY}
KNOT_PUBLIC_BASE_URL=${KNOT_PUBLIC_BASE_URL}
KNOT_BOOTSTRAP_ADMIN=${KNOT_BOOTSTRAP_ADMIN}
KNOT_BOOTSTRAP_PASSWORD=${KNOT_BOOTSTRAP_PASSWORD}
KNOT_CORS_ORIGIN=${KNOT_PUBLIC_BASE_URL}
EOF
  chmod 600 "$KNOT_CONF/knotd.env"
}

write_agent_env() {
  umask 077
  cat > "$KNOT_CONF/knot-agent.env" <<EOF
KNOT_CONTROL_URL=${KNOT_CONTROL_URL}
KNOT_REGISTRATION_TOKEN=${KNOT_REGISTRATION_TOKEN}
KNOT_DEVICE_NAME=${KNOT_DEVICE_NAME}
KNOT_AGENT_DATA=${KNOT_DATA}/agent
KNOT_STORAGE_DIR=${KNOT_STORAGE_DIR}
${KNOT_TLS_INSECURE:+KNOT_TLS_INSECURE=${KNOT_TLS_INSECURE}}
${KNOT_TLS_CA:+KNOT_TLS_CA=${KNOT_TLS_CA}}
EOF
  chmod 600 "$KNOT_CONF/knot-agent.env"
}

install_systemd_main() {
  cat > /etc/systemd/system/knotd.service <<EOF
[Unit]
Description=Node Main (knotd control plane + web panel)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=knot
Group=knot
EnvironmentFile=-${KNOT_CONF}/knotd.env
ExecStart=${KNOT_PREFIX}/bin/knotd
Restart=always
RestartSec=3
LimitNOFILE=65536
AmbientCapabilities=CAP_NET_BIND_SERVICE
CapabilityBoundingSet=CAP_NET_BIND_SERVICE
NoNewPrivileges=true
WorkingDirectory=${KNOT_DATA}

[Install]
WantedBy=multi-user.target
EOF
  systemctl daemon-reload
  systemctl enable --now knotd
}

install_systemd_agent() {
  local user_line=""
  if [ -n "${KNOT_AGENT_USER:-}" ] && [ "$KNOT_AGENT_USER" != "root" ]; then
    user_line="User=${KNOT_AGENT_USER}"
  elif is_root && id knot >/dev/null 2>&1; then
    user_line="User=knot"
  fi
  cat > /etc/systemd/system/knot-agent.service <<EOF
[Unit]
Description=Node Device (knot-agent)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
${user_line}
EnvironmentFile=-${KNOT_CONF}/knot-agent.env
ExecStart=${KNOT_PREFIX}/bin/knot-agent
Restart=always
RestartSec=5
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
EOF
  systemctl daemon-reload
  systemctl enable --now knot-agent
}

install_launchd_main() {
  local plist="$HOME/Library/LaunchAgents/com.node.knotd.plist"
  mkdir -p "$HOME/Library/LaunchAgents"
  cat > "$plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>com.node.knotd</string>
  <key>ProgramArguments</key><array>
    <string>${KNOT_PREFIX}/bin/knotd</string>
  </array>
  <key>EnvironmentVariables</key><dict>
    <key>KNOT_HTTP_ADDR</key><string>${KNOT_HTTP_ADDR}</string>
    <key>KNOT_DB_PATH</key><string>${KNOT_DATA}/knot.db</string>
    <key>KNOT_STATIC_DIR</key><string>${KNOT_DATA}/web</string>
    <key>KNOT_TLS_CERT</key><string>${KNOT_TLS_CERT}</string>
    <key>KNOT_TLS_KEY</key><string>${KNOT_TLS_KEY}</string>
    <key>KNOT_PUBLIC_BASE_URL</key><string>${KNOT_PUBLIC_BASE_URL}</string>
    <key>KNOT_BOOTSTRAP_ADMIN</key><string>${KNOT_BOOTSTRAP_ADMIN}</string>
    <key>KNOT_BOOTSTRAP_PASSWORD</key><string>${KNOT_BOOTSTRAP_PASSWORD}</string>
    <key>KNOT_CORS_ORIGIN</key><string>${KNOT_PUBLIC_BASE_URL}</string>
  </dict>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>StandardOutPath</key><string>${KNOT_DATA}/knotd.log</string>
  <key>StandardErrorPath</key><string>${KNOT_DATA}/knotd.log</string>
</dict></plist>
EOF
  launchctl unload "$plist" 2>/dev/null || true
  launchctl load "$plist"
}

install_launchd_agent() {
  local plist="$HOME/Library/LaunchAgents/com.node.knot-agent.plist"
  mkdir -p "$HOME/Library/LaunchAgents"
  cat > "$plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>com.node.knot-agent</string>
  <key>ProgramArguments</key><array>
    <string>${KNOT_PREFIX}/bin/knot-agent</string>
  </array>
  <key>EnvironmentVariables</key><dict>
    <key>KNOT_CONTROL_URL</key><string>${KNOT_CONTROL_URL}</string>
    <key>KNOT_REGISTRATION_TOKEN</key><string>${KNOT_REGISTRATION_TOKEN}</string>
    <key>KNOT_DEVICE_NAME</key><string>${KNOT_DEVICE_NAME}</string>
    <key>KNOT_AGENT_DATA</key><string>${KNOT_DATA}/agent</string>
    <key>KNOT_STORAGE_DIR</key><string>${KNOT_STORAGE_DIR}</string>
EOF
  if [ -n "${KNOT_TLS_INSECURE:-}" ]; then
    printf '    <key>KNOT_TLS_INSECURE</key><string>%s</string>\n' "$KNOT_TLS_INSECURE" >> "$plist"
  fi
  if [ -n "${KNOT_TLS_CA:-}" ]; then
    printf '    <key>KNOT_TLS_CA</key><string>%s</string>\n' "$KNOT_TLS_CA" >> "$plist"
  fi
  cat >> "$plist" <<EOF
  </dict>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>StandardOutPath</key><string>${KNOT_DATA}/knot-agent.log</string>
  <key>StandardErrorPath</key><string>${KNOT_DATA}/knot-agent.log</string>
</dict></plist>
EOF
  launchctl unload "$plist" 2>/dev/null || true
  launchctl load "$plist"
}

open_firewall() {
  local port="$1"
  if have ufw && ufw status 2>/dev/null | grep -q "Status: active"; then
    ufw allow "${port}/tcp" || true
    if [ "${KNOT_TLS_MODE:-}" = "letsencrypt" ]; then
      ufw allow 80/tcp || true
    fi
    ok "Opened ufw port ${port}"
  fi
}

issue_self_signed() {
  local cn="$1" ip="$2"
  mkdir -p "$KNOT_CONF/tls"
  local san="DNS:${cn}"
  if [ -n "$ip" ]; then
    san="${san},IP:${ip}"
  fi
  if [ "$cn" != "localhost" ]; then
    san="${san},DNS:localhost"
  fi
  openssl req -x509 -nodes -newkey rsa:2048 -days 825 \
    -keyout "$KNOT_CONF/tls/privkey.pem" \
    -out "$KNOT_CONF/tls/fullchain.pem" \
    -subj "/CN=${cn}" \
    -addext "subjectAltName=${san}" >/dev/null 2>&1 || \
  openssl req -x509 -nodes -newkey rsa:2048 -days 825 \
    -keyout "$KNOT_CONF/tls/privkey.pem" \
    -out "$KNOT_CONF/tls/fullchain.pem" \
    -subj "/CN=${cn}"
  chmod 600 "$KNOT_CONF/tls/privkey.pem"
  cp "$KNOT_CONF/tls/fullchain.pem" "$KNOT_CONF/tls/ca.pem"
  KNOT_TLS_CERT="$KNOT_CONF/tls/fullchain.pem"
  KNOT_TLS_KEY="$KNOT_CONF/tls/privkey.pem"
  ok "Self-signed TLS certificate"
}

issue_letsencrypt() {
  local domain="$1" email="$2"
  if ! have certbot; then
    if have apt-get; then
      export DEBIAN_FRONTEND=noninteractive
      apt-get install -y certbot
    elif have dnf; then
      dnf install -y certbot
    else
      die "Install certbot, then re-run."
    fi
  fi
  # Port 80 must be free for standalone HTTP-01.
  certbot certonly --standalone --non-interactive --agree-tos --email "$email" -d "$domain"
  KNOT_TLS_CERT="/etc/letsencrypt/live/${domain}/fullchain.pem"
  KNOT_TLS_KEY="/etc/letsencrypt/live/${domain}/privkey.pem"
  mkdir -p /etc/letsencrypt/renewal-hooks/deploy
  cat > /etc/letsencrypt/renewal-hooks/deploy/knotd.sh <<'HOOK'
#!/bin/sh
if command -v systemctl >/dev/null 2>&1; then
  systemctl kill -s HUP knotd 2>/dev/null || true
fi
HOOK
  chmod +x /etc/letsencrypt/renewal-hooks/deploy/knotd.sh
  # knotd runs as user knot — copy readable certs on each renew.
  mkdir -p "$KNOT_CONF/tls"
  cp "$KNOT_TLS_CERT" "$KNOT_CONF/tls/fullchain.pem"
  cp "$KNOT_TLS_KEY" "$KNOT_CONF/tls/privkey.pem"
  chmod 640 "$KNOT_CONF/tls/privkey.pem"
  KNOT_TLS_CERT="$KNOT_CONF/tls/fullchain.pem"
  KNOT_TLS_KEY="$KNOT_CONF/tls/privkey.pem"
  cat > /etc/letsencrypt/renewal-hooks/deploy/knotd-copy.sh <<HOOK
#!/bin/sh
cp /etc/letsencrypt/live/${domain}/fullchain.pem ${KNOT_CONF}/tls/fullchain.pem
cp /etc/letsencrypt/live/${domain}/privkey.pem ${KNOT_CONF}/tls/privkey.pem
chmod 640 ${KNOT_CONF}/tls/privkey.pem
chown knot:knot ${KNOT_CONF}/tls/fullchain.pem ${KNOT_CONF}/tls/privkey.pem 2>/dev/null || true
if command -v systemctl >/dev/null 2>&1; then
  systemctl kill -s HUP knotd 2>/dev/null || true
fi
HOOK
  chmod +x /etc/letsencrypt/renewal-hooks/deploy/knotd-copy.sh
  ok "Let's Encrypt certificate for ${domain}"
}

guess_ip() {
  curl -4 -fsS --max-time 4 https://ifconfig.me 2>/dev/null || \
    curl -4 -fsS --max-time 4 https://api.ipify.org 2>/dev/null || \
    hostname -I 2>/dev/null | awk '{print $1}' || true
}

wait_health() {
  local url="$1" i
  for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15; do
    if curl -sk --max-time 2 "${url}/healthz" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  return 1
}

create_join_token() {
  local base="$1" email="$2" password="$3"
  JOIN_TOKEN="$(python3 - "$base" "$email" "$password" <<'PY'
import json, ssl, sys, urllib.error, urllib.request
base, email, password = sys.argv[1], sys.argv[2], sys.argv[3]
ctx = ssl._create_unverified_context()
def post(path, payload, token=""):
    req = urllib.request.Request(
        base + path,
        data=json.dumps(payload).encode(),
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    if token:
        req.add_header("Authorization", "Bearer " + token)
    with urllib.request.urlopen(req, context=ctx, timeout=10) as resp:
        return json.loads(resp.read().decode())
try:
    login = post("/v1/auth/login", {"email": email, "password": password})
    access = login.get("access_token") or login.get("token") or ""
    if not access:
        sys.exit(0)
    out = post("/v1/devices/registration-tokens", {"name_hint": "first-device", "ttl_hours": 24}, access)
    print(out.get("token") or "")
except Exception:
    pass
PY
)"
  if [ -n "${JOIN_TOKEN:-}" ]; then
    umask 077
    printf '%s\n' "$JOIN_TOKEN" > "$KNOT_CONF/join-token.txt"
    chmod 600 "$KNOT_CONF/join-token.txt"
    ok "Join token saved to ${KNOT_CONF}/join-token.txt (24h, one-time)"
  else
    warn "Could not auto-create a join token. Create one in the panel → Devices."
  fi
}

banner() {
  printf '\n%s%sNode%s — one panel for your machines\n\n' "$BLD" "$CYN" "$RST"
  log "  Main Node    = VPS: web panel + control plane (always online)"
  log "  Device Node  = home PC / extra VPS / Pi: files and apps live here"
  log "  Devices dial out. Home PC does not need a public IP."
  printf '\n'
}

wizard_role() {
  if [ -n "${KNOT_INSTALL_ROLE:-}" ]; then
    ROLE="$KNOT_INSTALL_ROLE"
    return 0
  fi
  log "${BLD}What is this machine?${RST}"
  log "  1) Main Node     — install the panel (usually a VPS with a public IP)"
  log "  2) Device Node   — connect this computer to an existing panel"
  printf '\n'
  ask ROLE "Choice" "1"
  case "$ROLE" in
    1|main|Main|MAIN) ROLE=main ;;
    2|device|Device|DEVICE|secondary) ROLE=device ;;
    *) die "Choose 1 or 2" ;;
  esac
}

setup_prefixes() {
  OS="$(os_id)"
  if is_root; then
    KNOT_PREFIX="${KNOT_PREFIX:-/usr/local}"
    KNOT_CONF="${KNOT_CONF:-/etc/knot}"
    KNOT_DATA="${KNOT_DATA:-/var/lib/knot}"
  else
    KNOT_PREFIX="${KNOT_PREFIX:-$HOME/.local}"
    KNOT_CONF="${KNOT_CONF:-$HOME/.knot}"
    KNOT_DATA="${KNOT_DATA:-$HOME/.knot}"
    export PATH="$KNOT_PREFIX/bin:$PATH"
  fi
}

install_main() {
  local port domain ip tls_choice
  ip="$(guess_ip)"
  ask KNOT_DOMAIN "Domain for the panel (empty = IP / hostname)" "${KNOT_DOMAIN:-}"
  domain="${KNOT_DOMAIN:-}"
  if [ "$(os_id)" = "darwin" ] && ! is_root; then
    ask KNOT_PORT "Panel port" "${KNOT_PORT:-8443}"
  else
    ask KNOT_PORT "Panel port" "${KNOT_PORT:-443}"
  fi
  port="${KNOT_PORT}"
  if [ "$port" -lt 1024 ] && ! is_root; then
    die "Port ${port} needs root. Re-run with sudo, or choose a port ≥ 1024 (e.g. 8443)."
  fi
  ask KNOT_BOOTSTRAP_ADMIN "Admin email" "${KNOT_BOOTSTRAP_ADMIN:-admin@node.local}"
  ask_secret KNOT_BOOTSTRAP_PASSWORD "Admin password"

  if [ "$KNOT_BOOTSTRAP_PASSWORD" = "admin" ] && [ "$port" != "loopback" ]; then
    die "Password 'admin' is not allowed on a public panel. Choose a stronger one."
  fi

  if [ -z "$domain" ]; then
    domain="${ip:-$(hostname)}"
    KNOT_TLS_MODE="${KNOT_TLS_MODE:-selfsigned}"
    warn "No domain — using ${domain} and a self-signed certificate."
    warn "Browsers will warn. Device Nodes must allow this cert (the installer asks)."
  else
    if [ -z "${KNOT_TLS_MODE:-}" ]; then
      log ""
      log "${BLD}TLS${RST}"
      log "  1) Let's Encrypt  (needs DNS pointing here + port 80 free)"
      log "  2) Self-signed    (works with an IP; browser warning)"
      ask tls_choice "Choice" "1"
      case "$tls_choice" in
        2|self|selfsigned) KNOT_TLS_MODE=selfsigned ;;
        *) KNOT_TLS_MODE=letsencrypt ;;
      esac
    fi
  fi

  if [ "${KNOT_PUBLIC_BASE_URL:-}" = "" ]; then
    if [ "$port" = "443" ]; then
      KNOT_PUBLIC_BASE_URL="https://${domain}"
    else
      KNOT_PUBLIC_BASE_URL="https://${domain}:${port}"
    fi
  fi
  KNOT_HTTP_ADDR="0.0.0.0:${port}"

  if [ "$(os_id)" = "linux" ] && is_root; then
    install_build_deps_linux
  fi
  ensure_dirs
  ensure_go
  ensure_node
  ensure_src
  ensure_knot_user
  build_bins all
  build_web

  case "$KNOT_TLS_MODE" in
    letsencrypt)
      issue_letsencrypt "$domain" "$KNOT_BOOTSTRAP_ADMIN"
      ;;
    files)
      [ -n "${KNOT_TLS_CERT:-}" ] && [ -n "${KNOT_TLS_KEY:-}" ] || die "Set KNOT_TLS_CERT and KNOT_TLS_KEY"
      ;;
    *)
      issue_self_signed "$domain" "$ip"
      KNOT_TLS_MODE=selfsigned
      ;;
  esac

  write_main_env
  open_firewall "$port"
  chown_data

  log "Starting Main Node…"
  case "$OS" in
    linux)
      is_root || die "Main Node on Linux needs root (sudo) so it can install the service."
      install_systemd_main
      ;;
    darwin)
      write_main_env
      install_launchd_main
      ;;
    *)
      die "Unsupported OS for a service. Run knotd from ${KNOT_CONF}/knotd.env manually."
      ;;
  esac

  local health="https://127.0.0.1:${port}"
  if wait_health "$health"; then
    ok "Control plane is up"
  else
    warn "Panel did not answer /healthz yet. Check logs and ${KNOT_CONF}/knotd.env"
  fi

  create_join_token "$health" "$KNOT_BOOTSTRAP_ADMIN" "$KNOT_BOOTSTRAP_PASSWORD"

  printf '\n%s%sMain Node is ready.%s\n\n' "$BLD" "$GRN" "$RST"
  log "  Panel:    ${KNOT_PUBLIC_BASE_URL}"
  log "  Login:    ${KNOT_BOOTSTRAP_ADMIN}"
  log "  Password: (the one you just set)"
  printf '\n'
  log "${BLD}Add a Device Node${RST} (home PC, extra VPS, Pi):"
  log "  1. Open the panel → Devices → Create token"
  if [ -n "${JOIN_TOKEN:-}" ]; then
    log "     (a first token is already printed below — one-time, 24h)"
  fi
  log "  2. On that machine run:"
  log "       bash <(curl -fsSL https://raw.githubusercontent.com/xa1onex/knot/main/scripts/install.sh)"
  log "     choose  2) Device Node"
  log "     URL:    ${KNOT_PUBLIC_BASE_URL}"
  if [ -n "${JOIN_TOKEN:-}" ]; then
    log "     Token:  ${JOIN_TOKEN}"
  fi
  if [ "$KNOT_TLS_MODE" = "selfsigned" ]; then
    printf '\n'
    warn "Self-signed TLS: on the Device Node, answer that the panel uses a self-signed cert."
  fi
  printf '\n'
}

install_device() {
  local def_name
  def_name="$(hostname -s 2>/dev/null || hostname || echo device)"
  ask KNOT_CONTROL_URL "Main Node URL (https://node.example.com)" "${KNOT_CONTROL_URL:-}"
  [ -n "$KNOT_CONTROL_URL" ] || die "Main Node URL is required"
  KNOT_CONTROL_URL="${KNOT_CONTROL_URL%/}"
  ask KNOT_REGISTRATION_TOKEN "Join token (from the panel → Devices)" "${KNOT_REGISTRATION_TOKEN:-}"
  [ -n "$KNOT_REGISTRATION_TOKEN" ] || die "Join token is required"
  ask KNOT_DEVICE_NAME "Name for this device" "${KNOT_DEVICE_NAME:-$def_name}"
  ask KNOT_STORAGE_DIR "Folder for files on this machine" "${KNOT_STORAGE_DIR:-$HOME/knot-storage}"
  mkdir -p "$KNOT_STORAGE_DIR"

  if [ -z "${KNOT_TLS_INSECURE:-}" ]; then
    ask_yn KNOT_SELF_SIGNED "Does the panel use a self-signed certificate?" "n"
    if [ "$KNOT_SELF_SIGNED" = "y" ]; then
      KNOT_TLS_INSECURE=1
    fi
  fi

  if [ "$(os_id)" = "linux" ] && is_root; then
    install_build_deps_linux
  fi
  ensure_dirs
  ensure_go
  ensure_src
  build_bins agent
  mkdir -p "$KNOT_DATA/agent"
  write_agent_env

  if is_root && [ -n "${SUDO_USER:-}" ] && [ "$SUDO_USER" != "root" ]; then
    KNOT_AGENT_USER="$SUDO_USER"
    # Keep files in the desktop user's home when they used sudo.
    if [ "$KNOT_STORAGE_DIR" = "$HOME/knot-storage" ]; then
      uh="$(eval echo "~$SUDO_USER")"
      KNOT_STORAGE_DIR="${uh}/knot-storage"
      mkdir -p "$KNOT_STORAGE_DIR"
      write_agent_env
    fi
  fi

  log "Starting Device Node…"
  case "$(os_id)" in
    linux)
      if is_root; then
        ensure_knot_user
        if [ -n "${KNOT_AGENT_USER:-}" ]; then
          chown -R "$KNOT_AGENT_USER:" "$KNOT_DATA" "$KNOT_CONF" "$KNOT_STORAGE_DIR" || true
        else
          chown_data
        fi
        install_systemd_agent
      else
        warn "No root — starting knot-agent in the background for this session."
        warn "Re-run with sudo to install a permanent service."
        set -a
        # shellcheck disable=SC1090
        . "$KNOT_CONF/knot-agent.env"
        set +a
        nohup "$KNOT_PREFIX/bin/knot-agent" >>"$KNOT_DATA/knot-agent.log" 2>&1 &
      fi
      ;;
    darwin)
      install_launchd_agent
      ;;
    *)
      die "Unsupported OS. Start: $KNOT_PREFIX/bin/knot-agent"
      ;;
  esac

  printf '\n%s%sDevice Node connected.%s\n\n' "$BLD" "$GRN" "$RST"
  log "  Panel:   ${KNOT_CONTROL_URL}"
  log "  Device:  ${KNOT_DEVICE_NAME}"
  log "  Files:   ${KNOT_STORAGE_DIR}"
  log "  Open the panel on your phone or laptop and this machine should show online."
  printf '\n'
}

main() {
  need_tty
  banner
  setup_prefixes
  wizard_role
  case "$ROLE" in
    main)   install_main ;;
    device) install_device ;;
    *) die "Unknown role $ROLE" ;;
  esac
}

main "$@"
