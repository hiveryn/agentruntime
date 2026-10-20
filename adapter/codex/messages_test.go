package codex

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hiveryn/agentruntime"
)

// fakeCodex installs a `codex` executable on PATH that runs the fake
// app-server and returns the queue file it records to.
func fakeCodex(t *testing.T) (command, queueFile string) {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not available")
	}
	dir := t.TempDir()
	script, err := filepath.Abs(filepath.Join("testdata", "fake-app-server.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	command = filepath.Join(dir, "codex")
	wrapper := "#!/bin/sh\n[ \"$1\" = app-server ] || { echo \"unexpected args $*\" >&2; exit 2; }\nexec " + node + " " + script + "\n"
	if err := os.WriteFile(command, []byte(wrapper), 0o755); err != nil {
		t.Fatal(err)
	}
	queueFile = filepath.Join(dir, "queue.json")
	return command, queueFile
}

func TestDeliverMessageEnqueuesOnceUnderCallerID(t *testing.T) {
	adapter := New(DefaultOptions())
	command, queueFile := fakeCodex(t)
	home := t.TempDir()
	env := map[string]string{"CODEX_HOME": home, "FAKE_QUEUE_FILE": queueFile, "FAKE_EXPECT_HOME": home}
	req := agentruntime.DeliverRequest{Command: command, Env: env, NativeSessionID: "thread-1", MessageID: "msg-uuid-1", Text: "Hiveryn message msg-uuid-1: hello"}

	res, err := adapter.DeliverMessage(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != agentruntime.DeliverAccepted || res.ProviderRef != "qs-1" {
		t.Fatalf("first = %+v", res)
	}
	// A second message, then a retry of the first: the retry finds it queued
	// on the second page and sends nothing.
	second := req
	second.MessageID, second.Text = "msg-uuid-2", "second"
	if _, err := adapter.DeliverMessage(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	res, err = adapter.DeliverMessage(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != agentruntime.DeliverAlreadyPresent || res.ProviderRef != "qs-1" {
		t.Fatalf("retry = %+v", res)
	}
	data, _ := os.ReadFile(queueFile)
	var queue []map[string]any
	if err := json.Unmarshal(data, &queue); err != nil || len(queue) != 2 {
		t.Fatalf("queue = %s (%v)", data, err)
	}
	if queue[0]["clientUserMessageId"] != "msg-uuid-1" || queue[0]["text"] != "Hiveryn message msg-uuid-1: hello" {
		t.Fatalf("queued item = %+v", queue[0])
	}

	// Confirm: not in the rollout yet, still queued.
	rollout := filepath.Join(t.TempDir(), "rollout.jsonl")
	if err := os.WriteFile(rollout, []byte(`{"type":"event_msg","payload":{"type":"task_started"}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	receipt, err := adapter.ConfirmMessage(context.Background(), agentruntime.ConfirmRequest{TranscriptHandle: rollout, Command: command, Env: env, NativeSessionID: "thread-1", MessageID: "msg-uuid-1"})
	if err != nil || receipt.Appeared || !receipt.Queued {
		t.Fatalf("queued receipt = %+v %v", receipt, err)
	}

	// The wrong CODEX_HOME is an error, never a silent miss.
	bad := req
	bad.Env = map[string]string{"CODEX_HOME": "/elsewhere", "FAKE_QUEUE_FILE": queueFile, "FAKE_EXPECT_HOME": home}
	if _, err := adapter.DeliverMessage(context.Background(), bad); err == nil || !strings.Contains(err.Error(), "wrong CODEX_HOME") {
		t.Fatalf("wrong home err = %v", err)
	}
	// A missing executable is reported.
	missing := req
	missing.Command = filepath.Join(t.TempDir(), "nope")
	if _, err := adapter.DeliverMessage(context.Background(), missing); err == nil {
		t.Fatal("missing executable accepted")
	}
}

func TestConfirmMessageReadsRolloutClientID(t *testing.T) {
	adapter := New(DefaultOptions())
	rollout := filepath.Join(t.TempDir(), "rollout.jsonl")
	lines := []string{
		`{"timestamp":"t","type":"event_msg","payload":{"type":"task_started","turn_attribution":{"turn_trigger":"queue"}}}`,
		`{"timestamp":"t","type":"event_msg","payload":{"type":"item_completed","item":{"type":"user_message","id":"x","client_id":"msg-uuid-1","content":"Hiveryn message msg-uuid-1"}}}`,
	}
	write := func() {
		if err := os.WriteFile(rollout, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write()
	receipt, err := adapter.ConfirmMessage(context.Background(), agentruntime.ConfirmRequest{TranscriptHandle: rollout, MessageID: "msg-uuid-1"})
	if err != nil || !receipt.Appeared || receipt.Acknowledged || receipt.Queued {
		t.Fatalf("appeared = %+v %v", receipt, err)
	}
	// The reply is recorded as an item_completed AgentMessage item (0.163.0).
	lines = append(lines, `{"timestamp":"t","type":"event_msg","payload":{"type":"item_completed","item":{"type":"AgentMessage","id":"y","text":"ACK"}}}`)
	write()
	receipt, err = adapter.ConfirmMessage(context.Background(), agentruntime.ConfirmRequest{TranscriptHandle: rollout, MessageID: "msg-uuid-1"})
	if err != nil || !receipt.Appeared || !receipt.Acknowledged {
		t.Fatalf("acknowledged = %+v %v", receipt, err)
	}
	// Another message's id does not match, and the flat payload client_id
	// shape is read too.
	receipt, err = adapter.ConfirmMessage(context.Background(), agentruntime.ConfirmRequest{TranscriptHandle: rollout, MessageID: "msg-uuid-9"})
	if err != nil || receipt.Appeared {
		t.Fatalf("other id = %+v %v", receipt, err)
	}
	lines = []string{`{"type":"event_msg","payload":{"type":"user_message","client_id":"flat-1","message":"m"}}`,
		`{"type":"event_msg","payload":{"type":"agent_message","message":"ACK"}}`}
	write()
	receipt, err = adapter.ConfirmMessage(context.Background(), agentruntime.ConfirmRequest{TranscriptHandle: rollout, MessageID: "flat-1"})
	if err != nil || !receipt.Appeared || !receipt.Acknowledged {
		t.Fatalf("flat = %+v %v", receipt, err)
	}
	if cap := adapter.MessageCapability(); cap.Mode != agentruntime.MessageDeliveryPush || cap.GateOnIdle {
		t.Fatalf("capability %+v", cap)
	}
}
