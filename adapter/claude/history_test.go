package claude

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hiveryn/agentruntime"
)

// Shapes recorded from an isolated Claude Code 2.1.311 TUI session.
const historyFixture = `{"type":"permission-mode","permissionMode":"bypassPermissions"}
{"type":"user","uuid":"u1","promptId":"p1","turnOrigin":"human","timestamp":"2026-10-27T02:06:12Z","message":{"role":"user","content":"# Project: lab\n\nKICKOFF"}}
{"type":"attachment","uuid":"a1","attachment":{"type":"environment"}}
{"type":"assistant","uuid":"x1","timestamp":"2026-10-27T02:06:14Z","message":{"content":[{"type":"thinking","thinking":"hidden"},{"type":"text","text":"Oriented."}]}}
{"type":"system","subtype":"stop_hook_summary","uuid":"s0"}
{"type":"system","subtype":"turn_duration","uuid":"s1","timestamp":"2026-10-27T02:06:15Z"}
{"type":"user","uuid":"u2","promptId":"p2","turnOrigin":"human","timestamp":"2026-10-27T02:07:00Z","message":{"role":"user","content":"remember ORCHID-311"}}
{"type":"assistant","uuid":"x2","message":{"content":[{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"echo ok"}}]}}
{"type":"user","uuid":"u3","promptId":"p2","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"ok"}]}}
{"type":"assistant","uuid":"x3","message":{"content":[{"type":"text","text":"LAPTOP-ACK"}]}}
{"type":"assistant","uuid":"side","isSidechain":true,"message":{"content":[{"type":"text","text":"subagent"}]}}
{"type":"system","subtype":"turn_duration","uuid":"s2","timestamp":"2026-10-27T02:07:05Z"}
{"type":"user","uuid":"u4","promptId":"p3","turnOrigin":"task_notification","timestamp":"2026-10-27T02:08:00Z","message":{"role":"user","content":"<task-notification>\n<summary>x</summary>\n</task-notification>\n<system-reminder>\nHiveryn chat sub-1: hello from phone\n</system-reminder>"}}
{"type":"user","uuid":"u5","isMeta":true,"promptId":"p3","turnOrigin":"human","message":{"role":"user","content":"meta caveat"}}
{"type":"assistant","uuid":"x4","message":{"content":[{"type":"text","text":"PHONE-ACK"}]}}
{"type":"user","uuid":"u6","promptId":"p3","message":{"role":"user","content":[{"type":"text","text":"[Request interrupted by user]"}]}}
{"type":"user","uuid":"u7","promptId":"p4","turnOrigin":"human","message":{"role":"user","content":"<command-name>/model</command-name>"}}
{"type":"user","uuid":"torn"`

func TestReadHistoryNormalizesTUITranscript(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(path, []byte(historyFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := New(Options{}).ReadHistory(context.Background(), agentruntime.HistoryRequest{TranscriptHandle: path})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, item := range h.Items {
		got = append(got, strings.Join([]string{item.ID, item.TurnID, item.Role, item.NativeOrigin, item.Tool, item.Status, item.Text}, "|"))
	}
	want := []string{
		"u1|p1|user|human||completed|# Project: lab\n\nKICKOFF",
		"x1:1|p1|assistant|||completed|Oriented.",
		"s1|p1|system|||completed|",
		"u2|p2|user|human||completed|remember ORCHID-311",
		"toolu_1|p2|tool||Bash|completed|ok",
		"x3:0|p2|assistant|||completed|LAPTOP-ACK",
		"s2|p2|system|||completed|",
		"u4|p3|user|task_notification||completed|Hiveryn chat sub-1: hello from phone",
		"x4:0|p3|assistant|||completed|PHONE-ACK",
		"u6:0|p3|system|||interrupted|Request interrupted by user",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("items:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if !h.Items[2].TurnEnd || !h.Items[9].TurnEnd || h.Items[4].TurnEnd {
		t.Fatalf("turn ends wrong: %+v", h.Items)
	}
	if h.RecordedThrough.IsZero() {
		t.Fatal("recorded-through time missing")
	}
}

func TestReadHistoryRequiresReadableTranscript(t *testing.T) {
	if _, err := New(Options{}).ReadHistory(context.Background(), agentruntime.HistoryRequest{TranscriptHandle: filepath.Join(t.TempDir(), "missing.jsonl")}); err == nil {
		t.Fatal("missing transcript must be an error, never empty history")
	}
}
