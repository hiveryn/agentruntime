package claude

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/hiveryn/agentruntime"
)

func TestPrepareLaunchClaudeAutoMemory(t *testing.T) {
	adapter := New(Options{NewSessionID: func() (string, error) { return "00000000-0000-4000-8000-000000000001", nil }})
	for _, enabled := range []bool{false, true} {
		want := map[bool]string{false: "1", true: "0"}[enabled]
		for _, req := range []agentruntime.StartRequest{
			{ID: "s", Workdir: "/tmp/work", Prompt: "hi"},
			{ID: "s", Workdir: "/tmp/work", Resume: true},
			{ID: "s", Workdir: "/tmp/work", Resume: true, ResumeID: "abc"},
		} {
			req.ClaudeAutoMemory = enabled
			spec, err := adapter.PrepareLaunch(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			if got := spec.Env[autoMemoryEnv]; got != want {
				t.Fatalf("enabled=%v resume=%v id=%q: %s = %q, want %q", enabled, req.Resume, req.ResumeID, autoMemoryEnv, got, want)
			}
			// Auto-memory must not disturb transcript persistence or resume.
			if spec.Env["CLAUDE_CODE_FORCE_SESSION_PERSISTENCE"] != "1" {
				t.Fatalf("persistence not forced: %#v", spec.Env)
			}
			if req.ResumeID != "" && !slices.Contains(spec.Args, req.ResumeID) {
				t.Fatalf("resume id dropped: %q", spec.Args)
			}
		}
	}
}

func TestPrepareLaunchClaudeAutoMemoryEnvConflict(t *testing.T) {
	adapter := New(Options{NewSessionID: func() (string, error) { return "", nil }})
	cases := []struct {
		enabled bool
		value   string
		ok      bool
	}{
		{false, "1", true},
		{true, "0", true},
		{false, "0", false},
		{true, "1", false},
		{false, "true", false},
		{false, "", false},
	}
	for _, tc := range cases {
		spec, err := adapter.PrepareLaunch(context.Background(), agentruntime.StartRequest{
			ID: "s", Workdir: "/tmp/work", ClaudeAutoMemory: tc.enabled,
			Env: map[string]string{autoMemoryEnv: tc.value},
		})
		if tc.ok {
			if err != nil {
				t.Fatalf("enabled=%v %s=%q: matching value must be accepted: %v", tc.enabled, autoMemoryEnv, tc.value, err)
			}
			if spec.Env[autoMemoryEnv] != tc.value {
				t.Fatalf("enabled=%v: %s = %q", tc.enabled, autoMemoryEnv, spec.Env[autoMemoryEnv])
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), autoMemoryEnv) {
			t.Fatalf("enabled=%v %s=%q: expected conflict, got %v", tc.enabled, autoMemoryEnv, tc.value, err)
		}
	}
}
