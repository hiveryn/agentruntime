package codex

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hiveryn/agentruntime"
)

func TestParseUsage(t *testing.T) {
	adapter := New(DefaultOptions())
	usage, err := adapter.ParseUsage(context.Background(), filepath.Join("testdata", "rollout_usage.jsonl"))
	if err != nil {
		t.Fatal(err)
	}

	// Codex reports cumulative totals on every token_count event, so usage is the
	// LAST event (input=300, cached=200, output=25), not the sum of both events.
	// input/output are normalized: InputTokens drops the cached portion, OutputTokens
	// keeps reasoning folded in (do not add reasoning_output_tokens).
	want := agentruntime.Usage{
		InputTokens:      100, // 300 - 200 cached (final event, not summed)
		OutputTokens:     25,  // final output_tokens; reasoning (5) already included
		CacheWriteTokens: 0,   // codex has no cache-creation concept
		CacheReadTokens:  200, // final cached_input_tokens
		Model:            "gpt-5.5",
		Messages:         2, // two agent_message events
	}
	if usage != want {
		t.Fatalf("usage = %+v, want %+v", usage, want)
	}
}

func TestParseUsageMissingFile(t *testing.T) {
	adapter := New(DefaultOptions())
	if _, err := adapter.ParseUsage(context.Background(), filepath.Join(t.TempDir(), "nope.jsonl")); err == nil {
		t.Fatal("expected error for missing transcript")
	}
}

// writeRollout creates a rollout file holding only its session_meta line at
// sessions/<bucket>/rollout-<ts>-<id>.jsonl.
func writeRollout(t *testing.T, root, bucket, ts, id string) string {
	t.Helper()
	return writeSegment(t, root, bucket, ts, id, "", "")
}

func TestLocateTranscript(t *testing.T) {
	root := t.TempDir()
	id := "019eb65a-a50a-7353-b9ec-88fd2de2d323"
	want := writeRollout(t, root, filepath.Join("2026", "06", "11"), "2026-06-11T11-04-14", id)

	adapter := New(DefaultOptions())
	got, err := adapter.LocateTranscript(context.Background(), agentruntime.LocateRequest{
		NativeSessionID: id,
		ConfigRoot:      root,
		Workdir:         "/Users/dev/proj",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}

func TestLocateTranscriptEmptyID(t *testing.T) {
	adapter := New(DefaultOptions())
	if _, err := adapter.LocateTranscript(context.Background(), agentruntime.LocateRequest{ConfigRoot: t.TempDir()}); err == nil {
		t.Fatal("expected error for empty native session id")
	}
}

func TestLocateTranscriptNotFound(t *testing.T) {
	root := t.TempDir()
	writeRollout(t, root, filepath.Join("2026", "06", "11"), "2026-06-11T11-04-14", "11111111-1111-4111-8111-111111111111")

	adapter := New(DefaultOptions())
	_, err := adapter.LocateTranscript(context.Background(), agentruntime.LocateRequest{
		NativeSessionID: "00000000-0000-4000-8000-000000000000",
		ConfigRoot:      root,
	})
	if err == nil {
		t.Fatal("expected not-found error")
	}
}

func TestLocateTranscriptAmbiguous(t *testing.T) {
	root := t.TempDir()
	id := "019eb65a-a50a-7353-b9ec-88fd2de2d323"
	// Two first files of the same thread under two date buckets (a copied
	// rollout): neither can be told to be the record.
	writeRollout(t, root, filepath.Join("2026", "06", "11"), "2026-06-11T11-04-14", id)
	writeRollout(t, root, filepath.Join("2026", "06", "12"), "2026-06-12T09-00-00", id)

	adapter := New(DefaultOptions())
	_, err := adapter.LocateTranscript(context.Background(), agentruntime.LocateRequest{
		NativeSessionID: id,
		ConfigRoot:      root,
	})
	if err == nil || !strings.Contains(err.Error(), "ambiguous rollout segment") {
		t.Fatalf("expected ambiguous-match error, got %v", err)
	}
}
