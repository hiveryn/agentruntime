package opencode

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hiveryn/agentruntime"
)

func launchConfig(t *testing.T, req agentruntime.StartRequest) (agentruntime.LaunchSpec, string) {
	t.Helper()
	spec, err := New(DefaultOptions()).PrepareLaunch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	return spec, spec.Env["OPENCODE_CONFIG_CONTENT"]
}

func TestPrepareLaunch_DisableNativeQuestionsPermission(t *testing.T) {
	for _, tc := range []struct {
		name string
		mod  func(*agentruntime.StartRequest)
		want string
	}{
		{"default", func(*agentruntime.StartRequest) {}, `{"question":"deny"}`},
		// Object form keeps every other tool allowed; "*" precedes the rule.
		{"yolo", func(r *agentruntime.StartRequest) { r.Yolo = true }, `{"*":"allow","question":"deny"}`},
		{"yolo-additional", func(r *agentruntime.StartRequest) {
			r.Yolo = true
			r.AdditionalWorkdirs = []string{"/repo-b"}
		}, `{"*":"allow","question":"deny"}`},
		{"additional", func(r *agentruntime.StartRequest) { r.AdditionalWorkdirs = []string{"/repo-b"} },
			`{"external_directory":{"/repo-b/**":"allow"},"question":"deny"}`},
		{"resume-id", func(r *agentruntime.StartRequest) { r.Yolo, r.Resume, r.ResumeID = true, true, "ses_1" }, `{"*":"allow","question":"deny"}`},
		{"resume-bare", func(r *agentruntime.StartRequest) { r.Yolo, r.Resume = true, true }, `{"*":"allow","question":"deny"}`},
	} {
		req := baseReq()
		req.DisableNativeQuestions = true
		tc.mod(&req)
		_, raw := launchConfig(t, req)
		var cfg struct {
			Permission json.RawMessage `json:"permission"`
		}
		if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
			t.Fatal(err)
		}
		if string(cfg.Permission) != tc.want {
			t.Fatalf("%s: permission = %s, want %s", tc.name, cfg.Permission, tc.want)
		}
	}
}

func TestPrepareLaunch_QuestionsEnabledKeepsPermission(t *testing.T) {
	req := baseReq()
	req.Yolo = true
	_, raw := launchConfig(t, req)
	if !strings.Contains(raw, `"permission":"allow"`) {
		t.Fatalf("yolo without DisableNativeQuestions must stay permission allow: %s", raw)
	}
	_, raw = launchConfig(t, baseReq())
	if strings.Contains(raw, "permission") {
		t.Fatalf("permission must be omitted by default: %s", raw)
	}
}

func TestPrepareLaunch_DisableNativeQuestionsAgentConflict(t *testing.T) {
	for _, perm := range []map[string]string{{"question": "allow"}, {"question": "ask"}, {"*": "allow"}} {
		req := baseReq()
		req.DisableNativeQuestions = true
		req.OpenCodeAgentConfig = map[string]agentruntime.OpenCodeAgentConfig{"arch": {Mode: "primary", Permission: perm}}
		if _, err := New(DefaultOptions()).PrepareLaunch(context.Background(), req); err == nil || !strings.Contains(err.Error(), "question tool") {
			t.Fatalf("permission %v: expected question conflict, got %v", perm, err)
		}
	}

	req := baseReq()
	req.DisableNativeQuestions = true
	req.OpenCodeAgentConfig = map[string]agentruntime.OpenCodeAgentConfig{"arch": {Mode: "primary", Permission: map[string]string{"question": "deny", "bash": "allow"}}}
	if _, err := New(DefaultOptions()).PrepareLaunch(context.Background(), req); err != nil {
		t.Fatalf("compatible agent permission rejected: %v", err)
	}

	req.DisableNativeQuestions = false
	req.OpenCodeAgentConfig = map[string]agentruntime.OpenCodeAgentConfig{"arch": {Mode: "primary", Permission: map[string]string{"question": "allow"}}}
	if _, err := New(DefaultOptions()).PrepareLaunch(context.Background(), req); err != nil {
		t.Fatalf("agent question permission must be free when questions stay enabled: %v", err)
	}
}

func TestPrepareLaunch_MCPToolTimeout(t *testing.T) {
	for _, resume := range []bool{false, true} {
		req := baseReq()
		req.Resume, req.ResumeID = resume, "ses_1"
		req.MCPServers = []agentruntime.MCPServerConfig{
			{Name: "hiveryn", Command: "hiverynd", ToolTimeout: 65 * time.Minute},
			{Name: "remote", URL: "http://x", ToolTimeout: time.Microsecond},
			{Name: "other", Command: "other"},
		}
		_, raw := launchConfig(t, req)
		var cfg ocConfig
		if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
			t.Fatal(err)
		}
		got := map[string]int64{}
		for name, server := range cfg.MCP {
			got[name] = server.Timeout
		}
		want := map[string]int64{"hiveryn": 3900000, "remote": 1, "other": 0}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("resume=%v: timeouts = %v, want %v", resume, got, want)
		}
		if strings.Contains(raw, `"other":{`) && strings.Contains(raw, `"timeout":0`) {
			t.Fatalf("resume=%v: zero timeout must be omitted: %s", resume, raw)
		}
	}

	req := baseReq()
	req.MCPServers = []agentruntime.MCPServerConfig{{Name: "x", Command: "x", ToolTimeout: -time.Second}}
	if _, err := New(DefaultOptions()).PrepareLaunch(context.Background(), req); err == nil || !strings.Contains(err.Error(), "negative tool timeout") {
		t.Fatalf("expected negative timeout error, got %v", err)
	}
}
