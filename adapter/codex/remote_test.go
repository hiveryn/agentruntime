package codex

import (
	"context"
	"os"
	"os/exec"
	"path"
	"strings"
	"testing"

	"github.com/hiveryn/agentruntime"
)

// The record location globs match exactly the thread's segment files in both
// layouts, and a copy fetched at those relative paths reads like a CODEX_HOME.
func TestRecordLocationGlobsAndCopyRead(t *testing.T) {
	a := New(Options{})
	loc := a.RecordLocation(threadA)
	if loc.RootEnv != "CODEX_HOME" || loc.DefaultRoot != ".codex" {
		t.Fatalf("location = %+v", loc)
	}
	names := map[string]bool{
		"sessions/2026/10/27/rollout-2026-10-27T02-00-00-" + threadA + ".jsonl":               true,
		"sessions/2026/10/27/rollout-2026-10-27T02-05-00-" + threadA + "_" + segA1 + ".jsonl": true,
		"sessions/rollout-2025-01-01T00-00-00-" + threadA + ".jsonl":                          true,
		"sessions/2026/10/27/rollout-2026-10-27T02-00-00-" + threadB + ".jsonl":               false,
		"sessions/2026/10/27/other-" + threadA + ".jsonl":                                     false,
	}
	for name, want := range names {
		got := false
		for _, pattern := range loc.Patterns {
			if ok, err := path.Match(pattern, name); err != nil {
				t.Fatal(err)
			} else if ok {
				got = true
			}
		}
		if got != want {
			t.Errorf("%s matched=%v, want %v", name, got, want)
		}
	}
	root := t.TempDir()
	writeSegment(t, root, "2026/10/27", "2026-10-27T02-00-00", threadA, "", "",
		`{"timestamp":"2026-10-27T02:00:02Z","type":"event_msg","payload":{"type":"item_completed","turn_id":"t1","item":{"type":"UserMessage","id":"um1","content":[{"type":"text","text":"hi"}]}}}`)
	req, err := a.HistoryRecordRequest(threadA, root, nil)
	if err != nil {
		t.Fatal(err)
	}
	h, err := a.ReadHistory(context.Background(), req)
	if err != nil || len(h.Items) != 1 || h.Items[0].Text != "hi" {
		t.Fatalf("history = %+v, %v", h, err)
	}
}

// Exec builds the app-server process: the adapter uses it instead of a local
// command and does not layer the caller's environment over the session's.
func TestExecBuildsAppServerWithoutCallerEnvironment(t *testing.T) {
	command, queueFile := fakeCodex(t)
	home := t.TempDir()
	env := map[string]string{"CODEX_HOME": home, "FAKE_QUEUE_FILE": queueFile, "FAKE_EXPECT_HOME": home}
	t.Setenv("HIVERYN_LAPTOP_ONLY", "leak")
	var calls []string
	build := func(ctx context.Context, name string, args []string, env map[string]string) (*exec.Cmd, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		cmd := exec.CommandContext(ctx, command, args...)
		cmd.Env = []string{"PATH=" + os.Getenv("PATH")}
		for k, v := range env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
		return cmd, nil
	}
	a := New(DefaultOptions())
	req := agentruntime.DeliverRequest{Exec: build, Env: env, NativeSessionID: "thread-1", MessageID: "m1", Text: "hello"}
	if res, err := a.DeliverMessage(context.Background(), req); err != nil || res.Outcome != agentruntime.DeliverAccepted {
		t.Fatalf("deliver = %+v, %v", res, err)
	}
	queued, err := a.MessageQueued(context.Background(), agentruntime.ConfirmRequest{Exec: build, Env: env, NativeSessionID: "thread-1", MessageID: "m1"})
	if err != nil || !queued {
		t.Fatalf("queued = %v, %v", queued, err)
	}
	if queued, err := a.MessageQueued(context.Background(), agentruntime.ConfirmRequest{Exec: build, Env: env, NativeSessionID: "thread-1", MessageID: "other"}); err != nil || queued {
		t.Fatalf("other queued = %v, %v", queued, err)
	}
	if len(calls) != 3 || calls[0] != "codex app-server" {
		t.Fatalf("calls = %q", calls)
	}
	// A builder that fails establishes non-delivery.
	failing := req
	failing.Exec = func(context.Context, string, []string, map[string]string) (*exec.Cmd, error) {
		return nil, os.ErrPermission
	}
	if _, err := a.DeliverMessage(context.Background(), failing); err == nil || !agentruntime.IsNotDelivered(err) {
		t.Fatalf("failing builder err = %v", err)
	}
}
