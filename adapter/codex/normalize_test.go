package codex

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/hiveryn/agentruntime"
)

func TestCapturedCodexExecSequence(t *testing.T) {
	adapter := New(DefaultOptions())
	files := []string{
		"sessionstart_1.json",
		"userpromptsubmit_1.json",
		"pretooluse_1.json",
		"posttooluse_1.json",
		"stop_1.json",
	}
	wantStatus := []agentruntime.Status{
		agentruntime.StatusStarting,
		agentruntime.StatusWorking,
		agentruntime.StatusWorking,
		agentruntime.StatusWorking,
		agentruntime.StatusIdle,
	}
	wantTool := []string{"", "", "Bash", "Bash", ""}

	var native string
	for i, name := range files {
		data, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			t.Fatalf("read fixture %s: %v", name, err)
		}
		event, err := adapter.NormalizeEvent(context.Background(), data)
		if err != nil {
			t.Fatalf("normalize %s: %v", name, err)
		}
		if event == nil {
			t.Fatalf("normalize %s returned nil", name)
		}
		if event.Agent != agentruntime.AgentCodex {
			t.Errorf("%s agent: %q", name, event.Agent)
		}
		if event.ID != "hiveryn-lab-homehooks-1" {
			t.Errorf("%s ID: got %q", name, event.ID)
		}
		if event.NativeID == "" {
			t.Errorf("%s NativeID empty", name)
		}
		if native == "" {
			native = event.NativeID
		} else if event.NativeID != native {
			t.Errorf("%s NativeID changed: got %q want %q", name, event.NativeID, native)
		}
		if event.Status != wantStatus[i] {
			t.Errorf("%s status: got %q want %q", name, event.Status, wantStatus[i])
		}
		if event.Tool != wantTool[i] {
			t.Errorf("%s tool: got %q want %q", name, event.Tool, wantTool[i])
		}
		if event.At.IsZero() {
			t.Errorf("%s At zero", name)
		}
		if event.Raw == nil {
			t.Errorf("%s Raw nil", name)
		}
		if hook, _ := event.Raw["hook"].(map[string]any); hook == nil {
			t.Errorf("%s Raw missing hook envelope", name)
		}
	}
}

func TestNormalizeFallsBackToNativeIDWhenCallerIDMissing(t *testing.T) {
	adapter := New(DefaultOptions())
	data := []byte(`{"hook":{"hook_event_name":"SessionStart","session_id":"native-123"}}`)

	event, err := adapter.NormalizeEvent(context.Background(), data)
	if err != nil {
		t.Fatal(err)
	}
	if event.ID != "native-123" || event.NativeID != "native-123" {
		t.Fatalf("got ID=%q NativeID=%q", event.ID, event.NativeID)
	}
}

func TestNormalizeDropsUnknownEvent(t *testing.T) {
	adapter := New(DefaultOptions())
	data := []byte(`{"hook":{"hook_event_name":"SomethingElse","session_id":"native-123"}}`)

	event, err := adapter.NormalizeEvent(context.Background(), data)
	if err != nil {
		t.Fatal(err)
	}
	if event != nil {
		t.Fatalf("expected nil, got %+v", event)
	}
}

func TestPermissionRequestMapsToWorking(t *testing.T) {
	adapter := New(DefaultOptions())
	data := []byte(`{"hook":{"hook_event_name":"PermissionRequest","session_id":"native-123","tool_name":"apply_patch"},"env":{"AGENTRUNTIME_SESSION_ID":"hiv-123"}}`)

	event, err := adapter.NormalizeEvent(context.Background(), data)
	if err != nil {
		t.Fatal(err)
	}
	if event.Status != agentruntime.StatusWorking || event.Tool != "ApplyPatch" || event.ID != "hiv-123" {
		t.Fatalf("got %+v", event)
	}
}

func TestNormalizePreservesEnvelopeForDiagnostics(t *testing.T) {
	adapter := New(DefaultOptions())
	data := []byte(`{"received_at":"2026-05-01T08:46:58.178726Z","hook":{"hook_event_name":"SessionStart","session_id":"native-123"},"env":{"AGENTRUNTIME_SESSION_ID":"hiv-123"},"hook_cwd":"/tmp/work","args":["hook"]}`)

	event, err := adapter.NormalizeEvent(context.Background(), data)
	if err != nil {
		t.Fatal(err)
	}
	if event == nil {
		t.Fatal("expected event")
	}
	if got := event.Raw["received_at"]; got != "2026-05-01T08:46:58.178726Z" {
		t.Fatalf("received_at: %#v", got)
	}
	if got := event.Raw["hook_cwd"]; got != "/tmp/work" {
		t.Fatalf("hook_cwd: %#v", got)
	}
	env, _ := event.Raw["env"].(map[string]string)
	if env == nil || env["AGENTRUNTIME_SESSION_ID"] != "hiv-123" {
		t.Fatalf("env: %#v", event.Raw["env"])
	}
	args, _ := event.Raw["args"].([]string)
	if len(args) != 1 || args[0] != "hook" {
		t.Fatalf("args: %#v", event.Raw["args"])
	}
}
