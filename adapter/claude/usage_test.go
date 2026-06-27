package claude

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/hiveryn/agentruntime"
)

func TestParseUsage(t *testing.T) {
	adapter := New(DefaultOptions())
	usage, err := adapter.ParseUsage(context.Background(), filepath.Join("testdata", "transcript_usage.jsonl"))
	if err != nil {
		t.Fatal(err)
	}

	want := agentruntime.Usage{
		InputTokens:      151,                 // 100 + 1 (synthetic) + 50
		OutputTokens:     102,                 // 40 + 2 + 60
		CacheWriteTokens: 203,                 // 200 + 3 + 0
		CacheReadTokens:  1004,                // 300 + 4 + 700
		Model:            "claude-sonnet-4-6", // last non-synthetic assistant line
		Messages:         3,                   // assistant turns, synthetic included
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

func TestLocateTranscript(t *testing.T) {
	root := t.TempDir()
	id := "ba5b1aa9-542e-400c-960e-2af4c533b5a2"
	dir := filepath.Join(root, "projects", "-tmp-work")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, id+".jsonl")
	if err := os.WriteFile(want, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	adapter := New(DefaultOptions())
	got, err := adapter.LocateTranscript(context.Background(), agentruntime.LocateRequest{
		NativeSessionID: id,
		ConfigRoot:      root,
		Workdir:         "/tmp/work",
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
	adapter := New(DefaultOptions())
	_, err := adapter.LocateTranscript(context.Background(), agentruntime.LocateRequest{
		NativeSessionID: "00000000-0000-4000-8000-000000000000",
		ConfigRoot:      t.TempDir(),
	})
	if err == nil {
		t.Fatal("expected not-found error")
	}
}

func TestLocateTranscriptAmbiguous(t *testing.T) {
	root := t.TempDir()
	id := "00000000-0000-4000-8000-000000000001"
	for _, slug := range []string{"-a", "-b"} {
		dir := filepath.Join(root, "projects", slug)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	adapter := New(DefaultOptions())
	_, err := adapter.LocateTranscript(context.Background(), agentruntime.LocateRequest{
		NativeSessionID: id,
		ConfigRoot:      root,
	})
	if err == nil {
		t.Fatal("expected ambiguous-match error")
	}
}
