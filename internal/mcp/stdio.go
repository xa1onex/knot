package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
)

// JSON-RPC 2.0 + minimal MCP (initialize, tools/list, tools/call) over stdio.
// No AI logic — only protocol framing around Server.Call.

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type callParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

// ServeStdio runs the MCP JSON-RPC loop on in/out until EOF or context cancel.
func (s *Server) ServeStdio(ctx context.Context, in io.Reader, out io.Writer) error {
	rd := bufio.NewReader(in)
	enc := json.NewEncoder(out)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		line, err := rd.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		if len(line) == 0 || (len(line) == 1 && line[0] == '\n') {
			continue
		}
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			_ = enc.Encode(rpcResponse{
				JSONRPC: "2.0",
				Error:   &rpcError{Code: -32700, Message: "parse error"},
			})
			continue
		}
		if req.Method == "" {
			continue
		}
		// Notifications (no id) — acknowledge initialize-related only by ignoring.
		if len(req.ID) == 0 || string(req.ID) == "null" {
			if req.Method == "notifications/initialized" {
				continue
			}
			continue
		}
		res := s.handle(ctx, req)
		if err := enc.Encode(res); err != nil {
			return err
		}
	}
}

func (s *Server) handle(ctx context.Context, req rpcRequest) rpcResponse {
	resp := rpcResponse{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		resp.Result = map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
			"serverInfo": map[string]any{
				"name":    "knot-mcp",
				"version": "3.5.0",
				"title":   "Node (knot) — external client layer",
			},
			"instructions": "Thin MCP wrapper over Node API. Auth via KNOT_API_TOKEN. No AI inside Node.",
		}
	case "ping":
		resp.Result = map[string]any{}
	case "tools/list":
		resp.Result = map[string]any{"tools": s.Tools()}
	case "tools/call":
		var p callParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			resp.Error = &rpcError{Code: -32602, Message: "invalid params"}
			return resp
		}
		if p.Arguments == nil {
			p.Arguments = map[string]any{}
		}
		out, err := s.Call(ctx, p.Name, p.Arguments)
		if err != nil {
			// Surface API errors as tool result isError so MCP clients show them.
			resp.Result = map[string]any{
				"content": []map[string]any{{"type": "text", "text": err.Error()}},
				"isError": true,
			}
			return resp
		}
		raw, _ := json.MarshalIndent(out, "", "  ")
		resp.Result = map[string]any{
			"content": []map[string]any{{"type": "text", "text": string(raw)}},
			"isError": false,
		}
	default:
		resp.Error = &rpcError{Code: -32601, Message: fmt.Sprintf("method not found: %s", req.Method)}
	}
	return resp
}
