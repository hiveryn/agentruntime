package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/hiveryn/agentruntime"
	"github.com/hiveryn/agentruntime/internal/chatwire"
)

// ReadHistory normalizes every rollout segment of a TUI-owned thread (verified
// on 0.164.0). Visible items come from event_msg item_completed records:
// UserMessage (with the caller's client_id, the receipt of a queued input),
// AgentMessage and tool items. task_started carries the native turn trigger
// ("user" typed in the TUI, "queue" for the durable thread queue); a turn ends
// with task_complete or turn_aborted. Developer instructions, injected
// AGENTS.md/environment context and reasoning are response_item records and
// are never read.
func (a *Adapter) ReadHistory(ctx context.Context, req agentruntime.HistoryRequest) (agentruntime.History, error) {
	var paths []string
	if req.NativeSessionID != "" {
		segments, err := a.LocateRolloutSegments(ctx, agentruntime.LocateRequest{NativeSessionID: req.NativeSessionID, ConfigRoot: req.ConfigRoot})
		if err != nil {
			return agentruntime.History{}, fmt.Errorf("codex: read history: %w", err)
		}
		for _, segment := range segments {
			paths = append(paths, segment.Path)
		}
	}
	if len(paths) == 0 && req.TranscriptHandle != "" {
		paths = []string{req.TranscriptHandle}
	}
	if len(paths) == 0 {
		return agentruntime.History{}, fmt.Errorf("codex: read history: no rollout recorded yet for thread %q", req.NativeSessionID)
	}
	h := &historyReader{index: map[string]int{}, triggers: map[string]string{}, started: map[string]bool{}}
	for _, path := range paths {
		if err := h.read(path); err != nil {
			return agentruntime.History{}, err
		}
	}
	return h.history, nil
}

type historyReader struct {
	history  agentruntime.History
	index    map[string]int
	triggers map[string]string
	started  map[string]bool
}

func (h *historyReader) put(item agentruntime.HistoryItem) {
	if i, ok := h.index[item.ID]; ok {
		if item.At.IsZero() {
			item.At = h.history.Items[i].At
		}
		h.history.Items[i] = item
		return
	}
	h.index[item.ID] = len(h.history.Items)
	h.history.Items = append(h.history.Items, item)
}

func (h *historyReader) read(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("codex: open rollout: %w", err)
	}
	defer func() { _ = file.Close() }()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 1024*1024), 64*1024*1024)
	for scanner.Scan() {
		var rec struct {
			Timestamp string          `json:"timestamp"`
			Type      string          `json:"type"`
			Payload   chatwire.Object `json:"payload"`
		}
		if json.Unmarshal(scanner.Bytes(), &rec) != nil {
			continue // a torn final line is read again on the next pass
		}
		at := parseRolloutTime(rec.Timestamp)
		if at.After(h.history.RecordedThrough) {
			h.history.RecordedThrough = at
		}
		if rec.Type != "event_msg" {
			continue
		}
		p := rec.Payload
		turn := chatwire.String(p, "turn_id")
		switch chatwire.String(p, "type") {
		case "task_started":
			if trigger := chatwire.String(chatwire.Map(p, "turn_attribution"), "turn_trigger"); turn != "" && trigger != "" {
				h.triggers[turn] = trigger
			}
		case "task_complete":
			h.put(agentruntime.HistoryItem{ID: "turn:" + turn, TurnID: turn, Role: "system", Status: "completed", TurnEnd: true, At: at})
		case "turn_aborted":
			h.put(agentruntime.HistoryItem{ID: "turn:" + turn, TurnID: turn, Role: "system", Status: "interrupted", Text: chatwire.String(p, "reason"), TurnEnd: true, At: at})
		case "item_completed":
			h.item(chatwire.Map(p, "item"), turn, at)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("codex: scan rollout %s: %w", path, err)
	}
	return nil
}

// item maps one completed rollout item. Item types are PascalCase with
// snake_case fields in the rollout (app-server notifications use camelCase).
func (h *historyReader) item(item chatwire.Object, turn string, at time.Time) {
	id := chatwire.String(item, "id")
	if id == "" {
		return
	}
	switch typ := chatwire.String(item, "type"); typ {
	case "UserMessage":
		var text strings.Builder
		for _, v := range chatwire.List(item, "content") {
			if part, ok := v.(map[string]any); ok && strings.EqualFold(chatwire.String(part, "type"), "text") {
				text.WriteString(chatwire.String(part, "text"))
			}
		}
		// The trigger describes how the turn started: it belongs to the turn's
		// first input only. Input steered into a running turn has no native label.
		origin := h.triggers[turn]
		if h.started[turn] {
			origin = ""
		}
		h.started[turn] = true
		h.put(agentruntime.HistoryItem{ID: id, TurnID: turn, Role: "user", NativeOrigin: origin, Ref: chatwire.String(item, "client_id"), Status: "completed", Text: text.String(), At: at})
	case "AgentMessage":
		var text strings.Builder
		for _, v := range chatwire.List(item, "content") {
			if part, ok := v.(map[string]any); ok && strings.EqualFold(chatwire.String(part, "type"), "text") {
				text.WriteString(chatwire.String(part, "text"))
			}
		}
		if strings.TrimSpace(text.String()) != "" {
			h.put(agentruntime.HistoryItem{ID: id, TurnID: turn, Role: "assistant", Status: "completed", Text: text.String(), At: at})
		}
	case "Reasoning", "ContextCompaction", "Plan", "":
		return
	default:
		tool := typ
		if name := chatwire.String(item, "tool"); name != "" {
			tool = name
			if server := chatwire.String(item, "server"); server != "" {
				tool = server + "/" + name
			}
		}
		text := chatwire.String(item, "aggregated_output")
		if text == "" {
			text = chatwire.TextContent(chatwire.Map(item, "result")["content"])
		}
		if message := chatwire.String(chatwire.Map(item, "error"), "message"); message != "" {
			text = message
		}
		status := strings.ToLower(chatwire.String(item, "status"))
		switch status {
		case "", "completed", "success":
			status = "completed"
		case "failed", "error", "declined":
			status = "failed"
		}
		h.put(agentruntime.HistoryItem{ID: id, TurnID: turn, Role: "tool", Tool: tool, Status: status, Text: text, At: at})
	}
}
