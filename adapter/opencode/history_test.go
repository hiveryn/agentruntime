package opencode

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hiveryn/agentruntime"
)

// Shapes recorded from an isolated OpenCode 1.19.4 TUI session.
func TestReadHistoryFromDatabaseWithParentTurns(t *testing.T) {
	data := filepath.Join(t.TempDir(), "xdg-data")
	if err := os.MkdirAll(filepath.Join(data, "opencode"), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(data, "opencode", "opencode.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	for _, stmt := range []string{
		`CREATE TABLE message (id TEXT PRIMARY KEY, session_id TEXT NOT NULL, time_created INTEGER NOT NULL, time_updated INTEGER NOT NULL, data TEXT NOT NULL)`,
		`CREATE TABLE part (id TEXT PRIMARY KEY, message_id TEXT NOT NULL, session_id TEXT NOT NULL, time_created INTEGER NOT NULL, time_updated INTEGER NOT NULL, data TEXT NOT NULL)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	msg := func(id, session string, at int64, data map[string]any) {
		raw, _ := json.Marshal(data)
		if _, err := db.Exec(`INSERT INTO message VALUES (?,?,?,?,?)`, id, session, at, at, string(raw)); err != nil {
			t.Fatal(err)
		}
	}
	part := func(id, message string, at int64, data map[string]any) {
		raw, _ := json.Marshal(data)
		if _, err := db.Exec(`INSERT INTO part VALUES (?,?,?,?,?,?)`, id, message, "ses_a", at, at, string(raw)); err != nil {
			t.Fatal(err)
		}
	}
	msg("msg_01", "ses_a", 1000, map[string]any{"role": "user"})
	part("prt_01", "msg_01", 1001, map[string]any{"type": "text", "text": "KICKOFF"})
	part("prt_02", "msg_01", 1002, map[string]any{"type": "text", "text": "file contents", "synthetic": true})
	msg("msg_02", "ses_a", 1003, map[string]any{"role": "assistant", "parentID": "msg_01", "finish": "tool-calls", "time": map[string]any{"completed": 1005}})
	part("prt_03", "msg_02", 1004, map[string]any{"type": "step-start"})
	part("prt_04", "msg_02", 1004, map[string]any{"type": "reasoning", "text": "hidden"})
	part("prt_05", "msg_02", 1005, map[string]any{"type": "tool", "tool": "bash", "state": map[string]any{"status": "completed", "output": "ok"}})
	msg("msg_03", "ses_a", 1006, map[string]any{"role": "assistant", "parentID": "msg_01", "finish": "stop", "time": map[string]any{"completed": 1008}})
	part("prt_06", "msg_03", 1007, map[string]any{"type": "text", "text": "READY"})
	msg("msg_04", "ses_a", 2000, map[string]any{"role": "user"})
	part("prt_07", "msg_04", 2001, map[string]any{"type": "text", "text": "from phone"})
	msg("msg_05", "ses_a", 2002, map[string]any{"role": "assistant", "parentID": "msg_04", "error": map[string]any{"name": "MessageAbortedError"}})
	msg("msg_zz", "ses_other", 3000, map[string]any{"role": "user"})
	h, err := New(Options{}).ReadHistory(context.Background(), agentruntime.HistoryRequest{NativeSessionID: "ses_a", Env: map[string]string{"XDG_DATA_HOME": data}})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, item := range h.Items {
		got = append(got, strings.Join([]string{item.ID, item.TurnID, item.Role, item.NativeOrigin, item.Ref, item.Tool, item.Status, item.Text}, "|"))
	}
	want := []string{
		"msg_01|msg_01|user|prompt|msg_01||completed|KICKOFF",
		"prt_05|msg_01|tool|||bash|completed|ok",
		"prt_06|msg_01|assistant||||completed|READY",
		"turn:msg_01|msg_01|system||||completed|",
		"msg_04|msg_04|user|prompt|msg_04||completed|from phone",
		`turn:msg_04|msg_04|system||||interrupted|{"name":"MessageAbortedError"}`,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("items:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestReadHistoryMissingDatabaseIsAnError(t *testing.T) {
	if _, err := New(Options{}).ReadHistory(context.Background(), agentruntime.HistoryRequest{NativeSessionID: "ses_a", Env: map[string]string{"XDG_DATA_HOME": t.TempDir()}}); err == nil {
		t.Fatal("missing database must be an error")
	}
}
