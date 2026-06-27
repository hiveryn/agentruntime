package claude

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/hiveryn/agentruntime"
)

// syntheticModel marks transcript lines Claude generates internally; they are
// excluded from model-id resolution (but still summed/counted as turns).
const syntheticModel = "<synthetic>"

// LocateTranscript resolves the absolute path of a Claude session transcript.
// Claude stores transcripts at <configRoot>/projects/<cwd-slug>/<session-id>.jsonl.
// Session ids are globally unique UUIDs, so we glob by id across all project
// slugs rather than reconstructing the (lossy) cwd slug. req.Workdir is unused
// for Claude — it exists on LocateRequest for codex/opencode discovery.
func (a *Adapter) LocateTranscript(_ context.Context, req agentruntime.LocateRequest) (string, error) {
	if req.NativeSessionID == "" {
		return "", fmt.Errorf("claude: locate transcript: empty native session id")
	}
	root := req.ConfigRoot
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("claude: locate transcript: resolve home: %w", err)
		}
		root = filepath.Join(home, ".claude")
	}
	pattern := filepath.Join(root, "projects", "*", req.NativeSessionID+".jsonl")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return "", fmt.Errorf("claude: locate transcript: glob %q: %w", pattern, err)
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("claude: transcript not found for session %q under %q", req.NativeSessionID, root)
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("claude: ambiguous transcript for session %q: %d matches under %q", req.NativeSessionID, len(matches), root)
	}
}

// transcriptLine is the minimal subset of a Claude JSONL transcript line that
// usage accounting reads.
type transcriptLine struct {
	Type    string `json:"type"`
	Message struct {
		Model string `json:"model"`
		Usage struct {
			InputTokens         int `json:"input_tokens"`
			OutputTokens        int `json:"output_tokens"`
			CacheCreationTokens int `json:"cache_creation_input_tokens"`
			CacheReadTokens     int `json:"cache_read_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

// ParseUsage sums token usage across the assistant turns of a Claude transcript
// and resolves the model id from the last non-synthetic assistant message.
func (a *Adapter) ParseUsage(_ context.Context, transcriptPath string) (agentruntime.Usage, error) {
	file, err := os.Open(transcriptPath)
	if err != nil {
		return agentruntime.Usage{}, fmt.Errorf("claude: open transcript: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	// Transcript lines embed full message content and can be large; raise the
	// scanner's token cap well above the 64KiB default.
	scanner.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)

	var usage agentruntime.Usage
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec transcriptLine
		if err := json.Unmarshal(line, &rec); err != nil {
			return agentruntime.Usage{}, fmt.Errorf("claude: decode transcript line: %w", err)
		}
		if rec.Type != "assistant" {
			continue
		}
		usage.InputTokens += rec.Message.Usage.InputTokens
		usage.OutputTokens += rec.Message.Usage.OutputTokens
		usage.CacheWriteTokens += rec.Message.Usage.CacheCreationTokens
		usage.CacheReadTokens += rec.Message.Usage.CacheReadTokens
		usage.Messages++
		if rec.Message.Model != "" && rec.Message.Model != syntheticModel {
			usage.Model = rec.Message.Model
		}
	}
	if err := scanner.Err(); err != nil {
		return agentruntime.Usage{}, fmt.Errorf("claude: scan transcript: %w", err)
	}
	return usage, nil
}
