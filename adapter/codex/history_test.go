package codex

import (
	"context"
	"strings"
	"testing"

	"github.com/hiveryn/agentruntime"
)

// Shapes recorded from isolated Codex 0.164.0 TUI rollouts.
func TestReadHistoryAcrossSegmentsWithTriggersAndClientIDs(t *testing.T) {
	root := t.TempDir()
	ev := func(ts, payload string) string {
		return `{"timestamp":"2026-10-27T02:0` + ts + `Z","type":"event_msg","payload":` + payload + `}`
	}
	writeSegment(t, root, "2026/10/27", "2026-10-27T02-00-00", threadA, "", "",
		ev("0:01", `{"type":"task_started","turn_id":"t1","turn_attribution":{"turn_id":"t1","turn_trigger":"user"}}`),
		`{"timestamp":"2026-10-27T02:00:01Z","type":"response_item","payload":{"type":"message","role":"developer","content":[{"type":"input_text","text":"SYSTEM INSTRUCTIONS"}]}}`,
		`{"timestamp":"2026-10-27T02:00:01Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"# AGENTS.md instructions"}]}}`,
		ev("0:02", `{"type":"item_completed","turn_id":"t1","item":{"type":"UserMessage","id":"um1","client_id":"c-laptop","content":[{"type":"text","text":"KICKOFF","text_elements":[]}]}}`),
		ev("0:03", `{"type":"item_completed","turn_id":"t1","item":{"type":"Reasoning","id":"r1","summary":["secret"]}}`),
		ev("0:04", `{"type":"item_completed","turn_id":"t1","item":{"type":"CommandExecution","id":"exec-1","command":["/bin/zsh","-lc","echo ok"],"status":"completed","aggregated_output":"ok\n","exit_code":0}}`),
		ev("0:05", `{"type":"item_completed","turn_id":"t1","item":{"type":"AgentMessage","id":"am1","content":[{"type":"Text","text":"READY"}],"phase":"final_answer"}}`),
		ev("0:06", `{"type":"task_complete","turn_id":"t1","last_agent_message":"READY"}`),
	)
	writeSegment(t, root, "2026/10/27", "2026-10-27T02-05-00", threadA, segA1, "",
		ev("5:01", `{"type":"task_started","turn_id":"t2","turn_attribution":{"turn_id":"t2","turn_trigger":"queue"}}`),
		ev("5:02", `{"type":"item_completed","turn_id":"t2","item":{"type":"UserMessage","id":"um2","client_id":"sub-1","content":[{"type":"text","text":"from phone"}]}}`),
		ev("5:02", `{"type":"item_completed","turn_id":"t2","item":{"type":"UserMessage","id":"um3","client_id":"tui-x","content":[{"type":"text","text":"steered from laptop"}]}}`),
		ev("5:03", `{"type":"item_completed","turn_id":"t2","item":{"type":"McpToolCall","id":"mcp-1","server":"hiveryn-daemon","tool":"notify","status":"failed","error":{"message":"not configured"}}}`),
		ev("5:04", `{"type":"turn_aborted","turn_id":"t2","reason":"interrupted"}`),
		`{"type":"event_msg","payload":`,
	)
	h, err := New(Options{}).ReadHistory(context.Background(), agentruntime.HistoryRequest{NativeSessionID: threadA, ConfigRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, item := range h.Items {
		got = append(got, strings.Join([]string{item.ID, item.TurnID, item.Role, item.NativeOrigin, item.Ref, item.Tool, item.Status, strings.TrimSpace(item.Text)}, "|"))
	}
	want := []string{
		"um1|t1|user|user|c-laptop||completed|KICKOFF",
		"exec-1|t1|tool|||CommandExecution|completed|ok",
		"am1|t1|assistant||||completed|READY",
		"turn:t1|t1|system||||completed|",
		"um2|t2|user|queue|sub-1||completed|from phone",
		"um3|t2|user||tui-x||completed|steered from laptop",
		"mcp-1|t2|tool|||hiveryn-daemon/notify|failed|not configured",
		"turn:t2|t2|system||||interrupted|interrupted",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("items:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if !h.Items[3].TurnEnd || !h.Items[7].TurnEnd {
		t.Fatal("turn ends missing")
	}
}

func TestReadHistoryWithoutRolloutIsAnError(t *testing.T) {
	if _, err := New(Options{}).ReadHistory(context.Background(), agentruntime.HistoryRequest{NativeSessionID: threadA, ConfigRoot: t.TempDir()}); err == nil {
		t.Fatal("an unrecorded thread must be an error, never empty history")
	}
}
