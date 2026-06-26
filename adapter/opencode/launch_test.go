package opencode

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/hiveryn/agentruntime"
)

func baseReq() agentruntime.StartRequest {
	return agentruntime.StartRequest{
		ID:      "session-1",
		Workdir: "/tmp/work",
	}
}

func hasArgPair(args []string, flag, value string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag && args[i+1] == value {
			return true
		}
	}
	return false
}

func TestPrepareLaunch_Model(t *testing.T) {
	a := New(DefaultOptions())
	req := baseReq()
	req.Model = "deepseek/deepseek-v4-flash"
	spec, err := a.PrepareLaunch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !hasArgPair(spec.Args, "--model", "deepseek/deepseek-v4-flash") {
		t.Fatalf("args missing --model pair: %q", spec.Args)
	}
}

func TestPrepareLaunch_NoModel(t *testing.T) {
	a := New(DefaultOptions())
	spec, err := a.PrepareLaunch(context.Background(), baseReq())
	if err != nil {
		t.Fatal(err)
	}
	for _, arg := range spec.Args {
		if arg == "--model" {
			t.Fatalf("--model must not appear when Model is empty: %q", spec.Args)
		}
	}
}

func TestPrepareLaunch_YoloAndMode(t *testing.T) {
	a := New(DefaultOptions())

	// mode plan -> --agent plan
	req := baseReq()
	req.Mode = agentruntime.ModePlan
	spec, err := a.PrepareLaunch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !hasArgPair(spec.Args, "--agent", "plan") {
		t.Fatalf("plan: missing --agent plan: %q", spec.Args)
	}

	// yolo -> config permission: allow, no CLI flag
	req = baseReq()
	req.Yolo = true
	spec, err = a.PrepareLaunch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	var cfg ocConfig
	if err := json.Unmarshal([]byte(spec.Env["OPENCODE_CONFIG_CONTENT"]), &cfg); err != nil {
		t.Fatalf("parse config: %v", err)
	}
	if cfg.Permission != "allow" {
		t.Fatalf("yolo: expected permission allow, got %v", cfg.Permission)
	}

	// build mode default -> no --agent
	req = baseReq()
	spec, err = a.PrepareLaunch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	for _, arg := range spec.Args {
		if arg == "--agent" {
			t.Fatalf("default: unexpected --agent: %q", spec.Args)
		}
	}

	// conditional rejects
	rejects := []agentruntime.StartRequest{
		func() agentruntime.StartRequest { r := baseReq(); r.Mode = agentruntime.ModePlan; r.Args = []string{"--agent", "build"}; return r }(),
		func() agentruntime.StartRequest { r := baseReq(); r.Model = "x/y"; r.Args = []string{"--model", "a/b"}; return r }(),
		func() agentruntime.StartRequest { r := baseReq(); r.Mode = agentruntime.Mode("nope"); return r }(),
	}
	for i, r := range rejects {
		if _, err := a.PrepareLaunch(context.Background(), r); err == nil {
			t.Fatalf("reject case %d: expected error", i)
		}
	}

	// escape hatch: --agent allowed when mode unset
	req = baseReq()
	req.Args = []string{"--agent", "custom"}
	if _, err := a.PrepareLaunch(context.Background(), req); err != nil {
		t.Fatalf("escape hatch: --agent should be allowed when mode unset: %v", err)
	}
}

func TestPrepareLaunch_Basic(t *testing.T) {
	a := New(DefaultOptions())
	spec, err := a.PrepareLaunch(context.Background(), baseReq())
	if err != nil {
		t.Fatal(err)
	}
	if spec.Command != "opencode" {
		t.Errorf("command: got %q want %q", spec.Command, "opencode")
	}
	if spec.Workdir != "/tmp/work" {
		t.Errorf("workdir: got %q", spec.Workdir)
	}
	if spec.Env["AGENTRUNTIME_SESSION_ID"] != "session-1" {
		t.Errorf("AGENTRUNTIME_SESSION_ID: got %q", spec.Env["AGENTRUNTIME_SESSION_ID"])
	}
	if spec.Env["OPENCODE_CONFIG_CONTENT"] == "" {
		t.Error("OPENCODE_CONFIG_CONTENT not set")
	}
}

func TestPrepareLaunch_WithPrompt(t *testing.T) {
	a := New(DefaultOptions())
	req := baseReq()
	req.Prompt = "Hello, world"
	spec, err := a.PrepareLaunch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !hasArgPair(spec.Args, "--prompt", "Hello, world") {
		t.Errorf("args missing --prompt pair: %v", spec.Args)
	}
}

func TestPrepareLaunch_NoPrompt(t *testing.T) {
	a := New(DefaultOptions())
	req := baseReq()
	spec, err := a.PrepareLaunch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	for _, arg := range spec.Args {
		if arg == "--prompt" {
			t.Error("--prompt should not appear when Prompt is empty")
		}
	}
}

func TestPrepareLaunch_WithInstructions(t *testing.T) {
	a := New(DefaultOptions())
	req := baseReq()
	req.Instructions = "Be concise."
	spec, err := a.PrepareLaunch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		for _, p := range spec.CleanupPaths {
			_ = os.Remove(p)
		}
	}()

	if len(spec.CleanupPaths) == 0 {
		t.Fatal("expected CleanupPaths with instructions file")
	}

	instrPath := spec.CleanupPaths[0]
	data, err := os.ReadFile(instrPath)
	if err != nil {
		t.Fatalf("read instructions file: %v", err)
	}
	if string(data) != "Be concise." {
		t.Errorf("instructions content: got %q", string(data))
	}

	var cfg ocConfig
	if err := json.Unmarshal([]byte(spec.Env["OPENCODE_CONFIG_CONTENT"]), &cfg); err != nil {
		t.Fatalf("parse OPENCODE_CONFIG_CONTENT: %v", err)
	}
	if len(cfg.Instructions) == 0 {
		t.Error("OPENCODE_CONFIG_CONTENT missing instructions")
	}
	if cfg.Instructions[0] != instrPath {
		t.Errorf("instructions path: got %q want %q", cfg.Instructions[0], instrPath)
	}
}

func TestPrepareLaunch_WithMCPStdio(t *testing.T) {
	a := New(DefaultOptions())
	req := baseReq()
	req.MCPServers = []agentruntime.MCPServerConfig{{
		Name:    "my-tools",
		Command: "my-mcp-server",
		Args:    []string{"--port", "8080"},
		CWD:     "/srv",
	}}
	spec, err := a.PrepareLaunch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	var cfg ocConfig
	if err := json.Unmarshal([]byte(spec.Env["OPENCODE_CONFIG_CONTENT"]), &cfg); err != nil {
		t.Fatalf("parse OPENCODE_CONFIG_CONTENT: %v", err)
	}
	srv, ok := cfg.MCP["my-tools"]
	if !ok {
		t.Fatal("mcp entry my-tools not found")
	}
	if srv.Type != "local" {
		t.Errorf("type: got %q want %q", srv.Type, "local")
	}
	if !srv.Enabled {
		t.Error("expected Enabled=true")
	}
	if len(srv.Command) < 1 || srv.Command[0] != "my-mcp-server" {
		t.Errorf("command: got %v", srv.Command)
	}
	if srv.Environment["PWD"] != "/srv" {
		t.Errorf("PWD: got %q", srv.Environment["PWD"])
	}
}

func TestPrepareLaunch_WithMCPHTTP(t *testing.T) {
	a := New(DefaultOptions())
	req := baseReq()
	req.MCPServers = []agentruntime.MCPServerConfig{{
		Name:              "remote",
		URL:               "http://my-mcp.example.com",
		BearerTokenEnvVar: "MCP_TOKEN",
	}}
	spec, err := a.PrepareLaunch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	var cfg ocConfig
	if err := json.Unmarshal([]byte(spec.Env["OPENCODE_CONFIG_CONTENT"]), &cfg); err != nil {
		t.Fatalf("parse OPENCODE_CONFIG_CONTENT: %v", err)
	}
	srv, ok := cfg.MCP["remote"]
	if !ok {
		t.Fatal("mcp entry remote not found")
	}
	if srv.Type != "remote" {
		t.Errorf("type: got %q want %q", srv.Type, "remote")
	}
	if srv.URL != "http://my-mcp.example.com" {
		t.Errorf("URL: got %q", srv.URL)
	}
	if !strings.Contains(srv.Headers["Authorization"], "MCP_TOKEN") {
		t.Errorf("Authorization header: got %q", srv.Headers["Authorization"])
	}
}

func TestPrepareLaunch_ManagedArgRejected(t *testing.T) {
	a := New(DefaultOptions())
	req := baseReq()
	req.Args = []string{"--prompt", "sneaky"}
	_, err := a.PrepareLaunch(context.Background(), req)
	if err == nil {
		t.Error("expected error for managed arg --prompt in req.Args")
	}
}

func TestPrepareLaunch_MissingID(t *testing.T) {
	a := New(DefaultOptions())
	req := baseReq()
	req.ID = ""
	_, err := a.PrepareLaunch(context.Background(), req)
	if err == nil {
		t.Error("expected error for missing ID")
	}
}

func TestPrepareLaunch_MissingWorkdir(t *testing.T) {
	a := New(DefaultOptions())
	req := baseReq()
	req.Workdir = ""
	_, err := a.PrepareLaunch(context.Background(), req)
	if err == nil {
		t.Error("expected error for missing Workdir")
	}
}

func TestPrepareLaunch_CustomCommand(t *testing.T) {
	a := New(DefaultOptions())
	req := baseReq()
	req.Command = "opencode-dev"
	spec, err := a.PrepareLaunch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Command != "opencode-dev" {
		t.Errorf("command: got %q want %q", spec.Command, "opencode-dev")
	}
}

func TestPrepareLaunch_ReservedEnvConflict_AGENTRUNTIME_SESSION_ID(t *testing.T) {
	a := New(DefaultOptions())
	req := baseReq()
	req.Env = map[string]string{"AGENTRUNTIME_SESSION_ID": "someone-elses-id"}
	_, err := a.PrepareLaunch(context.Background(), req)
	if err == nil {
		t.Error("expected error for reserved env key conflict")
	}
}

func TestPrepareLaunch_ReservedEnvConflict_AGENTRUNTIME_SESSION_ID_SameValue(t *testing.T) {
	a := New(DefaultOptions())
	req := baseReq()
	req.Env = map[string]string{"AGENTRUNTIME_SESSION_ID": "session-1"}
	_, err := a.PrepareLaunch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
}

func TestPrepareLaunch_ReservedEnvConflict_AGENTRUNTIME_SESSION_ID_EmptyValue(t *testing.T) {
	a := New(DefaultOptions())
	req := baseReq()
	req.Env = map[string]string{"AGENTRUNTIME_SESSION_ID": ""}
	_, err := a.PrepareLaunch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
}

func TestPrepareLaunch_ReservedEnvConflict_OPENCODE_CONFIG_CONTENT(t *testing.T) {
	a := New(DefaultOptions())
	req := baseReq()
	req.Env = map[string]string{"OPENCODE_CONFIG_CONTENT": "fake-config"}
	_, err := a.PrepareLaunch(context.Background(), req)
	if err == nil {
		t.Error("expected error for reserved env key OPENCODE_CONFIG_CONTENT")
	}
}

func TestPrepareLaunch_ReservedEnvConflict_OPENCODE_CONFIG_CONTENT_EmptyValue(t *testing.T) {
	a := New(DefaultOptions())
	req := baseReq()
	req.Env = map[string]string{"OPENCODE_CONFIG_CONTENT": ""}
	_, err := a.PrepareLaunch(context.Background(), req)
	if err != nil {
		t.Fatal("empty OPENCODE_CONFIG_CONTENT should be allowed, got:", err)
	}
}

func TestPrepareLaunch_ResumeBare(t *testing.T) {
	a := New(DefaultOptions())
	req := baseReq()
	req.Resume = true

	spec, err := a.PrepareLaunch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, arg := range spec.Args {
		if arg == "--continue" {
			found = true
		}
		if arg == "--session" {
			t.Errorf("--session must not appear for bare resume: %v", spec.Args)
		}
	}
	if !found {
		t.Errorf("--continue missing from args: %v", spec.Args)
	}
	if spec.Env["AGENTRUNTIME_SESSION_ID"] != "session-1" {
		t.Errorf("AGENTRUNTIME_SESSION_ID: %q", spec.Env["AGENTRUNTIME_SESSION_ID"])
	}
}

func TestPrepareLaunch_ResumeSpecific(t *testing.T) {
	a := New(DefaultOptions())
	req := baseReq()
	req.Resume = true
	req.ResumeID = "ses_21277c40fffeuBv0E2V7Y81mkA"

	spec, err := a.PrepareLaunch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !hasArgPair(spec.Args, "--session", "ses_21277c40fffeuBv0E2V7Y81mkA") {
		t.Errorf("args missing --session <id> pair: %v", spec.Args)
	}
	for _, arg := range spec.Args {
		if arg == "--continue" {
			t.Errorf("--continue must not appear for specific resume: %v", spec.Args)
		}
	}
}

func TestPrepareLaunch_ResumeManagedArgRejected(t *testing.T) {
	a := New(DefaultOptions())
	req := baseReq()
	req.Args = []string{"--continue"}
	if _, err := a.PrepareLaunch(context.Background(), req); err == nil {
		t.Error("expected error for managed arg --continue in req.Args")
	}
	req.Args = []string{"-c"}
	if _, err := a.PrepareLaunch(context.Background(), req); err == nil {
		t.Error("expected error for managed arg -c in req.Args")
	}
	req.Args = []string{"--session", "some-id"}
	if _, err := a.PrepareLaunch(context.Background(), req); err == nil {
		t.Error("expected error for managed arg --session in req.Args")
	}
	req.Args = []string{"-s", "some-id"}
	if _, err := a.PrepareLaunch(context.Background(), req); err == nil {
		t.Error("expected error for managed arg -s in req.Args")
	}
}

func TestPrepareLaunch_ResumeArgOrdering(t *testing.T) {
	// Use bare resume (no ResumeID) so --prompt is still emitted.
	a := New(DefaultOptions())
	req := baseReq()
	req.Resume = true
	req.Prompt = "continue the work"
	req.Args = []string{"--agent", "cortex", "--no-auto-share"}

	spec, err := a.PrepareLaunch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	agentIdx, continueIdx, promptIdx, callerIdx := -1, -1, -1, -1
	for i, arg := range spec.Args {
		switch arg {
		case "--agent":
			agentIdx = i
		case "--continue":
			continueIdx = i
		case "--prompt":
			promptIdx = i
		case "--no-auto-share":
			callerIdx = i
		}
	}
	if agentIdx == -1 {
		t.Fatalf("--agent not found: %v", spec.Args)
	}
	if continueIdx == -1 {
		t.Fatalf("--continue not found: %v", spec.Args)
	}
	if promptIdx == -1 {
		t.Fatalf("--prompt not found: %v", spec.Args)
	}
	if callerIdx == -1 {
		t.Fatalf("caller arg not found: %v", spec.Args)
	}
	// Synthesized args (--continue, --prompt) come before caller args (--agent, --no-auto-share).
	if continueIdx > promptIdx {
		t.Errorf("--continue (%d) should come before --prompt (%d)", continueIdx, promptIdx)
	}
	if promptIdx > agentIdx {
		t.Errorf("--prompt (%d) should come before caller args (%d)", promptIdx, agentIdx)
	}
	if agentIdx > callerIdx {
		t.Errorf("--agent (%d) should come before --no-auto-share (%d)", agentIdx, callerIdx)
	}
}

func TestPrepareLaunch_ResumeSpecificSuppressesPrompt(t *testing.T) {
	a := New(DefaultOptions())
	req := baseReq()
	req.Resume = true
	req.ResumeID = "ses_2125a66deffebPnI7tduCLf0NG"
	req.Prompt = "What word did you say?"

	spec, err := a.PrepareLaunch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	for _, arg := range spec.Args {
		if arg == "--prompt" {
			t.Fatalf("--prompt must not appear when resuming a specific session by ID: %v", spec.Args)
		}
	}
	if !hasArgPair(spec.Args, "--session", "ses_2125a66deffebPnI7tduCLf0NG") {
		t.Fatalf("--session <id> missing: %v", spec.Args)
	}
}

func TestPrepareLaunch_WrongAgent(t *testing.T) {
	a := New(DefaultOptions())
	req := baseReq()
	req.Agent = agentruntime.AgentClaude
	_, err := a.PrepareLaunch(context.Background(), req)
	if err == nil {
		t.Error("expected error for wrong agent kind")
	}
}

func TestPrepareLaunch_AgentArgPassesThrough(t *testing.T) {
	a := New(DefaultOptions())
	req := baseReq()
	req.Args = []string{"--agent", "cortex"}
	spec, err := a.PrepareLaunch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !hasArgPair(spec.Args, "--agent", "cortex") {
		t.Errorf("args missing --agent cortex pair: %v", spec.Args)
	}
}

func TestPrepareLaunch_NoAgentArgByDefault(t *testing.T) {
	a := New(DefaultOptions())
	spec, err := a.PrepareLaunch(context.Background(), baseReq())
	if err != nil {
		t.Fatal(err)
	}
	for _, arg := range spec.Args {
		if arg == "--agent" {
			t.Errorf("--agent should not appear when not in req.Args: %v", spec.Args)
		}
	}
}

func TestPrepareLaunch_MultipleMCPServers(t *testing.T) {
	a := New(DefaultOptions())
	req := baseReq()
	req.MCPServers = []agentruntime.MCPServerConfig{
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
	}
	spec, err := a.PrepareLaunch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	var cfg ocConfig
	if err := json.Unmarshal([]byte(spec.Env["OPENCODE_CONFIG_CONTENT"]), &cfg); err != nil {
		t.Fatalf("parse OPENCODE_CONFIG_CONTENT: %v", err)
	}
	if len(cfg.MCP) != 2 {
		t.Fatalf("expected 2 mcp servers, got %d: %#v", len(cfg.MCP), cfg.MCP)
	}

	stdio := cfg.MCP["stdio-srv"]
	if stdio.Type != "local" {
		t.Fatalf("stdio server type: %q", stdio.Type)
	}
	if !stdio.Enabled {
		t.Error("stdio server expected Enabled=true")
	}
	if len(stdio.Command) == 0 || stdio.Command[0] != "cmd-a" {
		t.Errorf("stdio server command: %v", stdio.Command)
	}
	if stdio.Environment["PWD"] != "/opt" {
		t.Errorf("stdio server PWD: %q", stdio.Environment["PWD"])
	}
	if stdio.Environment["EXTRA"] != "val" {
		t.Errorf("stdio server EXTRA: %q", stdio.Environment["EXTRA"])
	}

	httpSrv := cfg.MCP["http-srv"]
	if httpSrv.Type != "remote" {
		t.Fatalf("http server type: %q", httpSrv.Type)
	}
	if httpSrv.URL != "https://api.example.com/mcp" {
		t.Errorf("http server URL: %q", httpSrv.URL)
	}
	if !strings.Contains(httpSrv.Headers["Authorization"], "TOKEN") {
		t.Errorf("http server Authorization header: %q", httpSrv.Headers["Authorization"])
	}
}

func TestPrepareLaunch_MCPStdioEnvPreservation(t *testing.T) {
	a := New(DefaultOptions())
	req := baseReq()
	req.MCPServers = []agentruntime.MCPServerConfig{{
		Name:    "my-tools",
		Command: "proxy",
		Args:    []string{"--listen", "0.0.0.0:8080"},
		Env:     map[string]string{"MY_ENV": "hello", "OTHER": "world"},
	}}
	spec, err := a.PrepareLaunch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	var cfg ocConfig
	if err := json.Unmarshal([]byte(spec.Env["OPENCODE_CONFIG_CONTENT"]), &cfg); err != nil {
		t.Fatalf("parse OPENCODE_CONFIG_CONTENT: %v", err)
	}
	srv := cfg.MCP["my-tools"]
	if srv.Environment["MY_ENV"] != "hello" {
		t.Errorf("env MY_ENV: %q", srv.Environment["MY_ENV"])
	}
	if srv.Environment["OTHER"] != "world" {
		t.Errorf("env OTHER: %q", srv.Environment["OTHER"])
	}
	if _, ok := srv.Environment["PWD"]; ok {
		t.Error("PWD should not be present when CWD is not set")
	}
}

func TestPrepareLaunch_MCPStdioCWDAndEnvPreservation(t *testing.T) {
	a := New(DefaultOptions())
	req := baseReq()
	req.MCPServers = []agentruntime.MCPServerConfig{{
		Name:    "my-tools",
		Command: "proxy",
		CWD:     "/srv",
		Env:     map[string]string{"MY_ENV": "hello"},
	}}
	spec, err := a.PrepareLaunch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	var cfg ocConfig
	if err := json.Unmarshal([]byte(spec.Env["OPENCODE_CONFIG_CONTENT"]), &cfg); err != nil {
		t.Fatalf("parse OPENCODE_CONFIG_CONTENT: %v", err)
	}
	srv := cfg.MCP["my-tools"]
	if srv.Environment["PWD"] != "/srv" {
		t.Errorf("PWD: %q", srv.Environment["PWD"])
	}
	if srv.Environment["MY_ENV"] != "hello" {
		t.Errorf("MY_ENV: %q", srv.Environment["MY_ENV"])
	}
	if len(srv.Environment) != 2 {
		t.Errorf("expected 2 env entries (PWD + MY_ENV), got %d: %#v", len(srv.Environment), srv.Environment)
	}
}

func TestPrepareLaunch_MCPStdioPWDOverride(t *testing.T) {
	a := New(DefaultOptions())
	req := baseReq()
	req.MCPServers = []agentruntime.MCPServerConfig{{
		Name:    "my-tools",
		Command: "proxy",
		CWD:     "/from-cwd",
		Env:     map[string]string{"PWD": "/from-env"},
	}}
	spec, err := a.PrepareLaunch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	var cfg ocConfig
	if err := json.Unmarshal([]byte(spec.Env["OPENCODE_CONFIG_CONTENT"]), &cfg); err != nil {
		t.Fatalf("parse OPENCODE_CONFIG_CONTENT: %v", err)
	}
	srv := cfg.MCP["my-tools"]
	if srv.Environment["PWD"] != "/from-cwd" {
		t.Fatalf("PWD should be overridden by CWD: got %q", srv.Environment["PWD"])
	}
}

func TestPrepareLaunch_MCPHTTPWithoutToken(t *testing.T) {
	a := New(DefaultOptions())
	req := baseReq()
	req.MCPServers = []agentruntime.MCPServerConfig{{
		Name: "public-server",
		URL:  "https://public-mcp.example.com/sse",
	}}
	spec, err := a.PrepareLaunch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	var cfg ocConfig
	if err := json.Unmarshal([]byte(spec.Env["OPENCODE_CONFIG_CONTENT"]), &cfg); err != nil {
		t.Fatalf("parse OPENCODE_CONFIG_CONTENT: %v", err)
	}
	srv := cfg.MCP["public-server"]
	if srv.Type != "remote" {
		t.Errorf("type: got %q want %q", srv.Type, "remote")
	}
	if srv.URL != "https://public-mcp.example.com/sse" {
		t.Errorf("URL: got %q", srv.URL)
	}
	if !srv.Enabled {
		t.Error("expected Enabled=true")
	}
	if len(srv.Headers) != 0 {
		t.Errorf("expected no headers without bearer token: %#v", srv.Headers)
	}
}

func TestPrepareLaunch_MCPNameValidation(t *testing.T) {
	a := New(DefaultOptions())
	req := baseReq()
	req.MCPServers = []agentruntime.MCPServerConfig{{
		Name:    "",
		Command: "proxy",
	}}

	_, err := a.PrepareLaunch(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for empty mcp server name")
	}
}

func TestPrepareLaunch_WithAgentConfig(t *testing.T) {
	a := New(DefaultOptions())
	req := baseReq()
	req.OpenCodeAgentConfig = map[string]agentruntime.OpenCodeAgentConfig{
		"cortex": {
			Description: "Cortex agent",
			Mode:        "auto",
			Prompt:      "You are Cortex.",
			Permission:  map[string]string{"bash": "allow"},
		},
	}
	spec, err := a.PrepareLaunch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	var cfg ocConfig
	if err := json.Unmarshal([]byte(spec.Env["OPENCODE_CONFIG_CONTENT"]), &cfg); err != nil {
		t.Fatalf("parse OPENCODE_CONFIG_CONTENT: %v", err)
	}
	entry, ok := cfg.Agent["cortex"]
	if !ok {
		t.Fatal("agent entry cortex not found")
	}
	if entry.Description != "Cortex agent" {
		t.Errorf("description: got %q", entry.Description)
	}
	if entry.Mode != "auto" {
		t.Errorf("mode: got %q", entry.Mode)
	}
	if entry.Prompt != "You are Cortex." {
		t.Errorf("prompt: got %q", entry.Prompt)
	}
	if entry.Permission["bash"] != "allow" {
		t.Errorf("permission bash: got %q", entry.Permission["bash"])
	}
}

func TestPrepareLaunch_NilAgentConfig(t *testing.T) {
	a := New(DefaultOptions())
	req := baseReq()
	// OpenCodeAgentConfig not set — agent key must be absent from config JSON.
	spec, err := a.PrepareLaunch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	var cfg ocConfig
	if err := json.Unmarshal([]byte(spec.Env["OPENCODE_CONFIG_CONTENT"]), &cfg); err != nil {
		t.Fatalf("parse OPENCODE_CONFIG_CONTENT: %v", err)
	}
	if cfg.Agent != nil {
		t.Errorf("expected no agent section for nil OpenCodeAgentConfig, got: %#v", cfg.Agent)
	}
}

func TestPrepareLaunch_EmptyAgentConfig(t *testing.T) {
	a := New(DefaultOptions())
	req := baseReq()
	req.OpenCodeAgentConfig = map[string]agentruntime.OpenCodeAgentConfig{}
	spec, err := a.PrepareLaunch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	var cfg ocConfig
	if err := json.Unmarshal([]byte(spec.Env["OPENCODE_CONFIG_CONTENT"]), &cfg); err != nil {
		t.Fatalf("parse OPENCODE_CONFIG_CONTENT: %v", err)
	}
	if cfg.Agent != nil {
		t.Errorf("expected no agent section for empty OpenCodeAgentConfig, got: %#v", cfg.Agent)
	}
}

func TestPrepareLaunch_AgentConfigCoexistsWithMCPInstructionsProfile(t *testing.T) {
	a := New(DefaultOptions())
	req := baseReq()
	req.Args = []string{"--agent", "cortex"}
	req.Instructions = "Be concise."
	req.MCPServers = []agentruntime.MCPServerConfig{{
		Name:    "tools",
		Command: "my-mcp",
	}}
	req.OpenCodeAgentConfig = map[string]agentruntime.OpenCodeAgentConfig{
		"cortex": {
			Description: "Cortex",
			Mode:        "auto",
		},
	}
	spec, err := a.PrepareLaunch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		for _, p := range spec.CleanupPaths {
			_ = os.Remove(p)
		}
	}()

	if !hasArgPair(spec.Args, "--agent", "cortex") {
		t.Errorf("--agent cortex missing: %v", spec.Args)
	}

	var cfg ocConfig
	if err := json.Unmarshal([]byte(spec.Env["OPENCODE_CONFIG_CONTENT"]), &cfg); err != nil {
		t.Fatalf("parse OPENCODE_CONFIG_CONTENT: %v", err)
	}
	if _, ok := cfg.MCP["tools"]; !ok {
		t.Error("mcp entry tools missing")
	}
	if len(cfg.Instructions) == 0 {
		t.Error("instructions missing")
	}
	if _, ok := cfg.Agent["cortex"]; !ok {
		t.Error("agent entry cortex missing")
	}
	if strings.Contains(spec.Env["OPENCODE_CONFIG_CONTENT"], `"permission":null`) {
		t.Fatalf("nil agent permission must be omitted, got %s", spec.Env["OPENCODE_CONFIG_CONTENT"])
	}
}

func TestPrepareLaunch_AgentConfigPromptIndependentFromRequestPrompt(t *testing.T) {
	a := New(DefaultOptions())
	req := baseReq()
	req.Prompt = "kickoff prompt"
	req.OpenCodeAgentConfig = map[string]agentruntime.OpenCodeAgentConfig{
		"cortex": {
			Description: "Cortex",
			Mode:        "auto",
			Prompt:      "agent definition prompt",
		},
	}
	spec, err := a.PrepareLaunch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	// StartRequest.Prompt becomes --prompt arg
	if !hasArgPair(spec.Args, "--prompt", "kickoff prompt") {
		t.Errorf("--prompt kickoff not found: %v", spec.Args)
	}

	// OpenCodeAgentConfig.Prompt goes into config agent section, not args
	var cfg ocConfig
	if err := json.Unmarshal([]byte(spec.Env["OPENCODE_CONFIG_CONTENT"]), &cfg); err != nil {
		t.Fatalf("parse OPENCODE_CONFIG_CONTENT: %v", err)
	}
	entry := cfg.Agent["cortex"]
	if entry.Prompt != "agent definition prompt" {
		t.Errorf("agent definition prompt: got %q", entry.Prompt)
	}
}

func TestPrepareLaunch_AgentConfigNoPrompt(t *testing.T) {
	a := New(DefaultOptions())
	req := baseReq()
	req.OpenCodeAgentConfig = map[string]agentruntime.OpenCodeAgentConfig{
		"minimal": {
			Description: "Minimal",
			Mode:        "auto",
		},
	}
	spec, err := a.PrepareLaunch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	var cfg ocConfig
	if err := json.Unmarshal([]byte(spec.Env["OPENCODE_CONFIG_CONTENT"]), &cfg); err != nil {
		t.Fatalf("parse OPENCODE_CONFIG_CONTENT: %v", err)
	}
	entry := cfg.Agent["minimal"]
	if entry.Prompt != "" {
		t.Errorf("expected empty prompt, got %q", entry.Prompt)
	}
}

func TestPrepareLaunch_ArgOrdering(t *testing.T) {
	a := New(DefaultOptions())
	req := baseReq()
	req.Prompt = "do the thing"
	req.Args = []string{"--agent", "cortex", "--no-auto-share"}
	spec, err := a.PrepareLaunch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	agentIdx := -1
	promptIdx := -1
	callerIdx := -1
	for i, arg := range spec.Args {
		switch arg {
		case "--agent":
			agentIdx = i
		case "--prompt":
			promptIdx = i
		case "--no-auto-share":
			callerIdx = i
		}
	}
	if agentIdx == -1 {
		t.Fatalf("--agent not found in args: %v", spec.Args)
	}
	if promptIdx == -1 {
		t.Fatalf("--prompt not found in args: %v", spec.Args)
	}
	if callerIdx == -1 {
		t.Fatalf("caller arg not found in args: %v", spec.Args)
	}
	// Synthesized --prompt comes before caller args (--agent, --no-auto-share).
	if promptIdx > agentIdx {
		t.Errorf("--prompt (%d) should come before caller args (%d)", promptIdx, agentIdx)
	}
	if agentIdx > callerIdx {
		t.Errorf("--agent (%d) should come before --no-auto-share (%d)", agentIdx, callerIdx)
	}
}
