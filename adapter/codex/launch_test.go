package codex

import (
	"context"
	"strings"
	"testing"

	"github.com/hiveryn/agentruntime"
)

func TestPrepareLaunchInteractiveWithMCP(t *testing.T) {
	adapter := New(DefaultOptions())
	req := agentruntime.StartRequest{
		ID:      "hiveryn-session-1",
		Agent:   agentruntime.AgentCodex,
		Workdir: "/tmp/work",
		Prompt:  "Start architect session",
		Metadata: map[string]string{
			"session_kind":     "architect",
			"architect_folder": "/tmp/architect",
		},
		MCPServers: []agentruntime.MCPServerConfig{ApplyMCPApprovalDefaults(agentruntime.MCPServerConfig{
			Name:    "hiveryn",
			Command: "hiveryn-mcp-proxy",
			Args:    []string{"--daemon", "http://127.0.0.1:4200"},
			CWD:     "/tmp/architect",
		})},
	}

	spec, err := adapter.PrepareLaunch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Command != "codex" {
		t.Fatalf("command: %q", spec.Command)
	}

	joined := strings.Join(spec.Args, "\x00")
	for _, want := range []string{
		"--no-alt-screen",
		"--enable\x00codex_hooks",
		"--sandbox\x00read-only",
		"--config\x00approval_policy=\"never\"",
		"mcp_servers.hiveryn.command=\"hiveryn-mcp-proxy\"",
		"mcp_servers.hiveryn.args=[\"--daemon\",\"http://127.0.0.1:4200\"]",
		"mcp_servers.hiveryn.default_tools_approval_mode=\"approve\"",
		"mcp_servers.hiveryn.approval_mode=\"approve\"",
		"--cd\x00/tmp/work",
		"Start architect session",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("args missing %q\nargs=%q", want, spec.Args)
		}
	}

	if spec.Env["HIVERYN_SESSION_ID"] != "hiveryn-session-1" {
		t.Fatalf("HIVERYN_SESSION_ID: %q", spec.Env["HIVERYN_SESSION_ID"])
	}
	if spec.Env["HIVERYN_SESSION_KIND"] != "architect" {
		t.Fatalf("HIVERYN_SESSION_KIND: %q", spec.Env["HIVERYN_SESSION_KIND"])
	}
}

func TestPrepareLaunchHTTPMCP(t *testing.T) {
	adapter := New(DefaultOptions())
	req := agentruntime.StartRequest{
		ID:      "worker-1",
		Agent:   agentruntime.AgentCodex,
		Workdir: "/repo",
		MCPServers: []agentruntime.MCPServerConfig{ApplyMCPApprovalDefaults(agentruntime.MCPServerConfig{
			Name:              "hiveryn",
			URL:               "http://127.0.0.1:4200/mcp/worker-1",
			BearerTokenEnvVar: "HIVERYN_MCP_TOKEN",
		})},
		Env: map[string]string{"HIVERYN_MCP_TOKEN": "secret"},
		Metadata: map[string]string{
			"session_kind": "worker",
			"ticket_id":    "2026-05-01-0915-test",
		},
	}

	spec, err := adapter.PrepareLaunch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	joined := strings.Join(spec.Args, "\x00")
	for _, want := range []string{
		"mcp_servers.hiveryn.url=\"http://127.0.0.1:4200/mcp/worker-1\"",
		"mcp_servers.hiveryn.bearer_token_env_var=\"HIVERYN_MCP_TOKEN\"",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("args missing %q\nargs=%q", want, spec.Args)
		}
	}

	if spec.Env["HIVERYN_TICKET_ID"] == "" || spec.Env["HIVERYN_MCP_TOKEN"] != "secret" {
		t.Fatalf("env: %#v", spec.Env)
	}
}

func TestPrepareLaunchValidation(t *testing.T) {
	adapter := New(DefaultOptions())

	if _, err := adapter.PrepareLaunch(context.Background(), agentruntime.StartRequest{Agent: agentruntime.AgentCodex, Workdir: "/tmp"}); err == nil {
		t.Fatal("expected missing ID error")
	}
	if _, err := adapter.PrepareLaunch(context.Background(), agentruntime.StartRequest{ID: "x", Agent: agentruntime.AgentClaude, Workdir: "/tmp"}); err == nil {
		t.Fatal("expected unsupported agent error")
	}
	if _, err := adapter.PrepareLaunch(context.Background(), agentruntime.StartRequest{ID: "x", Workdir: "/tmp", Instructions: "be terse"}); err == nil {
		t.Fatal("expected unsupported instructions error")
	}
}
