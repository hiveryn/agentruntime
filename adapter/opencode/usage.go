package opencode

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hiveryn/agentruntime"

	_ "modernc.org/sqlite" // pure-Go SQLite driver, registered as "sqlite"
)

// pathSep joins the opencode SQLite path and the session id into the single
// string the LocateTranscript/ParseUsage contract passes around. Opencode stores
// every session's usage in one shared database (no per-session transcript file),
// so the session id cannot be recovered from the db path alone — it is encoded
// here and split back out in ParseUsage. Consumers treat the result as opaque.
const pathSep = "::"

// LocateTranscript resolves the opencode usage source for a session. Unlike
// claude/codex (one transcript file per session), opencode 1.17.x persists all
// token usage in a single SQLite database under its XDG data dir. There is no
// per-session file, so we return "<dbPath>::<sessionID>" — an opaque handle that
// ParseUsage decodes. req.Workdir is unused (usage is keyed by session id, not cwd).
func (a *Adapter) LocateTranscript(_ context.Context, req agentruntime.LocateRequest) (string, error) {
	if req.NativeSessionID == "" {
		return "", fmt.Errorf("opencode: locate transcript: empty native session id")
	}
	dataDir, err := opencodeDataDir(req.ConfigRoot)
	if err != nil {
		return "", fmt.Errorf("opencode: locate transcript: resolve data dir: %w", err)
	}
	dbPath := filepath.Join(dataDir, "opencode.db")
	if _, err := os.Stat(dbPath); err != nil {
		return "", fmt.Errorf("opencode: locate transcript: %w", err)
	}

	db, err := openReadOnly(dbPath)
	if err != nil {
		return "", fmt.Errorf("opencode: locate transcript: open db: %w", err)
	}
	defer db.Close()

	var exists int
	row := db.QueryRow(`SELECT 1 FROM session WHERE id = ? LIMIT 1`, req.NativeSessionID)
	if err := row.Scan(&exists); err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("opencode: session %q not found in %s", req.NativeSessionID, dbPath)
		}
		return "", fmt.Errorf("opencode: locate transcript: query session: %w", err)
	}
	return dbPath + pathSep + req.NativeSessionID, nil
}

// messageData is the minimal subset of an opencode message.data JSON blob that
// usage accounting reads (schema: packages/opencode/src/session/message-v2.ts).
// Token fields are already EXCLUSIVE of one another — opencode's getUsage stores
// input net of cache (input = sdk_input - cache_read - cache_write) and output net
// of reasoning (output = sdk_output - reasoning) — so we never subtract, unlike codex.
type messageData struct {
	Role string `json:"role"`
	// opencode 1.17.x records the model at the top level; older versions nested it
	// under "model". Read both and prefer the top-level form.
	ModelID    string `json:"modelID"`
	ProviderID string `json:"providerID"`
	Model      struct {
		ProviderID string `json:"providerID"`
		ModelID    string `json:"modelID"`
	} `json:"model"`
	Tokens struct {
		Input     int `json:"input"`
		Output    int `json:"output"`
		Reasoning int `json:"reasoning"`
		Cache     struct {
			Read  int `json:"read"`
			Write int `json:"write"`
		} `json:"cache"`
	} `json:"tokens"`
}

// ParseUsage reads opencode's SQLite database and returns normalized usage for a
// session. The transcriptPath is the "<dbPath>::<sessionID>" handle minted by
// LocateTranscript. Token usage is SUMMED across all assistant messages of the
// session (contrast codex's cumulative last-wins). Reasoning folds into
// OutputTokens (opencode tracks it separately on disk; agentruntime.Usage does not).
func (a *Adapter) ParseUsage(_ context.Context, transcriptPath string) (agentruntime.Usage, error) {
	dbPath, sessionID, ok := strings.Cut(transcriptPath, pathSep)
	if !ok || sessionID == "" {
		return agentruntime.Usage{}, fmt.Errorf("opencode: parse usage: malformed transcript handle %q (want <dbPath>::<sessionID>)", transcriptPath)
	}

	db, err := openReadOnly(dbPath)
	if err != nil {
		return agentruntime.Usage{}, fmt.Errorf("opencode: parse usage: open db: %w", err)
	}
	defer db.Close()

	rows, err := db.Query(`SELECT data FROM message WHERE session_id = ? ORDER BY time_created ASC`, sessionID)
	if err != nil {
		return agentruntime.Usage{}, fmt.Errorf("opencode: parse usage: query messages: %w", err)
	}
	defer rows.Close()

	var usage agentruntime.Usage
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return agentruntime.Usage{}, fmt.Errorf("opencode: parse usage: scan row: %w", err)
		}
		var msg messageData
		if err := json.Unmarshal(raw, &msg); err != nil {
			return agentruntime.Usage{}, fmt.Errorf("opencode: parse usage: decode message data: %w", err)
		}
		if msg.Role != "assistant" {
			continue
		}
		usage.InputTokens += msg.Tokens.Input
		usage.OutputTokens += msg.Tokens.Output + msg.Tokens.Reasoning
		usage.CacheReadTokens += msg.Tokens.Cache.Read
		usage.CacheWriteTokens += msg.Tokens.Cache.Write
		usage.Messages++
		if modelID := msg.ModelID; modelID != "" {
			usage.Model = modelID // last assistant message wins
		} else if msg.Model.ModelID != "" {
			usage.Model = msg.Model.ModelID
		}
	}
	if err := rows.Err(); err != nil {
		return agentruntime.Usage{}, fmt.Errorf("opencode: parse usage: iterate rows: %w", err)
	}
	return usage, nil
}

// opencodeDataDir resolves the directory holding opencode.db. opencode follows
// the XDG base-dir spec for its data dir; ConfigRoot, when set, is the variant's
// HOME-base override (parallels setup.go's pluginPath, which appends .config/...).
// Precedence: explicit HOME-base > $XDG_DATA_HOME > $HOME/.local/share.
func opencodeDataDir(configRoot string) (string, error) {
	if configRoot != "" {
		return filepath.Join(configRoot, ".local", "share", "opencode"), nil
	}
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "opencode"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "opencode"), nil
}

// openReadOnly opens the opencode database read-only. opencode may be actively
// writing (WAL mode); mode=ro lets us read committed data without taking a write
// lock or mutating the file.
func openReadOnly(dbPath string) (*sql.DB, error) {
	return sql.Open("sqlite", "file:"+dbPath+"?mode=ro")
}
