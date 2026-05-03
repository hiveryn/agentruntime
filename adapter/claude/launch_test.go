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

func TestPrepareLaunchResumeBare(t *testing.T) {
	called := false
	adapter := New(Options{
		NewSessionID: func() (string, error) {
			called = true
			return "should-not-be-used", nil
		},
	})
	req := agentruntime.StartRequest{
		ID:      "hiv-claude-resume-1",
		Agent:   agentruntime.AgentClaude,
		Workdir: "/tmp/work",
		Resume:  true,
	}

	spec, err := adapter.PrepareLaunch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("NewSessionID must not be called in resume mode")
	}
	foundResume := false
	for _, arg := range spec.Args {
		if arg == "--resume" {
			foundResume = true
		}
		if arg == "--session-id" {
			t.Fatalf("--session-id must not appear in resume mode: %q", spec.Args)
		}
	}
	if !foundResume {
		t.Fatalf("--resume missing from args: %q", spec.Args)
	}
	if spec.Env["AGENTRUNTIME_SESSION_ID"] != req.ID {
		t.Fatalf("AGENTRUNTIME_SESSION_ID: %q", spec.Env["AGENTRUNTIME_SESSION_ID"])
	}
}

func TestPrepareLaunchResumeSpecific(t *testing.T) {
	adapter := New(Options{
		NewSessionID: func() (string, error) {
			t.Fatal("NewSessionID must not be called in resume mode")
			return "", nil
		},
	})
	req := agentruntime.StartRequest{
		ID:       "hiv-claude-resume-2",
		Agent:    agentruntime.AgentClaude,
		Workdir:  "/tmp/work",
		Resume:   true,
		ResumeID: "00000000-0000-4000-8000-000000000099",
	}

	spec, err := adapter.PrepareLaunch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !hasArgPair(spec.Args, "--resume", "00000000-0000-4000-8000-000000000099") {
		t.Fatalf("args missing --resume <id> pair: %q", spec.Args)
	}
	for _, arg := range spec.Args {
		if arg == "--session-id" {
			t.Fatalf("--session-id must not appear in resume mode: %q", spec.Args)
		}
	}
	if spec.Env["AGENTRUNTIME_SESSION_ID"] != req.ID {
		t.Fatalf("AGENTRUNTIME_SESSION_ID: %q", spec.Env["AGENTRUNTIME_SESSION_ID"])
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
	if _, err := adapter.PrepareLaunch(context.Background(), agentruntime.StartRequest{ID: "x", Agent: agentruntime.AgentClaude, Workdir: "/tmp", Args: []string{"--resume", "some-id"}}); err == nil {
		t.Fatal("expected managed argument error for --resume")
	}
}

func TestPrepareLaunchReservedEnvConflict(t *testing.T) {
	adapter := New(DefaultOptions())
	req := agentruntime.StartRequest{
		ID:      "session-1",
		Agent:   agentruntime.AgentClaude,
		Workdir: "/tmp/work",
		Env:     map[string]string{"AGENTRUNTIME_SESSION_ID": "someone-elses-id"},
	}
	_, err := adapter.PrepareLaunch(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for reserved env key conflict")
	}
}

func TestPrepareLaunchReservedEnvSameValueAllowed(t *testing.T) {
	adapter := New(DefaultOptions())
	req := agentruntime.StartRequest{
		ID:      "session-1",
		Agent:   agentruntime.AgentClaude,
		Workdir: "/tmp/work",
		Env:     map[string]string{"AGENTRUNTIME_SESSION_ID": "session-1"},
	}
	_, err := adapter.PrepareLaunch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
}

func TestPrepareLaunchReservedEnvEmptyValueAllowed(t *testing.T) {
	adapter := New(DefaultOptions())
	req := agentruntime.StartRequest{
		ID:      "session-1",
		Agent:   agentruntime.AgentClaude,
		Workdir: "/tmp/work",
		Env:     map[string]string{"AGENTRUNTIME_SESSION_ID": ""},
	}
	_, err := adapter.PrepareLaunch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
}

func TestPrepareLaunch_MultipleMCPServers(t *testing.T) {
	adapter := New(DefaultOptions())
	req := agentruntime.StartRequest{
		ID:      "session-1",
		Agent:   agentruntime.AgentClaude,
		Workdir: "/tmp/work",
		MCPServers: []agentruntime.MCPServerConfig{
			{
				Name:    "stdio-srv",
				Command: "cmd-a",
				Args:    []string{"--flag"},
				CWD:     "/opt",
				Env:     map[string]string{"EXTRA": "val"},
			},
			{
				Name:              "http-srv",
				URL:               "https://api.example.com/mcp",
				BearerTokenEnvVar: "TOKEN",
			},
		},
	}

	spec, err := adapter.PrepareLaunch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.CleanupPaths) != 1 {
		t.Fatalf("expected 1 cleanup path, got %d: %#v", len(spec.CleanupPaths), spec.CleanupPaths)
	}

	config := readMCPConfig(t, spec.CleanupPaths[0])
	if len(config.MCPServers) != 2 {
		t.Fatalf("expected 2 mcp servers, got %d: %#v", len(config.MCPServers), config.MCPServers)
	}

	stdio := config.MCPServers["stdio-srv"]
	if stdio.Type != "stdio" || stdio.Command != "cmd-a" {
		t.Fatalf("stdio server: %#v", stdio)
	}
	if len(stdio.Args) != 1 || stdio.Args[0] != "--flag" {
		t.Fatalf("stdio server args: %#v", stdio.Args)
	}
	if stdio.Env["PWD"] != "/opt" {
		t.Fatalf("stdio server PWD: %q", stdio.Env["PWD"])
	}
	if stdio.Env["EXTRA"] != "val" {
		t.Fatalf("stdio server EXTRA: %q", stdio.Env["EXTRA"])
	}

	httpSrv := config.MCPServers["http-srv"]
	if httpSrv.Type != "http" || httpSrv.URL != "https://api.example.com/mcp" {
		t.Fatalf("http server: %#v", httpSrv)
	}
	if httpSrv.Headers["Authorization"] != "Bearer ${TOKEN}" {
		t.Fatalf("http server headers: %#v", httpSrv.Headers)
	}

	for _, path := range spec.CleanupPaths {
		_ = os.Remove(path)
	}
}

func TestPrepareLaunch_MCPStdioEnvPreservation(t *testing.T) {
	adapter := New(DefaultOptions())
	req := agentruntime.StartRequest{
		ID:      "session-1",
		Agent:   agentruntime.AgentClaude,
		Workdir: "/tmp/work",
		MCPServers: []agentruntime.MCPServerConfig{{
			Name:    "my-server",
			Command: "proxy",
			Args:    []string{"--listen", "0.0.0.0:8080"},
			Env:     map[string]string{"MY_ENV": "hello", "OTHER": "world"},
		}},
	}

	spec, err := adapter.PrepareLaunch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	config := readMCPConfig(t, spec.CleanupPaths[0])
	server := config.MCPServers["my-server"]
	if server.Type != "stdio" || server.Command != "proxy" {
		t.Fatalf("server: %#v", server)
	}
	if server.Env["MY_ENV"] != "hello" {
		t.Fatalf("env MY_ENV: %q", server.Env["MY_ENV"])
	}
	if server.Env["OTHER"] != "world" {
		t.Fatalf("env OTHER: %q", server.Env["OTHER"])
	}
	if _, ok := server.Env["PWD"]; ok {
		t.Fatalf("PWD should not be present when CWD is not set: %#v", server.Env)
	}

	for _, path := range spec.CleanupPaths {
		_ = os.Remove(path)
	}
}

func TestPrepareLaunch_MCPStdioCWDOnly(t *testing.T) {
	adapter := New(DefaultOptions())
	req := agentruntime.StartRequest{
		ID:      "session-1",
		Agent:   agentruntime.AgentClaude,
		Workdir: "/tmp/work",
		MCPServers: []agentruntime.MCPServerConfig{{
			Name:    "my-server",
			Command: "proxy",
			CWD:     "/opt/app",
		}},
	}

	spec, err := adapter.PrepareLaunch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	config := readMCPConfig(t, spec.CleanupPaths[0])
	server := config.MCPServers["my-server"]
	if server.Env["PWD"] != "/opt/app" {
		t.Fatalf("PWD: got %q", server.Env["PWD"])
	}
	if len(server.Env) != 1 {
		t.Fatalf("expected exactly 1 env entry (PWD only), got %d: %#v", len(server.Env), server.Env)
	}

	for _, path := range spec.CleanupPaths {
		_ = os.Remove(path)
	}
}

func TestPrepareLaunch_MCPStdioPWDOverride(t *testing.T) {
	adapter := New(DefaultOptions())
	req := agentruntime.StartRequest{
		ID:      "session-1",
		Agent:   agentruntime.AgentClaude,
		Workdir: "/tmp/work",
		MCPServers: []agentruntime.MCPServerConfig{{
			Name:    "my-server",
			Command: "proxy",
			CWD:     "/from-cwd",
			Env:     map[string]string{"PWD": "/from-env"},
		}},
	}

	spec, err := adapter.PrepareLaunch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	config := readMCPConfig(t, spec.CleanupPaths[0])
	server := config.MCPServers["my-server"]
	if server.Env["PWD"] != "/from-cwd" {
		t.Fatalf("PWD should be overridden by CWD: got %q", server.Env["PWD"])
	}

	for _, path := range spec.CleanupPaths {
		_ = os.Remove(path)
	}
}

func TestPrepareLaunch_MCPHTTPWithoutToken(t *testing.T) {
	adapter := New(DefaultOptions())
	req := agentruntime.StartRequest{
		ID:      "session-1",
		Agent:   agentruntime.AgentClaude,
		Workdir: "/tmp/work",
		MCPServers: []agentruntime.MCPServerConfig{{
			Name: "public-server",
			URL:  "https://public-mcp.example.com/sse",
		}},
	}

	spec, err := adapter.PrepareLaunch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	config := readMCPConfig(t, spec.CleanupPaths[0])
	server := config.MCPServers["public-server"]
	if server.Type != "http" || server.URL != "https://public-mcp.example.com/sse" {
		t.Fatalf("server: %#v", server)
	}
	if len(server.Headers) != 0 {
		t.Fatalf("expected no headers without bearer token, got: %#v", server.Headers)
	}

	for _, path := range spec.CleanupPaths {
		_ = os.Remove(path)
	}
}

func TestPrepareLaunch_MCPTempFilePermissions(t *testing.T) {
	adapter := New(DefaultOptions())
	req := agentruntime.StartRequest{
		ID:      "session-1",
		Agent:   agentruntime.AgentClaude,
		Workdir: "/tmp/work",
		MCPServers: []agentruntime.MCPServerConfig{{
			Name:    "my-server",
			Command: "proxy",
		}},
	}

	spec, err := adapter.PrepareLaunch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.CleanupPaths) != 1 {
		t.Fatalf("expected 1 cleanup path, got %d", len(spec.CleanupPaths))
	}

	info, err := os.Stat(spec.CleanupPaths[0])
	if err != nil {
		t.Fatalf("stat mcp config: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected 0600 permissions on mcp config file, got %04o", info.Mode().Perm())
	}

	for _, path := range spec.CleanupPaths {
		_ = os.Remove(path)
	}
}

func TestPrepareLaunch_MCPNameValidation(t *testing.T) {
	adapter := New(DefaultOptions())
	req := agentruntime.StartRequest{
		ID:      "session-1",
		Agent:   agentruntime.AgentClaude,
		Workdir: "/tmp/work",
		MCPServers: []agentruntime.MCPServerConfig{{
			Name:    "",
			Command: "proxy",
		}},
	}

	_, err := adapter.PrepareLaunch(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for empty mcp server name")
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
