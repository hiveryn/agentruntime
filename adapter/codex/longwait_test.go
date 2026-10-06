package codex

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/hiveryn/agentruntime"
)

func TestPrepareLaunchDisableNativeQuestions(t *testing.T) {
	adapter := New(DefaultOptions())
	const want = "--config\x00tools.experimental_request_user_input.enabled=false"
	for _, tc := range []struct {
		name string
		req  agentruntime.StartRequest
	}{
		{"new", agentruntime.StartRequest{}},
		{"resume-id", agentruntime.StartRequest{Resume: true, ResumeID: "abc"}},
		{"resume-bare", agentruntime.StartRequest{Resume: true}},
		{"headless", agentruntime.StartRequest{RunMode: agentruntime.RunHeadless}},
	} {
		req := tc.req
		req.ID, req.Workdir, req.Prompt, req.DisableNativeQuestions = "s", "/tmp/work", "hi", true
		spec, err := adapter.PrepareLaunch(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(strings.Join(spec.Args, "\x00"), want) {
			t.Fatalf("%s: args missing question-tool disable: %q", tc.name, spec.Args)
		}
	}

	spec, err := adapter.PrepareLaunch(context.Background(), agentruntime.StartRequest{ID: "s", Workdir: "/tmp/work"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(spec.Args, " "), "request_user_input") {
		t.Fatalf("question tool disabled without DisableNativeQuestions: %q", spec.Args)
	}
}

func TestPrepareLaunchMCPToolTimeout(t *testing.T) {
	adapter := New(DefaultOptions())
	for _, resume := range []bool{false, true} {
		spec, err := adapter.PrepareLaunch(context.Background(), agentruntime.StartRequest{
			ID: "s", Workdir: "/tmp/work", Resume: resume, ResumeID: "abc",
			MCPServers: []agentruntime.MCPServerConfig{
				{Name: "hiveryn", Command: "hiverynd", ToolTimeout: 65 * time.Minute},
				{Name: "half", URL: "http://x", ToolTimeout: 1500 * time.Millisecond},
				{Name: "other", Command: "other"},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		joined := strings.Join(spec.Args, "\x00")
		for _, want := range []string{
			"--config\x00mcp_servers.hiveryn.tool_timeout_sec=3900",
			"--config\x00mcp_servers.half.tool_timeout_sec=1.5",
		} {
			if !strings.Contains(joined, want) {
				t.Fatalf("resume=%v: args missing %q: %q", resume, want, spec.Args)
			}
		}
		if strings.Contains(joined, "mcp_servers.other.tool_timeout_sec") {
			t.Fatalf("resume=%v: timeout set without ToolTimeout: %q", resume, spec.Args)
		}
	}

	if _, err := adapter.PrepareLaunch(context.Background(), agentruntime.StartRequest{
		ID: "s", Workdir: "/tmp/work",
		MCPServers: []agentruntime.MCPServerConfig{{Name: "x", Command: "x", ToolTimeout: -time.Second}},
	}); err == nil || !strings.Contains(err.Error(), "negative tool timeout") {
		t.Fatalf("expected negative timeout error, got %v", err)
	}
}
