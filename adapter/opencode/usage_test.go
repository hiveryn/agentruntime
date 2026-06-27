package opencode

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/hiveryn/agentruntime"

	_ "modernc.org/sqlite"
)

// newTestDB creates an opencode.db at dir/opencode.db with the minimal session
// and message schema usage accounting reads, and returns the db path.
func newTestDB(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dir, "opencode.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	stmts := []string{
		`CREATE TABLE session (id TEXT PRIMARY KEY)`,
		`CREATE TABLE message (id TEXT PRIMARY KEY, session_id TEXT NOT NULL, time_created INTEGER NOT NULL, data TEXT NOT NULL)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("exec %q: %v", s, err)
		}
	}
	return dbPath
}

func insertSession(t *testing.T, dbPath, sessionID string) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO session (id) VALUES (?)`, sessionID); err != nil {
		t.Fatal(err)
	}
}

// insertMessage writes one message row. tokens are [input, output, reasoning,
// cacheRead, cacheWrite]; modelID is recorded top-level (opencode 1.17.x shape).
func insertMessage(t *testing.T, dbPath, id, sessionID, role string, order int, modelID string, tokens [5]int) {
	t.Helper()
	data := map[string]any{
		"role":       role,
		"providerID": "xai",
		"modelID":    modelID, // opencode 1.17.x top-level shape
		"tokens": map[string]any{
			"input":     tokens[0],
			"output":    tokens[1],
			"reasoning": tokens[2],
			"cache":     map[string]any{"read": tokens[3], "write": tokens[4]},
		},
	}
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(
		`INSERT INTO message (id, session_id, time_created, data) VALUES (?, ?, ?, ?)`,
		id, sessionID, order, string(raw),
	); err != nil {
		t.Fatal(err)
	}
}

func TestParseUsage(t *testing.T) {
	dir := t.TempDir()
	dbPath := newTestDB(t, dir)
	const sid = "ses_test1"
	insertSession(t, dbPath, sid)

	// Two assistant turns are SUMMED; the user message is ignored. Reasoning folds
	// into OutputTokens; cache stays separate from InputTokens (exclusive mapping).
	insertMessage(t, dbPath, "m1", sid, "user", 1, "", [5]int{0, 0, 0, 0, 0})
	insertMessage(t, dbPath, "m2", sid, "assistant", 2, "grok-4.3", [5]int{100, 10, 5, 200, 3})
	insertMessage(t, dbPath, "m3", sid, "assistant", 3, "grok-4.4", [5]int{50, 20, 15, 300, 0})
	// A different session's rows must not bleed into the total.
	insertSession(t, dbPath, "ses_other")
	insertMessage(t, dbPath, "x1", "ses_other", "assistant", 9, "gpt-5.4", [5]int{999, 999, 999, 999, 999})

	adapter := New(DefaultOptions())
	usage, err := adapter.ParseUsage(context.Background(), dbPath+pathSep+sid)
	if err != nil {
		t.Fatal(err)
	}
	want := agentruntime.Usage{
		InputTokens:      150,        // 100 + 50 (already excludes cache)
		OutputTokens:     50,         // (10+5) + (20+15), reasoning folded in
		CacheWriteTokens: 3,          // 3 + 0
		CacheReadTokens:  500,        // 200 + 300
		Model:            "grok-4.4", // last assistant message wins
		Messages:         2,
	}
	if usage != want {
		t.Fatalf("usage = %+v, want %+v", usage, want)
	}
}

// TestParseUsageNestedModel covers older opencode versions that nested the model
// under "model" rather than recording modelID at the top level.
func TestParseUsageNestedModel(t *testing.T) {
	dir := t.TempDir()
	dbPath := newTestDB(t, dir)
	const sid = "ses_nested"
	insertSession(t, dbPath, sid)

	data := `{"role":"assistant","model":{"providerID":"xai","modelID":"grok-legacy"},"tokens":{"input":7,"output":3,"reasoning":2,"cache":{"read":1,"write":0}}}`
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO message (id, session_id, time_created, data) VALUES (?,?,?,?)`, "n1", sid, 1, data); err != nil {
		t.Fatal(err)
	}
	db.Close()

	usage, err := New(DefaultOptions()).ParseUsage(context.Background(), dbPath+pathSep+sid)
	if err != nil {
		t.Fatal(err)
	}
	want := agentruntime.Usage{InputTokens: 7, OutputTokens: 5, CacheReadTokens: 1, Model: "grok-legacy", Messages: 1}
	if usage != want {
		t.Fatalf("usage = %+v, want %+v", usage, want)
	}
}

func TestParseUsageMalformedHandle(t *testing.T) {
	adapter := New(DefaultOptions())
	if _, err := adapter.ParseUsage(context.Background(), "/tmp/opencode.db"); err == nil {
		t.Fatal("expected error for handle without ::sessionID")
	}
}

func TestLocateTranscript(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, ".local", "share", "opencode")
	dbPath := newTestDB(t, dataDir)
	const sid = "ses_locate"
	insertSession(t, dbPath, sid)

	adapter := New(DefaultOptions())
	got, err := adapter.LocateTranscript(context.Background(), agentruntime.LocateRequest{
		NativeSessionID: sid,
		ConfigRoot:      root, // HOME-base override → <root>/.local/share/opencode
		Workdir:         "/Users/dev/proj",
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := dbPath + pathSep + sid; got != want {
		t.Fatalf("handle = %q, want %q", got, want)
	}
}

func TestLocateTranscriptEmptyID(t *testing.T) {
	adapter := New(DefaultOptions())
	if _, err := adapter.LocateTranscript(context.Background(), agentruntime.LocateRequest{ConfigRoot: t.TempDir()}); err == nil {
		t.Fatal("expected error for empty native session id")
	}
}

func TestLocateTranscriptMissingDB(t *testing.T) {
	adapter := New(DefaultOptions())
	_, err := adapter.LocateTranscript(context.Background(), agentruntime.LocateRequest{
		NativeSessionID: "ses_x",
		ConfigRoot:      t.TempDir(), // no opencode.db under it
	})
	if err == nil {
		t.Fatal("expected error for missing database")
	}
}

func TestLocateTranscriptSessionNotFound(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, ".local", "share", "opencode")
	dbPath := newTestDB(t, dataDir)
	insertSession(t, dbPath, "ses_present")

	adapter := New(DefaultOptions())
	_, err := adapter.LocateTranscript(context.Background(), agentruntime.LocateRequest{
		NativeSessionID: "ses_absent",
		ConfigRoot:      root,
	})
	if err == nil {
		t.Fatal("expected not-found error for absent session")
	}
}

// TestParseUsageRealDB cross-checks our per-message summation against opencode's
// own denormalized session-aggregate columns, using whatever recent session
// exists in the developer's real database. It skips cleanly when opencode is not
// installed (CI / fresh machines). This guards the exclusive-mapping decision
// (input excludes cache, output excludes reasoning) against live data.
func TestParseUsageRealDB(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	dbPath := filepath.Join(home, ".local", "share", "opencode", "opencode.db")
	if _, err := os.Stat(dbPath); err != nil {
		t.Skipf("real opencode.db not present: %v", err)
	}
	db, err := openReadOnly(dbPath)
	if err != nil {
		t.Skipf("open real db: %v", err)
	}
	defer db.Close()

	var (
		sid                               string
		aggInput, aggOutput, aggReasoning int
		aggCacheRead, aggCacheWrite       int
	)
	row := db.QueryRow(`SELECT id, tokens_input, tokens_output, tokens_reasoning, tokens_cache_read, tokens_cache_write
		FROM session WHERE tokens_input > 0 ORDER BY time_updated DESC LIMIT 1`)
	if err := row.Scan(&sid, &aggInput, &aggOutput, &aggReasoning, &aggCacheRead, &aggCacheWrite); err != nil {
		t.Skipf("no session with tokens in real db: %v", err)
	}

	usage, err := New(DefaultOptions()).ParseUsage(context.Background(), dbPath+pathSep+sid)
	if err != nil {
		t.Fatal(err)
	}
	// Our message-sum must equal opencode's own aggregate, with reasoning folded
	// into OutputTokens and cache kept separate from InputTokens.
	want := agentruntime.Usage{
		InputTokens:      aggInput,
		OutputTokens:     aggOutput + aggReasoning,
		CacheReadTokens:  aggCacheRead,
		CacheWriteTokens: aggCacheWrite,
		Model:            usage.Model,    // model shape varies; asserted in synthetic tests
		Messages:         usage.Messages, // assistant-turn count not in the aggregate
	}
	if usage != want {
		t.Fatalf("session %s: usage = %+v, want (from session aggregate) %+v", sid, usage, want)
	}
}
