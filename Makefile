.PHONY: build test run-cp run-agent tidy clean build-windows sdk-js install

# Prefer a Go on PATH; fall back to the common local install used in this repo.
GO ?= $(shell command -v go 2>/dev/null || echo "$(HOME)/.local/go/bin/go")
export PATH := $(HOME)/.local/go/bin:$(PATH)

BIN_DIR := bin

build:
	mkdir -p $(BIN_DIR)
	$(GO) build -o $(BIN_DIR)/knotd ./cmd/knotd
	$(GO) build -o $(BIN_DIR)/knot-agent ./cmd/knot-agent
	$(GO) build -o $(BIN_DIR)/knot ./cmd/knot
	$(GO) build -o $(BIN_DIR)/knot-mcp ./cmd/knot-mcp

install:
	bash scripts/install.sh

build-windows:
	mkdir -p $(BIN_DIR)
	GOOS=windows GOARCH=amd64 $(GO) build -o $(BIN_DIR)/knot-agent.exe ./cmd/knot-agent
	GOOS=windows GOARCH=amd64 $(GO) build -o $(BIN_DIR)/knot.exe ./cmd/knot

sdk-js:
	cd sdk/js && npm install && npm run build

test:
	$(GO) test ./internal/... ./pkg/... ./cmd/... ./tests/integration

tidy:
	$(GO) mod tidy

run-cp: build
	mkdir -p data
	KNOT_HTTP_ADDR=127.0.0.1:8787 KNOT_DB_PATH=./data/knot.db \
	KNOT_BOOTSTRAP_ADMIN=admin@node.local KNOT_BOOTSTRAP_PASSWORD=admin \
	KNOT_CORS_ORIGIN=http://localhost:5173 \
	./$(BIN_DIR)/knotd

run-agent: build
	./$(BIN_DIR)/knot-agent -control-url http://127.0.0.1:8787

clean:
	rm -rf $(BIN_DIR) data/knot.db
