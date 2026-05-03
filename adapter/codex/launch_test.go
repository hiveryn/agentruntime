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
		ID:      "session-1",
		Agent:   agentruntime.AgentCodex,
		Workdir: "/tmp/work",
		Prompt:  "Start architect session",
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
	if spec.Command != "codex" {
		t.Fatalf("command: %q", spec.Command)
	}

	joined := strings.Join(spec.Args, "\x00")
	for _, want := range []string{
		"mcp_servers.my-server.command=\"my-mcp-proxy\"",
		"mcp_servers.my-server.args=[\"--daemon\",\"http://127.0.0.1:4200\"]",
		"--cd\x00/tmp/work",
		"Start architect session",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("args missing %q\nargs=%q", want, spec.Args)
		}
	}

	if spec.Env["AGENTRUNTIME_SESSION_ID"] != "session-1" {
		t.Fatalf("AGENTRUNTIME_SESSION_ID: %q", spec.Env["AGENTRUNTIME_SESSION_ID"])
	}
}

func TestPrepareLaunchHTTPMCP(t *testing.T) {
	adapter := New(DefaultOptions())
	req := agentruntime.StartRequest{
		ID:      "worker-1",
		Agent:   agentruntime.AgentCodex,
		Workdir: "/repo",
		MCPServers: []agentruntime.MCPServerConfig{{
			Name:              "my-server",
			URL:               "http://127.0.0.1:4200/mcp/worker-1",
			BearerTokenEnvVar: "MY_MCP_TOKEN",
		}},
		Env: map[string]string{"MY_MCP_TOKEN": "secret"},
	}

	spec, err := adapter.PrepareLaunch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	joined := strings.Join(spec.Args, "\x00")
	for _, want := range []string{
		"mcp_servers.my-server.url=\"http://127.0.0.1:4200/mcp/worker-1\"",
		"mcp_servers.my-server.bearer_token_env_var=\"MY_MCP_TOKEN\"",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("args missing %q\nargs=%q", want, spec.Args)
		}
	}

	if spec.Env["MY_MCP_TOKEN"] != "secret" {
		t.Fatalf("env: %#v", spec.Env)
	}
}

func TestPrepareLaunchInstructionsUseDeveloperOverride(t *testing.T) {
	adapter := New(DefaultOptions())
	req := agentruntime.StartRequest{
		ID:           "session-2",
		Agent:        agentruntime.AgentCodex,
		Workdir:      "/tmp/work",
		Instructions: "Be terse. Prefer bullets.",
		Prompt:       "Summarize this repository.",
	}

	spec, err := adapter.PrepareLaunch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	if !hasArgPair(spec.Args, "--config", "developer_instructions=\"Be terse. Prefer bullets.\"") {
		t.Fatalf("args missing developer instructions override: %q", spec.Args)
	}
	if got := spec.Args[len(spec.Args)-1]; got != req.Prompt {
		t.Fatalf("prompt arg = %q, want %q", got, req.Prompt)
	}
}

func TestPrepareLaunchIgnoresBlankInstructions(t *testing.T) {
	adapter := New(DefaultOptions())
	req := agentruntime.StartRequest{
		ID:           "session-3",
		Agent:        agentruntime.AgentCodex,
		Workdir:      "/tmp/work",
		Instructions: " \n\t ",
		Prompt:       "Summarize this repository.",
	}

	spec, err := adapter.PrepareLaunch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i+1 < len(spec.Args); i++ {
		if spec.Args[i] == "--config" && strings.HasPrefix(spec.Args[i+1], "developer_instructions=") {
			t.Fatalf("unexpected blank developer instructions override: %q", spec.Args)
		}
	}
}

func TestPrepareLaunchResumeBare(t *testing.T) {
	adapter := New(DefaultOptions())
	req := agentruntime.StartRequest{
		ID:      "session-1",
		Agent:   agentruntime.AgentCodex,
		Workdir: "/tmp/work",
		Resume:  true,
	}

	spec, err := adapter.PrepareLaunch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.Args) < 2 || spec.Args[0] != "resume" || spec.Args[1] != "--last" {
		t.Fatalf("args[0:2]: %q, want [resume --last]", spec.Args)
	}
	if spec.Env["AGENTRUNTIME_SESSION_ID"] != "session-1" {
		t.Fatalf("AGENTRUNTIME_SESSION_ID: %q", spec.Env["AGENTRUNTIME_SESSION_ID"])
	}
}

func TestPrepareLaunchResumeBareSupressesPrompt(t *testing.T) {
	// codex resume --last treats the next positional as SESSION_ID, not PROMPT.
	// A prompt passed alongside bare resume would be misread as a session ID lookup.
	adapter := New(DefaultOptions())
	req := agentruntime.StartRequest{
		ID:      "session-1",
		Agent:   agentruntime.AgentCodex,
		Workdir: "/tmp/work",
		Resume:  true,
		Prompt:  "What word did you say?",
	}

	spec, err := adapter.PrepareLaunch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	for _, arg := range spec.Args {
		if arg == "What word did you say?" {
			t.Fatalf("prompt must not appear in bare resume args: %v", spec.Args)
		}
	}
}

func TestPrepareLaunchResumeSpecific(t *testing.T) {
	adapter := New(DefaultOptions())
	req := agentruntime.StartRequest{
		ID:       "session-1",
		Agent:    agentruntime.AgentCodex,
		Workdir:  "/tmp/work",
		Resume:   true,
		ResumeID: "00000000-0000-4000-8000-000000000099",
	}

	spec, err := adapter.PrepareLaunch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.Args) < 2 || spec.Args[0] != "resume" || spec.Args[1] != "00000000-0000-4000-8000-000000000099" {
		t.Fatalf("args[0:2]: %q, want [resume 00000000-0000-4000-8000-000000000099]", spec.Args)
	}
}

func TestPrepareLaunchResumeWithPrompt(t *testing.T) {
	adapter := New(DefaultOptions())
	req := agentruntime.StartRequest{
		ID:       "session-1",
		Agent:    agentruntime.AgentCodex,
		Workdir:  "/tmp/work",
		Resume:   true,
		ResumeID: "abc-def",
		Prompt:   "Continue where we left off.",
	}

	spec, err := adapter.PrepareLaunch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.Args) < 2 || spec.Args[0] != "resume" || spec.Args[1] != "abc-def" {
		t.Fatalf("resume subcommand and session id: %q", spec.Args)
	}
	if got := spec.Args[len(spec.Args)-1]; got != req.Prompt {
		t.Fatalf("prompt should be last arg: %q", got)
	}
}

func TestPrepareLaunchValidation(t *testing.T) {
	adapter := New(DefaultOptions())

	if _, err := adapter.PrepareLaunch(context.Background(), agentruntime.StartRequest{Agent: agentruntime.AgentCodex, Workdir: "/tmp"}); err == nil {
		t.Fatal("expected missing ID error")
	}
	if _, err := adapter.PrepareLaunch(context.Background(), agentruntime.StartRequest{ID: "x", Agent: agentruntime.AgentCodex}); err == nil {
		t.Fatal("expected missing workdir error")
	}
	if _, err := adapter.PrepareLaunch(context.Background(), agentruntime.StartRequest{ID: "x", Agent: agentruntime.AgentClaude, Workdir: "/tmp"}); err == nil {
		t.Fatal("expected unsupported agent error")
	}
}

func TestPrepareLaunchReservedEnvConflict(t *testing.T) {
	adapter := New(DefaultOptions())
	req := agentruntime.StartRequest{
		ID:      "session-1",
		Agent:   agentruntime.AgentCodex,
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
		Agent:   agentruntime.AgentCodex,
		Workdir: "/tmp/work",
		Env:     map[string]string{"AGENTRUNTIME_SESSION_ID": "session-1"},
	}
	spec, err := adapter.PrepareLaunch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Env["AGENTRUNTIME_SESSION_ID"] != "session-1" {
		t.Fatalf("AGENTRUNTIME_SESSION_ID: %q", spec.Env["AGENTRUNTIME_SESSION_ID"])
	}
}

func TestPrepareLaunchReservedEnvEmptyValueAllowed(t *testing.T) {
	adapter := New(DefaultOptions())
	req := agentruntime.StartRequest{
		ID:      "session-1",
		Agent:   agentruntime.AgentCodex,
		Workdir: "/tmp/work",
		Env:     map[string]string{"AGENTRUNTIME_SESSION_ID": ""},
	}
	spec, err := adapter.PrepareLaunch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Env["AGENTRUNTIME_SESSION_ID"] != "session-1" {
		t.Fatalf("AGENTRUNTIME_SESSION_ID: %q", spec.Env["AGENTRUNTIME_SESSION_ID"])
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
