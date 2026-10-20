package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/hiveryn/agentruntime"
)

// LocateTranscript resolves the absolute path of the rollout that currently
// records a Codex thread: its newest verified segment (see rollout.go). The
// date bucket under <configRoot>/sessions is variable depth and the thread id
// is a suffix of the filename, so the sessions tree is walked and every
// candidate's identity is read from its session_meta rather than trusting the
// name. The hook payload does not reliably carry the transcript path (its
// transcript_path field is nullable), so this is the primary discovery
// mechanism. A never-resumed thread has exactly one segment; after a resume
// the newest segment is where new entries land and where token_count totals
// (cumulative across segments) are current, so ParseUsage reads it unchanged,
// though it counts only that segment's agent messages. Callers that need the
// whole record (receipts) use LocateRolloutSegments. req.Workdir is unused —
// the rollout location is not keyed on cwd.
func (a *Adapter) LocateTranscript(ctx context.Context, req agentruntime.LocateRequest) (string, error) {
	if req.NativeSessionID == "" {
		return "", fmt.Errorf("codex: locate transcript: empty native session id")
	}
	segments, err := a.LocateRolloutSegments(ctx, req)
	if err != nil {
		return "", fmt.Errorf("codex: locate transcript: %w", err)
	}
	if len(segments) == 0 {
		home, _ := codexHome(req.ConfigRoot)
		return "", fmt.Errorf("codex: transcript not found for session %q under %q", req.NativeSessionID, filepath.Join(home, "sessions"))
	}
	return segments[len(segments)-1].Path, nil
}

// rolloutLine is the minimal subset of a Codex JSONL rollout line that usage
// accounting reads. Token usage lives on event_msg lines whose payload.type is
// "token_count"; the model id lives on turn_context lines (payload.model).
type rolloutLine struct {
	Type    string `json:"type"`
	Payload struct {
		Type  string `json:"type"`
		Model string `json:"model"`
		Info  struct {
			TotalTokenUsage struct {
				InputTokens       int `json:"input_tokens"`
				CachedInputTokens int `json:"cached_input_tokens"`
				OutputTokens      int `json:"output_tokens"`
			} `json:"total_token_usage"`
		} `json:"info"`
	} `json:"payload"`
}

// ParseUsage reads a Codex rollout and returns normalized usage. Codex reports
// cumulative totals on every token_count event, so usage is the LAST such event
// (not a sum). Codex's input_tokens is the blended prompt total (it includes
// cached_input_tokens) and output_tokens already includes reasoning_output_tokens,
// so we normalize to match agentruntime.Usage's uncached/non-reasoning semantics:
// InputTokens = input - cached, OutputTokens = output. Codex has no cache-creation
// concept, so CacheWriteTokens is always 0.
func (a *Adapter) ParseUsage(_ context.Context, transcriptPath string) (agentruntime.Usage, error) {
	file, err := os.Open(transcriptPath)
	if err != nil {
		return agentruntime.Usage{}, fmt.Errorf("codex: open transcript: %w", err)
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	// The session_meta line embeds the full base instructions and exceeds the
	// 64KiB default scanner cap; raise it well above that.
	scanner.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)

	var (
		usage    agentruntime.Usage
		haveUsed bool
		lastUsed struct{ input, cached, output int }
	)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec rolloutLine
		if err := json.Unmarshal(line, &rec); err != nil {
			return agentruntime.Usage{}, fmt.Errorf("codex: decode rollout line: %w", err)
		}
		if rec.Payload.Model != "" {
			usage.Model = rec.Payload.Model
		}
		if rec.Type != "event_msg" {
			continue
		}
		switch rec.Payload.Type {
		case "agent_message":
			usage.Messages++
		case "token_count":
			t := rec.Payload.Info.TotalTokenUsage
			lastUsed.input = t.InputTokens
			lastUsed.cached = t.CachedInputTokens
			lastUsed.output = t.OutputTokens
			haveUsed = true
		}
	}
	if err := scanner.Err(); err != nil {
		return agentruntime.Usage{}, fmt.Errorf("codex: scan transcript: %w", err)
	}

	if haveUsed {
		usage.InputTokens = lastUsed.input - lastUsed.cached
		usage.CacheReadTokens = lastUsed.cached
		usage.OutputTokens = lastUsed.output
	}
	return usage, nil
}
