package codex

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/hiveryn/agentruntime"
	"github.com/hiveryn/agentruntime/internal/chatwire"
)

func TestConversationCompletesOnlyMatchingNativeTurn(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var sent bytes.Buffer
	input := strings.Join([]string{
		`{"id":1,"result":{"turn":{"id":"turn"}}}`,
		`{"method":"turn/completed","params":{"threadId":"thread","turn":{"id":"other","status":"completed"}}}`,
		`{"method":"item/completed","params":{"threadId":"thread","turnId":"turn","item":{"id":"thought","type":"reasoning","text":"PRIVATE"}}}`,
		`{"id":40,"method":"item/commandExecution/requestApproval","params":{"threadId":"thread","turnId":"turn"}}`,
		`{"method":"item/completed","params":{"threadId":"thread","turnId":"turn","item":{"id":"tool","type":"mcpToolCall","tool":"echo","server":"fixture","status":"failed","error":{"message":"original tool failure"}}}}`,
		`{"method":"turn/completed","params":{"threadId":"thread","turn":{"id":"turn","status":"completed"}}}`,
	}, "\n")
	c := codexConversation{wire: chatwire.New(ctx, strings.NewReader(input), &sent), id: "thread"}
	events := []agentruntime.ConversationEvent{}
	if err := c.Turn(ctx, agentruntime.ConversationInput{ID: "request", Text: "hi"}, func(e agentruntime.ConversationEvent) error { events = append(events, e); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(events) != 4 || events[1].Status != "blocked" || events[2].Status != "failed" || events[2].Tool != "fixture/echo" || events[2].Text != "original tool failure" || !events[3].Complete || events[3].Status != "completed" {
		t.Fatalf("events %#v", events)
	}
	if strings.Contains(sent.String(), "decision") {
		t.Fatal("permission answered")
	}
}
func TestConversationRejectsUnsupportedProfileFlags(t *testing.T) {
	a := New(DefaultOptions())
	if _, err := a.PrepareConversation(context.Background(), agentruntime.StartRequest{Args: []string{"--unknown-setting"}}); err == nil {
		t.Fatal("silently discarded profile argument")
	}
}
