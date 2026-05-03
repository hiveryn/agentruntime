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

func TestPrepareLaunch_WrongAgent(t *testing.T) {
	a := New(DefaultOptions())
	req := baseReq()
	req.Agent = agentruntime.AgentClaude
	_, err := a.PrepareLaunch(context.Background(), req)
	if err == nil {
		t.Error("expected error for wrong agent kind")
	}
}
