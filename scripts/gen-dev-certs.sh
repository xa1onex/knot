#!/usr/bin/env bash
# Generate localhost TLS certs for knotd smoke tests (NOT for production).
set -euo pipefail
DIR="${1:-./certs}"
mkdir -p "$DIR"
openssl req -x509 -newkey rsa:2048 -sha256 -days 365 -nodes \
  -keyout "$DIR/server.key" \
  -out "$DIR/server.crt" \
  -subj "/CN=localhost" \
  -addext "subjectAltName=DNS:localhost,IP:127.0.0.1"
chmod 600 "$DIR/server.key"
echo "Wrote $DIR/server.crt and $DIR/server.key"
echo "Run: KNOT_TLS_CERT=$DIR/server.crt KNOT_TLS_KEY=$DIR/server.key KNOT_HTTP_ADDR=127.0.0.1:8787 ./bin/knotd"
