package claude

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/hiveryn/agentruntime"
)

func TestCapturedClaudeSequence(t *testing.T) {
	adapter := New(DefaultOptions())
	files := []string{
		"sessionstart_1.json",
		"userpromptsubmit_1.json",
		"pretooluse_agent_1.json",
		"subagentstart_1.json",
		"pretooluse_subagent_1.json",
		"subagentstop_1.json",
		"posttooluse_agent_1.json",
		"stop_1.json",
		"sessionend_1.json",
	}
	wantStatus := []agentruntime.Status{
		agentruntime.StatusStarting,
		agentruntime.StatusWorking,
		agentruntime.StatusWorking,
		agentruntime.StatusStarting,
		agentruntime.StatusWorking,
		agentruntime.StatusIdle,
		agentruntime.StatusWorking,
		agentruntime.StatusIdle,
		agentruntime.StatusEnded,
	}
	wantTool := []string{"", "", "Agent", "", "Read", "", "Agent", "", ""}
	wantNative := []string{
		"00000000-0000-4000-8000-000000000001",
		"00000000-0000-4000-8000-000000000001",
		"00000000-0000-4000-8000-000000000001",
		"a1bcb29f5d4b9670b",
		"a1bcb29f5d4b9670b",
		"a1bcb29f5d4b9670b",
		"00000000-0000-4000-8000-000000000001",
		"00000000-0000-4000-8000-000000000001",
		"00000000-0000-4000-8000-000000000001",
	}
	wantRole := []agentruntime.NativeSessionRole{
		agentruntime.NativeSessionRolePrimary,
		agentruntime.NativeSessionRolePrimary,
		agentruntime.NativeSessionRolePrimary,
		agentruntime.NativeSessionRoleSubsession,
		agentruntime.NativeSessionRoleSubsession,
		agentruntime.NativeSessionRoleSubsession,
		agentruntime.NativeSessionRolePrimary,
		agentruntime.NativeSessionRolePrimary,
		agentruntime.NativeSessionRolePrimary,
	}

	for i, name := range files {
		data := readFixture(t, name)
		event, err := adapter.NormalizeEvent(context.Background(), data)
		if err != nil {
			t.Fatalf("normalize %s: %v", name, err)
		}
		if event == nil {
			t.Fatalf("normalize %s returned nil", name)
		}
		if event.Agent != agentruntime.AgentClaude {
			t.Errorf("%s agent: %q", name, event.Agent)
		}
		if event.ID != "hiv-claude-lab-1" {
			t.Errorf("%s ID: got %q", name, event.ID)
		}
		if event.NativeID != wantNative[i] {
			t.Errorf("%s NativeID: got %q want %q", name, event.NativeID, wantNative[i])
		}
		if event.PrimaryNativeID != "00000000-0000-4000-8000-000000000001" {
			t.Errorf("%s PrimaryNativeID: got %q", name, event.PrimaryNativeID)
		}
		if event.NativeSessionRole != wantRole[i] {
			t.Errorf("%s role: got %q want %q", name, event.NativeSessionRole, wantRole[i])
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
		if wantRole[i] == agentruntime.NativeSessionRoleSubsession && event.Metadata["agent_type"] != "Explore" {
			t.Errorf("%s agent_type: got %q", name, event.Metadata["agent_type"])
		}
	}
}

func TestNormalizePermissionRequestMapsToAwaitingInput(t *testing.T) {
	adapter := New(DefaultOptions())
	data := []byte(`{"hook":{"hook_event_name":"PermissionRequest","session_id":"native-123","tool_name":"apply_patch"},"env":{"HIVERYN_SESSION_ID":"hiv-123"}}`)

	event, err := adapter.NormalizeEvent(context.Background(), data)
	if err != nil {
		t.Fatal(err)
	}
	if event.Status != agentruntime.StatusAwaitingInput || event.Tool != "ApplyPatch" || event.ID != "hiv-123" {
		t.Fatalf("got %+v", event)
	}
}

func TestNormalizeStopFailureMapsToError(t *testing.T) {
	adapter := New(DefaultOptions())
	data := []byte(`{"hook":{"hook_event_name":"StopFailure","session_id":"native-123","error":"rate_limit"},"env":{"HIVERYN_SESSION_ID":"hiv-123"}}`)

	event, err := adapter.NormalizeEvent(context.Background(), data)
	if err != nil {
		t.Fatal(err)
	}
	if event.Status != agentruntime.StatusError || event.NativeSessionRole != agentruntime.NativeSessionRolePrimary {
		t.Fatalf("got %+v", event)
	}
}

func TestNormalizeDropsInstructionsLoaded(t *testing.T) {
	adapter := New(DefaultOptions())
	data := []byte(`{"hook":{"hook_event_name":"InstructionsLoaded","session_id":"native-123"}}`)

	event, err := adapter.NormalizeEvent(context.Background(), data)
	if err != nil {
		t.Fatal(err)
	}
	if event != nil {
		t.Fatalf("expected nil, got %+v", event)
	}
}

func TestNormalizeAcceptsCapturedWrapper(t *testing.T) {
	adapter := New(DefaultOptions())
	data := []byte(`{"payload":{"agent":"claude","env":{"HIVERYN_SESSION_ID":"hiv-123"},"hook":{"hook_event_name":"SessionStart","session_id":"native-123","source":"startup"},"received_at":"2026-05-01T13:31:16.146505+00:00"}}`)

	event, err := adapter.NormalizeEvent(context.Background(), data)
	if err != nil {
		t.Fatal(err)
	}
	if event == nil {
		t.Fatal("expected event")
	}
	if event.ID != "hiv-123" || event.NativeID != "native-123" || event.NativeSessionRole != agentruntime.NativeSessionRolePrimary {
		t.Fatalf("got %+v", event)
	}
	if got := event.Raw["agent"]; got != "claude" {
		t.Fatalf("agent: %#v", got)
	}
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}
