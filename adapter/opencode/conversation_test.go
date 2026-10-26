package opencode

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hiveryn/agentruntime"
)

func TestConversation204DoesNotMeanCompletion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/event" {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(200)
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			return
		}
		if strings.HasSuffix(r.URL.Path, "prompt_async") {
			w.WriteHeader(204)
			return
		}
		if r.URL.Path == "/permission" {
			_ = json.NewEncoder(w).Encode([]any{map[string]any{"id": "p", "sessionID": "session", "permission": "bash"}})
			return
		}
		_ = json.NewEncoder(w).Encode([]any{map[string]any{"info": map[string]any{"parentID": "unrelated", "finish": "stop", "time": map[string]any{"completed": 1}}, "parts": []any{map[string]any{"type": "text", "id": "wrong", "text": "unrelated answer"}}}})
	}))
	defer server.Close()
	c := ocConversation{base: server.URL, id: "session", client: server.Client()}
	ctx, cancel := context.WithTimeout(context.Background(), 350*time.Millisecond)
	defer cancel()
	blocked := false
	err := c.Turn(ctx, agentruntime.ConversationInput{ID: "01234567-0123-0123-0123-012345678901", Text: "hello"}, func(e agentruntime.ConversationEvent) error {
		if e.Complete || e.Text == "unrelated answer" {
			t.Error("accepted unrelated completion")
		}
		if e.Status == "blocked" {
			blocked = true
		}
		return nil
	})
	if err == nil || !blocked {
		t.Fatalf("err=%v blocked=%v", err, blocked)
	}
}
