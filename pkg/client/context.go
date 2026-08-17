package client

import (
	"context"
	"crypto/rand"
	"encoding/hex"
)

type ctxKey int

const (
	mcpClientKey ctxKey = 1
	traceIDKey   ctxKey = 2
)

func WithMCPClient(ctx context.Context, name string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if name == "" {
		return ctx
	}
	return context.WithValue(ctx, mcpClientKey, name)
}

func MCPClientFrom(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(mcpClientKey).(string)
	return v
}

func WithTraceID(ctx context.Context, id string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, traceIDKey, id)
}

func TraceIDFrom(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(traceIDKey).(string)
	return v
}

func NewTraceID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "knottrace00000000"
	}
	return hex.EncodeToString(b)
}
