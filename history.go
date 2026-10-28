package agentruntime

import (
	"context"
	"time"
)

// HistoryReader reads the visible conversation of a session owned by another
// process (a terminal TUI) from the provider's own durable record, read-only.
// It never starts, resumes or attaches to the conversation. The caller decides
// what each user input is (its own submission, a notification, the launch
// prompt, typed terminal input) from Ref and the text; the adapter reports
// only what the record says, including the provider's provenance.
//
// Hidden reasoning, developer/system instructions, injected context files and
// hook plumbing are excluded. Text is the visible message text or tool output.
type HistoryReader interface {
	ReadHistory(context.Context, HistoryRequest) (History, error)
}

// HistoryRequest names the record: TranscriptHandle from LocateTranscript
// (Claude), or NativeSessionID with ConfigRoot and the session's launch Env
// for providers that resolve their record themselves (Codex rollout segments,
// OpenCode's database under the session's XDG_DATA_HOME).
type HistoryRequest struct {
	TranscriptHandle string
	NativeSessionID  string
	ConfigRoot       string
	Env              map[string]string
}

// History is the record as of RecordedThrough (the newest entry time read).
type History struct {
	Items           []HistoryItem
	RecordedThrough time.Time
}

// HistoryItem is one visible native entry, in record order.
//
// Role: user, assistant, tool or system. A system item with TurnEnd set marks
// explicit native evidence that TurnID ended; Status is then completed,
// failed or interrupted. NativeOrigin is the provider's own provenance for a
// user input (Claude turnOrigin such as "human" or "task_notification", Codex
// turn_trigger such as "user" or "queue", OpenCode "prompt"); it is never
// rewritten. Ref is the provider-carried identity of a user input when the
// record has one (Codex client_id, OpenCode message id) — Claude carries none,
// so callers that need correlation embed their own marker in the text.
type HistoryItem struct {
	ID           string
	TurnID       string
	Role         string
	NativeOrigin string
	Ref          string
	Text         string
	Tool         string
	Status       string
	At           time.Time
	TurnEnd      bool
}

// RecordLocator says where a provider keeps a session's durable record, for
// callers that read it on another machine (over SSH) instead of the local
// filesystem. The caller resolves the root on the target from the session's
// launch environment there — RootEnv when set, else DefaultRoot under the
// target account's home — fetches the files matching Patterns (shell globs
// relative to that root) into a private local copy that keeps their relative
// paths, and reads the copy with HistoryRecordRequest. Nothing on the target is
// interpreted locally, and no laptop provider home is ever consulted.
type RecordLocator interface {
	RecordLocation(nativeSessionID string) RecordLocation
}

// RecordLocation is a provider's record layout for one native session.
type RecordLocation struct {
	// RootEnv names the environment variable selecting the config root
	// (CLAUDE_CONFIG_DIR, CODEX_HOME).
	RootEnv string
	// DefaultRoot is the root relative to the account's home when RootEnv
	// is unset (".claude", ".codex").
	DefaultRoot string
	// Patterns are shell globs relative to the root naming every file of the
	// session's record. Callers must pass only a native id they validated as
	// glob- and shell-safe.
	Patterns []string
}

// HistoryRecordRequest builds the HistoryRequest that reads a local copy of a
// session's record: copyRoot holds the fetched files at their paths relative
// to the provider root, and files lists those relative paths. It is the
// provider's own interpretation of its layout (Claude: exactly one transcript;
// Codex: every rollout segment under the copy, verified as usual).
type HistoryRecordRequester interface {
	HistoryRecordRequest(nativeSessionID, copyRoot string, files []string) (HistoryRequest, error)
}
