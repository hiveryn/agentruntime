package claude

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

// ReadHistory normalizes a TUI session's transcript (verified on 2.1.311).
// A user input carries turnOrigin ("human" typed in the TUI,
// "task_notification" for an asyncRewake wake-up) and a promptId shared by the
// tool results of its turn; a system turn_duration entry ends the turn.
// Sidechains (subagents), meta entries, attachments other than queued input,
// hook summaries, local slash-command plumbing and thinking are excluded. An
// asyncRewake input is unwrapped to the hook's text but keeps its native
// task_notification provenance.
func (a *Adapter) ReadHistory(_ context.Context, req agentruntime.HistoryRequest) (agentruntime.History, error) {
	if req.TranscriptHandle == "" {
		return agentruntime.History{}, fmt.Errorf("claude: read history: transcript handle is required")
	}
	file, err := os.Open(req.TranscriptHandle)
	if err != nil {
		return agentruntime.History{}, fmt.Errorf("claude: open transcript: %w", err)
	}
	defer func() { _ = file.Close() }()
	var (
		history agentruntime.History
		index   = map[string]int{}
		turn    string
	)
	put := func(item agentruntime.HistoryItem) {
		if i, ok := index[item.ID]; ok {
			previous := history.Items[i]
			if item.Tool == "" {
				item.Tool = previous.Tool
			}
			if item.At.IsZero() {
				item.At = previous.At
			}
			history.Items[i] = item
			return
		}
		index[item.ID] = len(history.Items)
		history.Items = append(history.Items, item)
	}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 1024*1024), 64*1024*1024)
	for scanner.Scan() {
		var m chatwire.Object
		if json.Unmarshal(scanner.Bytes(), &m) != nil {
			continue // a torn final line is read again on the next pass
		}
		at, _ := time.Parse(time.RFC3339Nano, chatwire.String(m, "timestamp"))
		if at.After(history.RecordedThrough) {
			history.RecordedThrough = at
		}
		if m["isSidechain"] == true || m["isMeta"] == true {
			continue
		}
		id := chatwire.String(m, "uuid")
		switch chatwire.String(m, "type") {
		case "user":
			msg := chatwire.Map(m, "message")
			if prompt := chatwire.String(m, "promptId"); prompt != "" && chatwire.String(m, "turnOrigin") != "" {
				turn = prompt
			}
			if text, ok := msg["content"].(string); ok {
				readUserText(put, m, id, turn, text, at)
				continue
			}
			for i, v := range chatwire.List(msg, "content") {
				part, ok := v.(map[string]any)
				if !ok {
					continue
				}
				switch chatwire.String(part, "type") {
				case "text":
					readUserText(put, m, fmt.Sprintf("%s:%d", id, i), turn, chatwire.String(part, "text"), at)
				case "tool_result":
					status := "completed"
					if part["is_error"] == true {
						status = "failed"
					}
					put(agentruntime.HistoryItem{ID: chatwire.String(part, "tool_use_id"), TurnID: turn, Role: "tool", Status: status, Text: chatwire.TextContent(part["content"])})
				}
			}
		case "assistant":
			msg := chatwire.Map(m, "message")
			for i, v := range chatwire.List(msg, "content") {
				part, ok := v.(map[string]any)
				if !ok {
					continue
				}
				switch chatwire.String(part, "type") {
				case "text":
					if text := chatwire.String(part, "text"); strings.TrimSpace(text) != "" {
						put(agentruntime.HistoryItem{ID: fmt.Sprintf("%s:%d", id, i), TurnID: turn, Role: "assistant", Status: "completed", Text: text, At: at})
					}
				case "tool_use":
					put(agentruntime.HistoryItem{ID: chatwire.String(part, "id"), TurnID: turn, Role: "tool", Tool: chatwire.String(part, "name"), Status: "started", At: at})
				}
			}
		case "attachment":
			// Input typed while a turn runs is recorded as a queued command.
			attachment := chatwire.Map(m, "attachment")
			if chatwire.String(attachment, "type") == "queued_command" {
				text := chatwire.String(attachment, "prompt")
				if text == "" {
					text = chatwire.TextContent(attachment["content"])
				}
				if strings.TrimSpace(text) != "" {
					put(agentruntime.HistoryItem{ID: id, TurnID: turn, Role: "user", NativeOrigin: "queued_command", Status: "completed", Text: text, At: at})
				}
			}
		case "system":
			if chatwire.String(m, "subtype") == "turn_duration" && turn != "" {
				put(agentruntime.HistoryItem{ID: id, TurnID: turn, Role: "system", Status: "completed", TurnEnd: true, At: at})
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return agentruntime.History{}, fmt.Errorf("claude: scan transcript: %w", err)
	}
	return history, nil
}

const interruptedMarker = "[Request interrupted by user"

func readUserText(put func(agentruntime.HistoryItem), m chatwire.Object, id, turn, text string, at time.Time) {
	origin := chatwire.String(m, "turnOrigin")
	switch {
	case strings.TrimSpace(text) == "":
		return
	case strings.HasPrefix(text, interruptedMarker):
		if turn != "" {
			put(agentruntime.HistoryItem{ID: id, TurnID: turn, Role: "system", Status: "interrupted", Text: strings.Trim(text, "[]"), TurnEnd: true, At: at})
		}
		return
	case strings.HasPrefix(text, "<local-command-") || strings.HasPrefix(text, "<command-name>") || strings.HasPrefix(text, "<command-message>"):
		return
	case origin == "":
		return // tool-result envelopes and provider-inserted text without a turn origin
	case origin == "task_notification":
		text = unwrapRewake(text)
	}
	put(agentruntime.HistoryItem{ID: id, TurnID: turn, Role: "user", NativeOrigin: origin, Status: "completed", Text: text, At: at})
}

// unwrapRewake returns the hook text of an asyncRewake input: the body of its
// system reminder after the task-notification header.
func unwrapRewake(text string) string {
	const open, closing = "<system-reminder>", "</system-reminder>"
	start := strings.Index(text, open)
	if start < 0 {
		return text
	}
	body := text[start+len(open):]
	if end := strings.LastIndex(body, closing); end >= 0 {
		body = body[:end]
	}
	return strings.TrimSpace(body)
}
