package claude

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hiveryn/agentruntime"
)

func TestPrepareLaunchDisableNativeQuestions(t *testing.T) {
	adapter := New(Options{NewSessionID: func() (string, error) { return "00000000-0000-4000-8000-000000000001", nil }})
	for _, resume := range []bool{false, true} {
		req := agentruntime.StartRequest{ID: "s", Workdir: "/tmp/work", Prompt: "hi", DisableNativeQuestions: true, Resume: resume, ResumeID: map[bool]string{true: "abc"}[resume]}
		spec, err := adapter.PrepareLaunch(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(spec.Args, "--disallowedTools=AskUserQuestion") {
			t.Fatalf("resume=%v: args missing --disallowedTools=AskUserQuestion: %q", resume, spec.Args)
		}
		// The variadic flag must not take a separate value token.
		if slices.Contains(spec.Args, "--disallowedTools") {
			t.Fatalf("resume=%v: variadic --disallowedTools used without = form: %q", resume, spec.Args)
		}
	}

	spec, err := adapter.PrepareLaunch(context.Background(), agentruntime.StartRequest{ID: "s", Workdir: "/tmp/work"})
	if err != nil {
		t.Fatal(err)
	}
	for _, arg := range spec.Args {
		if strings.HasPrefix(arg, "--disallowedTools") {
			t.Fatalf("question tool disabled without DisableNativeQuestions: %q", spec.Args)
		}
	}
}

func TestPrepareLaunchMCPToolTimeout(t *testing.T) {
	adapter := New(Options{NewSessionID: func() (string, error) { return "", nil }})
	for _, resume := range []bool{false, true} {
		req := agentruntime.StartRequest{
			ID: "s", Workdir: "/tmp/work", Resume: resume,
			Env: map[string]string{"KEEP": "1"},
			MCPServers: []agentruntime.MCPServerConfig{
				{Name: "hiveryn", Command: "hiverynd", ToolTimeout: 65 * time.Minute},
				{Name: "other", Command: "other"},
			},
		}
		spec, err := adapter.PrepareLaunch(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		config := readMCPConfig(t, spec.CleanupPaths[0])
		_ = os.Remove(spec.CleanupPaths[0])
		if got := config.MCPServers["hiveryn"].Timeout; got != 3900000 {
			t.Fatalf("resume=%v: hiveryn timeout = %d, want 3900000", resume, got)
		}
		if got := config.MCPServers["other"].Timeout; got != 0 {
			t.Fatalf("resume=%v: other timeout = %d, want provider default", resume, got)
		}
		if spec.Env[autoBackgroundEnv] != "0" {
			t.Fatalf("resume=%v: %s = %q, want 0", resume, autoBackgroundEnv, spec.Env[autoBackgroundEnv])
		}
		if spec.Env["KEEP"] != "1" {
			t.Fatalf("resume=%v: caller env dropped: %#v", resume, spec.Env)
		}
	}
}

func TestPrepareLaunchMCPToolTimeoutRoundsUpToMilliseconds(t *testing.T) {
	adapter := New(Options{NewSessionID: func() (string, error) { return "", nil }})
	spec, err := adapter.PrepareLaunch(context.Background(), agentruntime.StartRequest{
		ID: "s", Workdir: "/tmp/work",
		MCPServers: []agentruntime.MCPServerConfig{{Name: "x", Command: "x", ToolTimeout: time.Microsecond}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(spec.CleanupPaths[0]) }()
	if got := readMCPConfig(t, spec.CleanupPaths[0]).MCPServers["x"].Timeout; got != 1 {
		t.Fatalf("timeout = %d, want 1", got)
	}
}

func TestPrepareLaunchNoToolTimeoutLeavesBackgroundingAlone(t *testing.T) {
	adapter := New(Options{NewSessionID: func() (string, error) { return "", nil }})
	spec, err := adapter.PrepareLaunch(context.Background(), agentruntime.StartRequest{
		ID: "s", Workdir: "/tmp/work",
		Env:        map[string]string{autoBackgroundEnv: "5000"},
		MCPServers: []agentruntime.MCPServerConfig{{Name: "x", Command: "x"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(spec.CleanupPaths[0]) }()
	if spec.Env[autoBackgroundEnv] != "5000" {
		t.Fatalf("%s = %q, want caller value kept", autoBackgroundEnv, spec.Env[autoBackgroundEnv])
	}
	if got := readMCPConfig(t, spec.CleanupPaths[0]).MCPServers["x"].Timeout; got != 0 {
		t.Fatalf("timeout = %d, want omitted", got)
	}
}

func TestPrepareLaunchMCPToolTimeoutConflicts(t *testing.T) {
	adapter := New(Options{NewSessionID: func() (string, error) { return "", nil }})
	servers := []agentruntime.MCPServerConfig{{Name: "x", Command: "x", ToolTimeout: time.Hour}}

	if _, err := adapter.PrepareLaunch(context.Background(), agentruntime.StartRequest{
		ID: "s", Workdir: "/tmp/work", MCPServers: servers,
		Env: map[string]string{autoBackgroundEnv: "120000"},
	}); err == nil || !strings.Contains(err.Error(), autoBackgroundEnv) {
		t.Fatalf("expected %s conflict, got %v", autoBackgroundEnv, err)
	}

	spec, err := adapter.PrepareLaunch(context.Background(), agentruntime.StartRequest{
		ID: "s", Workdir: "/tmp/work", MCPServers: servers,
		Env: map[string]string{autoBackgroundEnv: "0"},
	})
	if err != nil {
		t.Fatalf("matching %s=0 must be accepted: %v", autoBackgroundEnv, err)
	}
	_ = os.Remove(spec.CleanupPaths[0])

	if _, err := adapter.PrepareLaunch(context.Background(), agentruntime.StartRequest{
		ID: "s", Workdir: "/tmp/work",
		MCPServers: []agentruntime.MCPServerConfig{{Name: "x", Command: "x", ToolTimeout: -time.Second}},
	}); err == nil || !strings.Contains(err.Error(), "negative tool timeout") {
		t.Fatalf("expected negative timeout error, got %v", err)
	}
}
