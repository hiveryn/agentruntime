package opencode

import (
	"context"
	"os"
	"testing"

	"github.com/hiveryn/agentruntime"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

func TestNormalize_SessionCreated_Primary(t *testing.T) {
	a := New(DefaultOptions())
	ev, err := a.NormalizeEvent(context.Background(), fixture(t, "session_created_1.json"))
	if err != nil {
		t.Fatal(err)
	}
	if ev == nil {
		t.Fatal("expected event, got nil")
	}
	if ev.Status != agentruntime.StatusStarting {
		t.Errorf("status: got %q want %q", ev.Status, agentruntime.StatusStarting)
	}
	if ev.Agent != agentruntime.AgentOpenCode {
		t.Errorf("agent: got %q want %q", ev.Agent, agentruntime.AgentOpenCode)
	}
	if ev.ID != "hiv-opencode-capture-1" {
		t.Errorf("ID: got %q want %q", ev.ID, "hiv-opencode-capture-1")
	}
	if ev.NativeID != "ses_21873b6cbffeL3JOLPOFsrfKEt" {
		t.Errorf("NativeID: got %q", ev.NativeID)
	}
	if ev.PrimaryNativeID != "ses_21873b6cbffeL3JOLPOFsrfKEt" {
		t.Errorf("PrimaryNativeID: got %q", ev.PrimaryNativeID)
	}
	if ev.NativeSessionRole != agentruntime.NativeSessionRolePrimary {
		t.Errorf("role: got %q want %q", ev.NativeSessionRole, agentruntime.NativeSessionRolePrimary)
	}
}

func TestNormalize_NonCreatedEvents_LeaveRoleUnknown(t *testing.T) {
	// session.status, tool, idle events carry no parent info — the adapter must
	// leave PrimaryNativeID and Role unset so the ingest receiver can classify
	// subagent events correctly using its stored (agent, callerID) → primaryNativeID map.
	a := New(DefaultOptions())
	for _, name := range []string{"session_status_busy_1.json", "session_status_idle_1.json", "session_idle_1.json", "tool_before_1.json", "tool_after_1.json"} {
		ev, err := a.NormalizeEvent(context.Background(), fixture(t, name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if ev == nil {
			t.Fatalf("%s: unexpected nil", name)
		}
		if ev.PrimaryNativeID != "" {
			t.Errorf("%s: PrimaryNativeID should be empty, got %q", name, ev.PrimaryNativeID)
		}
		if ev.NativeSessionRole != agentruntime.NativeSessionRoleUnknown {
			t.Errorf("%s: role should be Unknown, got %q", name, ev.NativeSessionRole)
		}
	}
}

func TestNormalize_SessionCreated_Subagent(t *testing.T) {
	a := New(DefaultOptions())
	ev, err := a.NormalizeEvent(context.Background(), fixture(t, "session_created_subagent_1.json"))
	if err != nil {
		t.Fatal(err)
	}
	if ev == nil {
		t.Fatal("expected event, got nil")
	}
	if ev.Status != agentruntime.StatusStarting {
		t.Errorf("status: got %q want %q", ev.Status, agentruntime.StatusStarting)
	}
	if ev.NativeID != "ses_2187309a0ffei2yvqz6hijRuQX" {
		t.Errorf("NativeID: got %q", ev.NativeID)
	}
	if ev.PrimaryNativeID != "ses_21873b6cbffeL3JOLPOFsrfKEt" {
		t.Errorf("PrimaryNativeID: got %q", ev.PrimaryNativeID)
	}
	if ev.NativeSessionRole != agentruntime.NativeSessionRoleSubsession {
		t.Errorf("role: got %q want %q", ev.NativeSessionRole, agentruntime.NativeSessionRoleSubsession)
	}
	if ev.Metadata["parent_session_id"] != "ses_21873b6cbffeL3JOLPOFsrfKEt" {
		t.Errorf("metadata parent_session_id: got %q", ev.Metadata["parent_session_id"])
	}
}

func TestNormalize_SessionStatus(t *testing.T) {
	a := New(DefaultOptions())
	for _, tc := range []struct {
		name    string
		want    agentruntime.Status
		fixture string
	}{
		{name: "busy", fixture: "session_status_busy_1.json", want: agentruntime.StatusWorking},
		{name: "idle", fixture: "session_status_idle_1.json", want: agentruntime.StatusIdle},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ev, err := a.NormalizeEvent(context.Background(), fixture(t, tc.fixture))
			if err != nil {
				t.Fatal(err)
			}
			if ev == nil {
				t.Fatal("expected event, got nil")
			}
			if ev.Status != tc.want {
				t.Errorf("status: got %q want %q", ev.Status, tc.want)
			}
			if ev.NativeID != "ses_21873b6cbffeL3JOLPOFsrfKEt" {
				t.Errorf("NativeID: got %q", ev.NativeID)
			}
		})
	}
}

func TestNormalize_SessionIdle(t *testing.T) {
	a := New(DefaultOptions())
	ev, err := a.NormalizeEvent(context.Background(), fixture(t, "session_idle_1.json"))
	if err != nil {
		t.Fatal(err)
	}
	if ev == nil {
		t.Fatal("expected event, got nil")
	}
	if ev.Status != agentruntime.StatusIdle {
		t.Errorf("status: got %q want %q", ev.Status, agentruntime.StatusIdle)
	}
}

func TestNormalize_PermissionAsked(t *testing.T) {
	a := New(DefaultOptions())
	ev, err := a.NormalizeEvent(context.Background(), fixture(t, "permission_asked_1.json"))
	if err != nil {
		t.Fatal(err)
	}
	if ev == nil {
		t.Fatal("expected event, got nil")
	}
	if ev.Status != agentruntime.StatusAwaitingInput {
		t.Errorf("status: got %q want %q", ev.Status, agentruntime.StatusAwaitingInput)
	}
	if ev.Metadata["permission"] != "bash" {
		t.Errorf("metadata permission: got %q want %q", ev.Metadata["permission"], "bash")
	}
}

func TestNormalize_ToolBefore(t *testing.T) {
	a := New(DefaultOptions())
	ev, err := a.NormalizeEvent(context.Background(), fixture(t, "tool_before_1.json"))
	if err != nil {
		t.Fatal(err)
	}
	if ev == nil {
		t.Fatal("expected event, got nil")
	}
	if ev.Status != agentruntime.StatusWorking {
		t.Errorf("status: got %q want %q", ev.Status, agentruntime.StatusWorking)
	}
	if ev.Tool != "Bash" {
		t.Errorf("tool: got %q want %q", ev.Tool, "Bash")
	}
	if ev.Metadata["call_id"] != "call_KuqKK0jB1dYNkfUE6SmMHOwO" {
		t.Errorf("metadata call_id: got %q", ev.Metadata["call_id"])
	}
}

func TestNormalize_ToolAfter(t *testing.T) {
	a := New(DefaultOptions())
	ev, err := a.NormalizeEvent(context.Background(), fixture(t, "tool_after_1.json"))
	if err != nil {
		t.Fatal(err)
	}
	if ev == nil {
		t.Fatal("expected event, got nil")
	}
	if ev.Status != agentruntime.StatusWorking {
		t.Errorf("status: got %q want %q", ev.Status, agentruntime.StatusWorking)
	}
	if ev.Tool != "Bash" {
		t.Errorf("tool: got %q want %q", ev.Tool, "Bash")
	}
}

func TestNormalize_Dropped_SessionUpdated(t *testing.T) {
	a := New(DefaultOptions())
	ev, err := a.NormalizeEvent(context.Background(), fixture(t, "session_updated_1.json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ev != nil {
		t.Errorf("expected nil event for session.updated, got %+v", ev)
	}
}

func TestNormalize_Dropped_ServerDisposed(t *testing.T) {
	a := New(DefaultOptions())
	ev, err := a.NormalizeEvent(context.Background(), fixture(t, "server_disposed_1.json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ev != nil {
		t.Errorf("expected nil event for server.instance.disposed, got %+v", ev)
	}
}

func TestNormalize_RawPreserved(t *testing.T) {
	a := New(DefaultOptions())
	ev, err := a.NormalizeEvent(context.Background(), fixture(t, "session_created_1.json"))
	if err != nil {
		t.Fatal(err)
	}
	if ev == nil {
		t.Fatal("expected event, got nil")
	}
	if ev.Raw == nil {
		t.Fatal("Raw is nil")
	}
	for _, key := range []string{"hook_event_name", "session_id", "parent_session_id", "agentruntime_session_id", "payload"} {
		if _, ok := ev.Raw[key]; !ok {
			t.Errorf("Raw missing key %q", key)
		}
	}
}

func TestNormalize_FallbackID(t *testing.T) {
	a := New(DefaultOptions())
	// session with no agentruntime_session_id — should fall back to session_id
	data := []byte(`{
		"hook_event_name": "session.created",
		"session_id": "ses_native123",
		"parent_session_id": "",
		"agentruntime_session_id": "",
		"payload": {"sessionID": "ses_native123"}
	}`)
	ev, err := a.NormalizeEvent(context.Background(), data)
	if err != nil {
		t.Fatal(err)
	}
	if ev.ID != "ses_native123" {
		t.Errorf("ID fallback: got %q want %q", ev.ID, "ses_native123")
	}
}

func TestNormalize_QuestionTool_AwaitingInput(t *testing.T) {
	a := New(DefaultOptions())
	data := []byte(`{
		"hook_event_name": "tool.execute.before",
		"session_id": "ses_abc",
		"parent_session_id": "",
		"agentruntime_session_id": "hiv-test-1",
		"payload": {"tool": "question", "callID": "call_xyz"}
	}`)
	ev, err := a.NormalizeEvent(context.Background(), data)
	if err != nil {
		t.Fatal(err)
	}
	if ev == nil {
		t.Fatal("expected event, got nil")
	}
	if ev.Status != agentruntime.StatusAwaitingInput {
		t.Errorf("status: got %q want %q", ev.Status, agentruntime.StatusAwaitingInput)
	}
	if ev.Tool != "Question" {
		t.Errorf("tool: got %q want %q", ev.Tool, "Question")
	}
}

func TestNormalize_QuestionTool_After_Working(t *testing.T) {
	a := New(DefaultOptions())
	data := []byte(`{
		"hook_event_name": "tool.execute.after",
		"session_id": "ses_abc",
		"parent_session_id": "",
		"agentruntime_session_id": "hiv-test-1",
		"payload": {"tool": "question", "callID": "call_xyz"}
	}`)
	ev, err := a.NormalizeEvent(context.Background(), data)
	if err != nil {
		t.Fatal(err)
	}
	if ev == nil {
		t.Fatal("expected event, got nil")
	}
	if ev.Status != agentruntime.StatusWorking {
		t.Errorf("status: got %q want %q", ev.Status, agentruntime.StatusWorking)
	}
}

func TestNormalize_UnknownEventDropped(t *testing.T) {
	a := New(DefaultOptions())
	data := []byte(`{
		"hook_event_name": "some.unknown.event",
		"session_id": "ses_123",
		"payload": {}
	}`)
	ev, err := a.NormalizeEvent(context.Background(), data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ev != nil {
		t.Errorf("expected nil for unknown event, got %+v", ev)
	}
}
