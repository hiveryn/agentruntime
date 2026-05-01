package ingest

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/hiveryn/agentruntime"
	"github.com/hiveryn/agentruntime/adapter/claude"
	"github.com/hiveryn/agentruntime/adapter/codex"
)

func TestReceiverIngestPublishesMatchingSession(t *testing.T) {
	receiver := NewReceiver(codex.New(codex.DefaultOptions()))
	sub := receiver.Hub().Subscribe(Filter{ID: "hiveryn-lab-homehooks-1"})
	defer sub.Close()

	data := readFixture(t, "sessionstart_1.json")
	event, err := receiver.Ingest(context.Background(), agentruntime.AgentCodex, data)
	if err != nil {
		t.Fatal(err)
	}
	if event == nil {
		t.Fatal("expected event")
	}
	if event.ID != "hiveryn-lab-homehooks-1" {
		t.Fatalf("ID: %q", event.ID)
	}
	if event.NativeID == "" {
		t.Fatal("expected NativeID")
	}
	if event.PrimaryNativeID != event.NativeID {
		t.Fatalf("PrimaryNativeID: got %q want %q", event.PrimaryNativeID, event.NativeID)
	}
	if event.NativeSessionRole != agentruntime.NativeSessionRolePrimary {
		t.Fatalf("NativeSessionRole: got %q want %q", event.NativeSessionRole, agentruntime.NativeSessionRolePrimary)
	}

	select {
	case got := <-sub.Events:
		if got.ID != event.ID || got.NativeID != event.NativeID || got.Status != event.Status {
			t.Fatalf("got %+v want %+v", got, *event)
		}
		if got.NativeSessionRole != agentruntime.NativeSessionRolePrimary {
			t.Fatalf("published NativeSessionRole: %q", got.NativeSessionRole)
		}
	default:
		t.Fatal("expected published event")
	}
}

func TestReceiverClassifiesSubsessionEvents(t *testing.T) {
	receiver := NewReceiver(codex.New(codex.DefaultOptions()))

	primary, err := receiver.Ingest(context.Background(), agentruntime.AgentCodex, []byte(`{"hook":{"hook_event_name":"SessionStart","session_id":"native-primary"},"env":{"HIVERYN_SESSION_ID":"caller-1"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if primary.NativeSessionRole != agentruntime.NativeSessionRolePrimary {
		t.Fatalf("primary role: %q", primary.NativeSessionRole)
	}
	if primary.PrimaryNativeID != "native-primary" {
		t.Fatalf("primary native ID: %q", primary.PrimaryNativeID)
	}

	subsession, err := receiver.Ingest(context.Background(), agentruntime.AgentCodex, []byte(`{"hook":{"hook_event_name":"SessionStart","session_id":"native-sub"},"env":{"HIVERYN_SESSION_ID":"caller-1"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if subsession.NativeSessionRole != agentruntime.NativeSessionRoleSubsession {
		t.Fatalf("subsession role: %q", subsession.NativeSessionRole)
	}
	if subsession.PrimaryNativeID != "native-primary" {
		t.Fatalf("subsession primary native ID: %q", subsession.PrimaryNativeID)
	}

	stop, err := receiver.Ingest(context.Background(), agentruntime.AgentCodex, []byte(`{"hook":{"hook_event_name":"Stop","session_id":"native-sub"},"env":{"HIVERYN_SESSION_ID":"caller-1"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if stop.Status != agentruntime.StatusIdle {
		t.Fatalf("stop status: %q", stop.Status)
	}
	if stop.NativeSessionRole != agentruntime.NativeSessionRoleSubsession {
		t.Fatalf("stop role: %q", stop.NativeSessionRole)
	}
	if stop.PrimaryNativeID != "native-primary" {
		t.Fatalf("stop primary native ID: %q", stop.PrimaryNativeID)
	}

	primaryStop, err := receiver.Ingest(context.Background(), agentruntime.AgentCodex, []byte(`{"hook":{"hook_event_name":"Stop","session_id":"native-primary"},"env":{"HIVERYN_SESSION_ID":"caller-1"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if primaryStop.NativeSessionRole != agentruntime.NativeSessionRolePrimary {
		t.Fatalf("primary stop role: %q", primaryStop.NativeSessionRole)
	}
}

func TestReceiverLeavesRoleUnknownWithoutNativeID(t *testing.T) {
	receiver := NewReceiver(codex.New(codex.DefaultOptions()))

	event, err := receiver.Ingest(context.Background(), agentruntime.AgentCodex, []byte(`{"hook":{"hook_event_name":"SessionStart"},"env":{"HIVERYN_SESSION_ID":"caller-1"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if event.NativeSessionRole != agentruntime.NativeSessionRoleUnknown {
		t.Fatalf("role: %q", event.NativeSessionRole)
	}
	if event.PrimaryNativeID != "" {
		t.Fatalf("PrimaryNativeID: %q", event.PrimaryNativeID)
	}
}

func TestReceiverDropsUnknownEvent(t *testing.T) {
	receiver := NewReceiver(codex.New(codex.DefaultOptions()))
	sub := receiver.Hub().Subscribe(Filter{ID: "native-123"})
	defer sub.Close()

	event, err := receiver.Ingest(context.Background(), agentruntime.AgentCodex, []byte(`{"hook":{"hook_event_name":"SomethingElse","session_id":"native-123"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if event != nil {
		t.Fatalf("expected nil event, got %+v", event)
	}

	select {
	case got := <-sub.Events:
		t.Fatalf("unexpected event: %+v", got)
	default:
	}
}

func TestReceiverRejectsUnsupportedAgent(t *testing.T) {
	receiver := NewReceiver()
	if _, err := receiver.Ingest(context.Background(), agentruntime.AgentCodex, []byte(`{}`)); err == nil {
		t.Fatal("expected error")
	}
}

func TestReceiverIngestPublishesClaudeSession(t *testing.T) {
	receiver := NewReceiver(claude.New(claude.DefaultOptions()))
	sub := receiver.Hub().Subscribe(Filter{ID: "hiv-claude-lab-1"})
	defer sub.Close()

	data := readClaudeFixture(t, "sessionstart_1.json")
	event, err := receiver.Ingest(context.Background(), agentruntime.AgentClaude, data)
	if err != nil {
		t.Fatal(err)
	}
	if event == nil {
		t.Fatal("expected event")
	}
	if event.Agent != agentruntime.AgentClaude {
		t.Fatalf("agent: %q", event.Agent)
	}
	if event.PrimaryNativeID != event.NativeID {
		t.Fatalf("PrimaryNativeID: got %q want %q", event.PrimaryNativeID, event.NativeID)
	}
	if event.NativeSessionRole != agentruntime.NativeSessionRolePrimary {
		t.Fatalf("NativeSessionRole: got %q want %q", event.NativeSessionRole, agentruntime.NativeSessionRolePrimary)
	}

	select {
	case got := <-sub.Events:
		if got.ID != event.ID || got.NativeID != event.NativeID || got.Status != event.Status {
			t.Fatalf("got %+v want %+v", got, *event)
		}
	default:
		t.Fatal("expected published event")
	}
}

func TestReceiverKeepsClaudeSubagentParentWhenSeenFirst(t *testing.T) {
	receiver := NewReceiver(claude.New(claude.DefaultOptions()))

	subagent, err := receiver.Ingest(context.Background(), agentruntime.AgentClaude, []byte(`{"hook":{"hook_event_name":"PreToolUse","session_id":"native-primary","agent_id":"native-sub","agent_type":"Explore","tool_name":"Read"},"env":{"HIVERYN_SESSION_ID":"caller-1"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if subagent.NativeSessionRole != agentruntime.NativeSessionRoleSubsession {
		t.Fatalf("subagent role: %q", subagent.NativeSessionRole)
	}
	if subagent.PrimaryNativeID != "native-primary" {
		t.Fatalf("subagent primary native ID: %q", subagent.PrimaryNativeID)
	}

	primary, err := receiver.Ingest(context.Background(), agentruntime.AgentClaude, []byte(`{"hook":{"hook_event_name":"SessionStart","session_id":"native-primary"},"env":{"HIVERYN_SESSION_ID":"caller-1"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if primary.NativeSessionRole != agentruntime.NativeSessionRolePrimary {
		t.Fatalf("primary role: %q", primary.NativeSessionRole)
	}
	if primary.PrimaryNativeID != "native-primary" {
		t.Fatalf("primary native ID: %q", primary.PrimaryNativeID)
	}
}

func TestReceiverSeparatesPrimaryNativeByAgent(t *testing.T) {
	receiver := NewReceiver(codex.New(codex.DefaultOptions()), claude.New(claude.DefaultOptions()))

	codexEvent, err := receiver.Ingest(context.Background(), agentruntime.AgentCodex, []byte(`{"hook":{"hook_event_name":"SessionStart","session_id":"codex-native"},"env":{"HIVERYN_SESSION_ID":"caller-1"}}`))
	if err != nil {
		t.Fatal(err)
	}
	claudeEvent, err := receiver.Ingest(context.Background(), agentruntime.AgentClaude, []byte(`{"hook":{"hook_event_name":"SessionStart","session_id":"claude-native"},"env":{"HIVERYN_SESSION_ID":"caller-1"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if codexEvent.NativeSessionRole != agentruntime.NativeSessionRolePrimary {
		t.Fatalf("codex role: %q", codexEvent.NativeSessionRole)
	}
	if claudeEvent.NativeSessionRole != agentruntime.NativeSessionRolePrimary {
		t.Fatalf("claude role: %q", claudeEvent.NativeSessionRole)
	}
	if codexEvent.PrimaryNativeID != "codex-native" {
		t.Fatalf("codex primary native ID: %q", codexEvent.PrimaryNativeID)
	}
	if claudeEvent.PrimaryNativeID != "claude-native" {
		t.Fatalf("claude primary native ID: %q", claudeEvent.PrimaryNativeID)
	}
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "adapter", "codex", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func readClaudeFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "adapter", "claude", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}
