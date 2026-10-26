package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hiveryn/agentruntime"
	"github.com/hiveryn/agentruntime/internal/chatwire"
)

func TestConversationNativeCorrelationAndPermissionWait(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	input := strings.Join([]string{
		`{"type":"control_request","request_id":"approval","request":{"subtype":"can_use_tool","tool_name":"Bash"}}`,
		`{"type":"assistant","uuid":"assistant","message":{"content":[{"type":"thinking","thinking":"PRIVATE"},{"type":"text","text":"answer"}]}}`,
		`{"type":"result","uuid":"result","user_message_uuid":"user","is_error":false}`,
	}, "\n")
	var sent bytes.Buffer
	c := claudeConversation{wire: chatwire.New(ctx, strings.NewReader(input), &sent), id: "session"}
	var events []agentruntime.ConversationEvent
	if err := c.Turn(ctx, agentruntime.ConversationInput{ID: "user", Text: "hello"}, func(e agentruntime.ConversationEvent) error { events = append(events, e); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 || events[0].Status != "blocked" || events[1].Text != "answer" || !events[2].Complete {
		t.Fatalf("events: %#v", events)
	}
	var msg chatwire.Object
	if err := json.Unmarshal(sent.Bytes(), &msg); err != nil {
		t.Fatal(err)
	}
	if chatwire.String(msg, "uuid") != "user" || strings.Contains(sent.String(), "control_response") {
		t.Fatal("permission was answered or user identity lost")
	}
	c.wire = chatwire.New(ctx, strings.NewReader(`{"type":"result","uuid":"result","user_message_uuid":"other"}`), &sent)
	if err := c.Turn(ctx, agentruntime.ConversationInput{ID: "user"}, func(agentruntime.ConversationEvent) error { return nil }); err == nil {
		t.Fatal("accepted unrelated turn completion")
	}
}
func TestConversationRejectsMissingResumeIdentity(t *testing.T) {
	a := New(DefaultOptions())
	if _, err := a.PrepareConversation(context.Background(), agentruntime.StartRequest{Resume: true}); err == nil {
		t.Fatal("chat must not open a resume picker")
	}
}

func TestReconcileFollowsUserUUIDLineage(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "projects", "fixture")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	records := `{"type":"user","uuid":"wanted","turnOrigin":"sdk"}
{"type":"assistant","uuid":"unrelated","parentUuid":"other","message":{"stop_reason":"end_turn","content":[{"type":"text","text":"wrong"}]}}
{"type":"attachment","uuid":"attachment","parentUuid":"wanted"}
{"type":"assistant","uuid":"right","parentUuid":"attachment","message":{"stop_reason":"end_turn","content":[{"type":"thinking","thinking":"PRIVATE"},{"type":"text","text":"right"}]}}
`
	if err := os.WriteFile(filepath.Join(dir, "session.jsonl"), []byte(records), 0600); err != nil {
		t.Fatal(err)
	}
	c := claudeConversation{id: "session", adapter: New(DefaultOptions()), configRoot: root}
	events, err := c.Reconcile(context.Background(), agentruntime.ConversationInput{ID: "wanted"}, "")
	if err != nil || len(events) != 2 || events[0].Text != "right" || !events[1].Complete {
		t.Fatalf("events=%#v err=%v", events, err)
	}
	events, err = c.Reconcile(context.Background(), agentruntime.ConversationInput{ID: "missing"}, "")
	if err != nil || len(events) != 0 {
		t.Fatalf("missing receipt accepted: %#v %v", events, err)
	}
}
