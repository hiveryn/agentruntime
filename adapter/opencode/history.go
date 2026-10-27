package opencode

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hiveryn/agentruntime"
	"github.com/hiveryn/agentruntime/internal/chatwire"
)

// ReadHistory reads a TUI-owned session from OpenCode's database, read-only
// (verified on 1.19.4). A user message is a turn: its id is the native turn id
// and, for input the caller posted through prompt_async, the caller's chosen
// message id (Ref). Assistant messages name it as parentID; the turn ends when
// an assistant message for it completes with a final finish ("stop" or
// "length") or an error. Reasoning, step markers, snapshots and patches are
// excluded; synthetic text parts (file/context injection) are skipped.
func (a *Adapter) ReadHistory(_ context.Context, req agentruntime.HistoryRequest) (agentruntime.History, error) {
	dbPath, sessionID := "", req.NativeSessionID
	if req.TranscriptHandle != "" {
		var ok bool
		dbPath, sessionID, ok = strings.Cut(req.TranscriptHandle, pathSep)
		if !ok || sessionID == "" {
			return agentruntime.History{}, fmt.Errorf("opencode: read history: malformed transcript handle %q", req.TranscriptHandle)
		}
	} else {
		dataDir, err := opencodeDataDir(req.ConfigRoot)
		if err != nil {
			return agentruntime.History{}, fmt.Errorf("opencode: read history: resolve data dir: %w", err)
		}
		if xdg := req.Env["XDG_DATA_HOME"]; xdg != "" && req.ConfigRoot == "" {
			dataDir = filepath.Join(xdg, "opencode")
		}
		dbPath = filepath.Join(dataDir, "opencode.db")
	}
	if sessionID == "" {
		return agentruntime.History{}, fmt.Errorf("opencode: read history: session id is required")
	}
	if _, err := os.Stat(dbPath); err != nil {
		return agentruntime.History{}, fmt.Errorf("opencode: read history: %w", err)
	}
	db, err := openReadOnly(dbPath)
	if err != nil {
		return agentruntime.History{}, fmt.Errorf("opencode: read history: open db: %w", err)
	}
	defer func() { _ = db.Close() }()
	rows, err := db.Query(`SELECT m.id, m.time_created, m.data, COALESCE(p.id,''), COALESCE(p.data,'')
		FROM message m LEFT JOIN part p ON p.message_id = m.id
		WHERE m.session_id = ? ORDER BY m.time_created, m.id, p.time_created, p.id`, sessionID)
	if err != nil {
		return agentruntime.History{}, fmt.Errorf("opencode: read history: query: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var history agentruntime.History
	ended := map[string]bool{}
	lastMessage := ""
	for rows.Next() {
		var id, partID string
		var created int64
		var rawMessage, rawPart []byte
		if err := rows.Scan(&id, &created, &rawMessage, &partID, &rawPart); err != nil {
			return agentruntime.History{}, fmt.Errorf("opencode: read history: scan: %w", err)
		}
		at := time.UnixMilli(created).UTC()
		if created > 0 && at.After(history.RecordedThrough) {
			history.RecordedThrough = at
		}
		var info chatwire.Object
		if json.Unmarshal(rawMessage, &info) != nil {
			continue
		}
		role := chatwire.String(info, "role")
		turn := id
		if role == "assistant" {
			turn = chatwire.String(info, "parentID")
		}
		if partID != "" {
			var part chatwire.Object
			if json.Unmarshal(rawPart, &part) == nil {
				if item, ok := historyPart(part, partID, role, turn, at); ok {
					if role == "user" {
						item.ID, item.Ref, item.NativeOrigin = id, id, "prompt"
						if n := len(history.Items); n > 0 && history.Items[n-1].ID == id {
							history.Items[n-1].Text += "\n" + item.Text
							continue
						}
					}
					history.Items = append(history.Items, item)
				}
			}
		}
		if id == lastMessage || role != "assistant" || ended[turn] {
			lastMessage = id
			continue
		}
		lastMessage = id
		completed := chatwire.Map(info, "time")["completed"] != nil
		finish := chatwire.String(info, "finish")
		switch {
		case info["error"] != nil:
			data, _ := json.Marshal(info["error"])
			status := "failed"
			if strings.Contains(string(data), "MessageAbortedError") {
				status = "interrupted"
			}
			history.Items = append(history.Items, agentruntime.HistoryItem{ID: "turn:" + turn, TurnID: turn, Role: "system", Status: status, Text: string(data), TurnEnd: true, At: at})
			ended[turn] = true
		case completed && (finish == "stop" || finish == "length"):
			history.Items = append(history.Items, agentruntime.HistoryItem{ID: "turn:" + turn, TurnID: turn, Role: "system", Status: "completed", TurnEnd: true, At: at})
			ended[turn] = true
		}
	}
	if err := rows.Err(); err != nil {
		return agentruntime.History{}, fmt.Errorf("opencode: read history: rows: %w", err)
	}
	return history, nil
}

func historyPart(part chatwire.Object, id, role, turn string, at time.Time) (agentruntime.HistoryItem, bool) {
	item := agentruntime.HistoryItem{ID: id, TurnID: turn, Role: role, Status: "completed", At: at}
	switch chatwire.String(part, "type") {
	case "text":
		if part["synthetic"] == true || part["ignored"] == true {
			return item, false
		}
		item.Text = chatwire.String(part, "text")
		return item, strings.TrimSpace(item.Text) != ""
	case "tool":
		state := chatwire.Map(part, "state")
		item.Role = "tool"
		item.Tool = chatwire.String(part, "tool")
		item.Status = chatwire.String(state, "status")
		item.Text = chatwire.String(state, "output")
		if item.Status == "error" {
			item.Status = "failed"
			item.Text = chatwire.String(state, "error")
		}
		return item, true
	}
	return item, false
}
