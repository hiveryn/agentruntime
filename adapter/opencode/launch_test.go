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
	req.OpenCodeProfile = "cortex"
	req.Resume = true
	req.Prompt = "continue the work"
	req.Args = []string{"--no-auto-share"}

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
	if agentIdx > continueIdx {
		t.Errorf("--agent (%d) should come before --continue (%d)", agentIdx, continueIdx)
	}
	if continueIdx > promptIdx {
		t.Errorf("--continue (%d) should come before --prompt (%d)", continueIdx, promptIdx)
	}
	if promptIdx > callerIdx {
		t.Errorf("--prompt (%d) should come before caller args (%d)", promptIdx, callerIdx)
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

func TestPrepareLaunch_WithOpenCodeProfile(t *testing.T) {
	a := New(DefaultOptions())
	req := baseReq()
	req.OpenCodeProfile = "cortex"
	spec, err := a.PrepareLaunch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !hasArgPair(spec.Args, "--agent", "cortex") {
		t.Errorf("args missing --agent cortex pair: %v", spec.Args)
	}
}

func TestPrepareLaunch_NoOpenCodeProfile(t *testing.T) {
	a := New(DefaultOptions())
	spec, err := a.PrepareLaunch(context.Background(), baseReq())
	if err != nil {
		t.Fatal(err)
	}
	for _, arg := range spec.Args {
		if arg == "--agent" {
			t.Errorf("--agent should not appear when OpenCodeProfile is empty: %v", spec.Args)
		}
	}
}

func TestPrepareLaunch_RawAgentArgRejected(t *testing.T) {
	a := New(DefaultOptions())
	req := baseReq()
	req.Args = []string{"--agent", "cortex"}
	_, err := a.PrepareLaunch(context.Background(), req)
	if err == nil {
		t.Error("expected error for managed arg --agent in req.Args")
	}
}

func TestPrepareLaunch_ArgOrdering(t *testing.T) {
	a := New(DefaultOptions())
	req := baseReq()
	req.OpenCodeProfile = "cortex"
	req.Prompt = "do the thing"
	req.Args = []string{"--no-auto-share"}
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
	if agentIdx > promptIdx {
		t.Errorf("--agent (%d) should come before --prompt (%d)", agentIdx, promptIdx)
	}
	if promptIdx > callerIdx {
		t.Errorf("--prompt (%d) should come before caller args (%d)", promptIdx, callerIdx)
	}
}
