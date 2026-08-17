package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/knot-infra/knot/internal/mcp"
	"github.com/knot-infra/knot/pkg/client"
)

func TestToolsCatalog(t *testing.T) {
	s := &mcp.Server{Client: client.New("http://127.0.0.1:1", "x")}
	tools := s.Tools()
	want := map[string]bool{
		mcp.ToolDevicesList: true, mcp.ToolStorageList: true, mcp.ToolStorageStat: true,
		mcp.ToolStorageRead: true, mcp.ToolStorageUpload: true, mcp.ToolStorageDownload: true,
		mcp.ToolFilesSearch: true, mcp.ToolServicesList: true, mcp.ToolServicesRegister: true,
		mcp.ToolRoutesList: true, mcp.ToolRoutesAdd: true, mcp.ToolComputeList: true,
		mcp.ToolComputeLabels: true,
		mcp.ToolJobsList:      true, mcp.ToolJobsCreate: true, mcp.ToolJobsGet: true,
		mcp.ToolJobsCancel: true, mcp.ToolJobsLogs: true, mcp.ToolJobsArtifacts: true,
		mcp.ToolSecretsList: true, mcp.ToolEnvList: true, mcp.ToolDeployEnvironment: true,
		mcp.ToolSourceList: true, mcp.ToolBuildCreate: true, mcp.ToolBuildStatus: true, mcp.ToolBuildLogs: true,
		mcp.ToolReleaseList: true, mcp.ToolReleaseStatus: true, mcp.ToolReleaseRollback: true,
		mcp.ToolTrafficStatus: true, mcp.ToolTrafficSwitch: true,
		mcp.ToolLogsSearch: true, mcp.ToolLogsTail: true, mcp.ToolLogsService: true,
		mcp.ToolOpsContext:   true,
		mcp.ToolWorkflowList: true, mcp.ToolWorkflowRun: true, mcp.ToolWorkflowStatus: true,
		mcp.ToolAISession: true,
		mcp.ToolAuditSearch: true, mcp.ToolAuditAIActivity: true, mcp.ToolAuditTrace: true,
		mcp.ToolPlanCreate: true, mcp.ToolPlanStatus: true, mcp.ToolPlanApprove: true, mcp.ToolPlanCancel: true,
	}
	if len(tools) != len(want) {
		t.Fatalf("got %d tools", len(tools))
	}
	for _, tool := range tools {
		if !want[tool.Name] {
			t.Fatalf("unexpected tool %s", tool.Name)
		}
	}
}

func TestStdioInitializeAndList(t *testing.T) {
	s := &mcp.Server{Client: client.New("http://127.0.0.1:1", "x")}
	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}
{"jsonrpc":"2.0","id":2,"method":"tools/list"}
`)
	var out bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.ServeStdio(ctx, in, &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected 2 responses, got %q", out.String())
	}
	var initRes map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &initRes); err != nil {
		t.Fatal(err)
	}
	if initRes["error"] != nil {
		t.Fatalf("init error: %v", initRes["error"])
	}
	var listRes struct {
		Result struct {
			Tools []mcp.ToolDesc `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &listRes); err != nil {
		t.Fatal(err)
	}
	if len(listRes.Result.Tools) != 46 {
		t.Fatalf("tools=%d", len(listRes.Result.Tools))
	}
}
