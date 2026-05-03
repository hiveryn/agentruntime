package claude

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/hiveryn/agentruntime"
)

func TestPrepareLaunchInteractiveWithMCP(t *testing.T) {
	adapter := New(Options{
		NewSessionID: func() (string, error) { return "00000000-0000-4000-8000-000000000001", nil },
	})
	req := agentruntime.StartRequest{
		ID:           "hiv-claude-1",
		Agent:        agentruntime.AgentClaude,
		Workdir:      "/tmp/work",
		Prompt:       "Summarize this repository.",
		Instructions: "Be terse. Prefer bullets.",
		MCPServers: []agentruntime.MCPServerConfig{{
			Name:    "my-server",
			Command: "my-mcp-proxy",
			Args:    []string{"--daemon", "http://127.0.0.1:4200"},
			CWD:     "/tmp/work",
		}},
	}

	spec, err := adapter.PrepareLaunch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Command != "claude" {
		t.Fatalf("command: %q", spec.Command)
	}
	if got := spec.Args[0]; got != req.Prompt {
		t.Fatalf("prompt arg = %q, want %q", got, req.Prompt)
	}
	if !hasArgPair(spec.Args, "--system-prompt", req.Instructions) {
		t.Fatalf("args missing system prompt flag: %q", spec.Args)
	}
	if !hasArgPair(spec.Args, "--session-id", "00000000-0000-4000-8000-000000000001") {
		t.Fatalf("args missing session ID: %q", spec.Args)
	}
	if len(spec.CleanupPaths) != 1 {
		t.Fatalf("CleanupPaths: %#v", spec.CleanupPaths)
	}
	if spec.Env["AGENTRUNTIME_SESSION_ID"] != "hiv-claude-1" {
		t.Fatalf("AGENTRUNTIME_SESSION_ID: %q", spec.Env["AGENTRUNTIME_SESSION_ID"])
	}

	config := readMCPConfig(t, spec.CleanupPaths[0])
	server, ok := config.MCPServers["my-server"]
	if !ok {
		t.Fatalf("missing my-server: %#v", config.MCPServers)
	}
	if server.Type != "stdio" || server.Command != "my-mcp-proxy" {
		t.Fatalf("server: %#v", server)
	}
	if len(server.Args) != 2 || server.Args[1] != "http://127.0.0.1:4200" {
		t.Fatalf("server args: %#v", server.Args)
	}
	if server.Env["PWD"] != "/tmp/work" {
		t.Fatalf("server env: %#v", server.Env)
	}

	for _, path := range spec.CleanupPaths {
		_ = os.Remove(path)
	}
}

func TestPrepareLaunchHTTPMCPAndAppendInstructions(t *testing.T) {
	adapter := New(Options{
		AppendInstructions: true,
		NewSessionID:       func() (string, error) { return "native-session-1", nil },
	})
	req := agentruntime.StartRequest{
		ID:           "worker-1",
		Agent:        agentruntime.AgentClaude,
		Workdir:      "/repo",
		Instructions: "Use the project MCP server when relevant.",
		Env:          map[string]string{"MY_MCP_TOKEN": "secret"},
		MCPServers: []agentruntime.MCPServerConfig{{
			Name:              "my-server",
			URL:               "https://127.0.0.1:4200/mcp/worker-1",
			BearerTokenEnvVar: "MY_MCP_TOKEN",
		}},
	}

	spec, err := adapter.PrepareLaunch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !hasArgPair(spec.Args, "--append-system-prompt", req.Instructions) {
		t.Fatalf("args missing append system prompt flag: %q", spec.Args)
	}
	if spec.Env["MY_MCP_TOKEN"] != "secret" {
		t.Fatalf("env: %#v", spec.Env)
	}

	config := readMCPConfig(t, spec.CleanupPaths[0])
	server := config.MCPServers["my-server"]
	if server.Type != "http" || server.URL != "https://127.0.0.1:4200/mcp/worker-1" {
		t.Fatalf("server: %#v", server)
	}
	if server.Headers["Authorization"] != "Bearer ${MY_MCP_TOKEN}" {
		t.Fatalf("server headers: %#v", server.Headers)
	}

	for _, path := range spec.CleanupPaths {
		_ = os.Remove(path)
	}
}

func TestPrepareLaunchValidation(t *testing.T) {
	adapter := New(DefaultOptions())

	if _, err := adapter.PrepareLaunch(context.Background(), agentruntime.StartRequest{Agent: agentruntime.AgentClaude, Workdir: "/tmp"}); err == nil {
		t.Fatal("expected missing ID error")
	}
	if _, err := adapter.PrepareLaunch(context.Background(), agentruntime.StartRequest{ID: "x", Agent: agentruntime.AgentClaude}); err == nil {
		t.Fatal("expected missing workdir error")
	}
	if _, err := adapter.PrepareLaunch(context.Background(), agentruntime.StartRequest{ID: "x", Agent: agentruntime.AgentCodex, Workdir: "/tmp"}); err == nil {
		t.Fatal("expected unsupported agent error")
	}
	if _, err := adapter.PrepareLaunch(context.Background(), agentruntime.StartRequest{ID: "x", Agent: agentruntime.AgentClaude, Workdir: "/tmp", Args: []string{"--session-id", "override"}}); err == nil {
		t.Fatal("expected managed argument error")
	}
}

func hasArgPair(args []string, key, value string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == key && args[i+1] == value {
			return true
		}
	}
	return false
}

func readMCPConfig(t *testing.T, path string) mcpConfig {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var config mcpConfig
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	return config
}
