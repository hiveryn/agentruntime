package claude

import (
	"context"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
)

func TestRecordLocationAndCopyRead(t *testing.T) {
	a := New(DefaultOptions())
	id := "0b6e3c55-1f1d-4c6a-9d5e-5b8a3c0b9f11"
	loc := a.RecordLocation(id)
	if loc.RootEnv != "CLAUDE_CONFIG_DIR" || loc.DefaultRoot != ".claude" || len(loc.Patterns) != 1 {
		t.Fatalf("location = %+v", loc)
	}
	if ok, _ := path.Match(loc.Patterns[0], "projects/-home-x-repo/"+id+".jsonl"); !ok {
		t.Fatal("transcript not matched")
	}
	if ok, _ := path.Match(loc.Patterns[0], "projects/-home-x-repo/"+id+"/subagents/a.jsonl"); ok {
		t.Fatal("subagent file matched")
	}
	root := t.TempDir()
	rel := "projects/-home-x-repo/" + id + ".jsonl"
	if err := os.MkdirAll(filepath.Join(root, "projects/-home-x-repo"), 0o700); err != nil {
		t.Fatal(err)
	}
	line := `{"type":"user","uuid":"u1","promptId":"p1","turnOrigin":"human","timestamp":"2026-10-28T10:00:00Z","message":{"role":"user","content":"hello"}}` + "\n"
	if err := os.WriteFile(filepath.Join(root, rel), []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	req, err := a.HistoryRecordRequest(id, root, []string{rel})
	if err != nil {
		t.Fatal(err)
	}
	h, err := a.ReadHistory(context.Background(), req)
	if err != nil || len(h.Items) != 1 || h.Items[0].Text != "hello" {
		t.Fatalf("history = %+v, %v", h, err)
	}
	if _, err := a.HistoryRecordRequest(id, root, nil); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing = %v", err)
	}
	if _, err := a.HistoryRecordRequest(id, root, []string{rel, "projects/b/" + id + ".jsonl"}); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("ambiguous = %v", err)
	}
}
