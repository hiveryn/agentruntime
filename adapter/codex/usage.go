package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/hiveryn/agentruntime"
)

// LocateTranscript resolves the absolute path of a Codex session rollout.
// Codex stores rollouts at <configRoot>/sessions/YYYY/MM/DD/rollout-<ts>-<id>.jsonl
// (older layouts place them flat under <configRoot>/sessions/). The session id is
// a suffix of the filename, not the whole stem, and the date bucket is variable
// depth, so we walk the sessions tree and match by the rollout-…-<id>.jsonl suffix
// rather than constructing a fixed path. The hook payload does not reliably carry
// the transcript path (its transcript_path field is nullable), so this glob is the
// primary discovery mechanism. req.Workdir is unused — the rollout location is not
// keyed on cwd.
func (a *Adapter) LocateTranscript(_ context.Context, req agentruntime.LocateRequest) (string, error) {
	if req.NativeSessionID == "" {
		return "", fmt.Errorf("codex: locate transcript: empty native session id")
	}
	home, err := codexHome(req.ConfigRoot)
	if err != nil {
		return "", fmt.Errorf("codex: locate transcript: resolve codex home: %w", err)
	}
	sessions := filepath.Join(home, "sessions")
	suffix := "-" + req.NativeSessionID + ".jsonl"

	var matches []string
	walkErr := filepath.WalkDir(sessions, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if strings.HasPrefix(name, "rollout-") && strings.HasSuffix(name, suffix) {
			matches = append(matches, path)
		}
		return nil
	})
	if walkErr != nil {
		return "", fmt.Errorf("codex: locate transcript: walk %q: %w", sessions, walkErr)
	}

	switch len(matches) {
	case 0:
		return "", fmt.Errorf("codex: transcript not found for session %q under %q", req.NativeSessionID, sessions)
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("codex: ambiguous transcript for session %q: %d matches under %q", req.NativeSessionID, len(matches), sessions)
	}
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
	defer file.Close()

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
