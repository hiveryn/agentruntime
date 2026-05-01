package ingest

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hiveryn/agentruntime"
	"github.com/hiveryn/agentruntime/adapter/claude"
	"github.com/hiveryn/agentruntime/adapter/codex"
)

func TestHandlerAcceptsCapturedCodexHook(t *testing.T) {
	receiver := NewReceiver(codex.New(codex.DefaultOptions()))
	sub := receiver.Hub().Subscribe(Filter{ID: "hiveryn-lab-homehooks-1"})
	defer sub.Close()

	req := httptest.NewRequest(http.MethodPost, "/hook", bytes.NewReader(readFixture(t, "pretooluse_1.json")))
	rec := httptest.NewRecorder()

	receiver.Handler(agentruntime.AgentCodex).ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body.String())
	}
	select {
	case event := <-sub.Events:
		if event.Tool != "Bash" || event.NativeID == "" {
			t.Fatalf("got %+v", event)
		}
	default:
		t.Fatal("expected published event")
	}
}

func TestHandlerRejectsBadJSON(t *testing.T) {
	receiver := NewReceiver(codex.New(codex.DefaultOptions()))
	req := httptest.NewRequest(http.MethodPost, "/hook", bytes.NewReader([]byte(`{"hook":`)))
	rec := httptest.NewRecorder()

	receiver.Handler(agentruntime.AgentCodex).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandlerRejectsWrongMethod(t *testing.T) {
	receiver := NewReceiver(codex.New(codex.DefaultOptions()))
	req := httptest.NewRequest(http.MethodGet, "/hook", nil)
	rec := httptest.NewRecorder()

	receiver.Handler(agentruntime.AgentCodex).ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandlerAcceptsCapturedClaudeHook(t *testing.T) {
	receiver := NewReceiver(claude.New(claude.DefaultOptions()))
	sub := receiver.Hub().Subscribe(Filter{ID: "hiv-claude-lab-1"})
	defer sub.Close()

	req := httptest.NewRequest(http.MethodPost, "/hook", bytes.NewReader(readClaudeFixture(t, "pretooluse_subagent_1.json")))
	rec := httptest.NewRecorder()

	receiver.Handler(agentruntime.AgentClaude).ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body.String())
	}
	select {
	case event := <-sub.Events:
		if event.Tool != "Read" || event.NativeSessionRole != agentruntime.NativeSessionRoleSubsession {
			t.Fatalf("got %+v", event)
		}
	default:
		t.Fatal("expected published event")
	}
}
