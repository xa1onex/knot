package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/knot-infra/knot/internal/mcp"
	"github.com/knot-infra/knot/pkg/client"
)

func main() {
	apiURL := flag.String("api-url", envOr("KNOT_API_URL", "http://127.0.0.1:8787"), "Node Control Plane URL")
	token := flag.String("token", os.Getenv("KNOT_API_TOKEN"), "API credential or session token")
	flag.Parse()

	if *token == "" {
		fmt.Fprintln(os.Stderr, "knot-mcp: set -token or KNOT_API_TOKEN (same credentials as CLI)")
		os.Exit(1)
	}

	cl := client.New(*apiURL, *token)
	srv := &mcp.Server{Client: cl, MCPClient: envOr("KNOT_MCP_CLIENT", "knot-mcp")}

	args := flag.Args()
	// Legacy / script mode: knot-mcp call <tool> '<json-args>'
	if len(args) > 0 && args[0] == "call" {
		if len(args) < 2 {
			fatal("usage: knot-mcp call <tool> [json-args]")
		}
		argMap := map[string]any{}
		if len(args) >= 3 && args[2] != "" {
			if err := json.Unmarshal([]byte(args[2]), &argMap); err != nil {
				fatal("invalid json args: " + err.Error())
			}
		}
		out, err := srv.Call(context.Background(), args[1], argMap)
		must(err)
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(out)
		return
	}
	if len(args) > 0 && args[0] == "tools" {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"tools": srv.Tools()})
		return
	}

	// Default: MCP stdio server for Cursor / Claude Code / etc.
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	if err := srv.ServeStdio(ctx, os.Stdin, os.Stdout); err != nil && err != context.Canceled {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func fatal(msg string) {
	fmt.Fprintln(os.Stderr, msg)
	os.Exit(1)
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
