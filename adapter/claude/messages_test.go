package claude

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hiveryn/agentruntime"
)

func TestPrepareLaunchMessageDeliveryAddsRewakeSettings(t *testing.T) {
	adapter := New(Options{NewSessionID: func() (string, error) { return "00000000-0000-4000-8000-000000000001", nil }})
	spec, err := adapter.PrepareLaunch(context.Background(), agentruntime.StartRequest{
		ID: "s1", Agent: agentruntime.AgentClaude, Workdir: "/tmp/work",
		MessageDelivery: &agentruntime.MessageDelivery{HookWaitTimeout: 2 * time.Hour},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		for _, p := range spec.CleanupPaths {
			_ = os.Remove(p)
		}
	}()
	var path string
	for i, arg := range spec.Args {
		if arg == "--settings" && i+1 < len(spec.Args) {
			path = spec.Args[i+1]
		}
	}
	if path == "" {
		t.Fatalf("no --settings in %q", spec.Args)
	}
	found := false
	for _, p := range spec.CleanupPaths {
		found = found || p == path
	}
	if !found {
		t.Fatalf("settings %s not in cleanup paths %v", path, spec.CleanupPaths)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var settings struct {
		Hooks map[string][]struct {
			Hooks []map[string]any `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	for _, event := range []string{"SessionStart", "Stop"} {
		groups := settings.Hooks[event]
		if len(groups) != 1 || len(groups[0].Hooks) != 1 {
			t.Fatalf("%s groups = %+v", event, groups)
		}
		hook := groups[0].Hooks[0]
		if hook["asyncRewake"] != true || hook["timeout"] != float64(7200) || hook["type"] != "command" {
			t.Fatalf("%s hook = %+v", event, hook)
		}
		cmd, _ := hook["command"].(string)
		if !strings.HasPrefix(cmd, "node -e '") || !strings.Contains(cmd, `/claude/wait`) || strings.Count(cmd, "'") != 2 {
			t.Fatalf("%s command = %q", event, cmd)
		}
	}
	if len(settings.Hooks) != 2 {
		t.Fatalf("hooks = %v", settings.Hooks)
	}
	// Default timeout is applied when none is set.
	if (&agentruntime.MessageDelivery{}).MessageHookTimeout() != agentruntime.DefaultMessageHookTimeout {
		t.Fatal("default hook timeout not applied")
	}
	// --settings is managed.
	if _, err := adapter.PrepareLaunch(context.Background(), agentruntime.StartRequest{ID: "s1", Workdir: "/tmp/work", Args: []string{"--settings", "x"}}); err == nil {
		t.Fatal("--settings in raw args accepted")
	}
}

func TestPrepareLaunchWithoutMessageDeliveryHasNoSettings(t *testing.T) {
	adapter := New(Options{NewSessionID: func() (string, error) { return "00000000-0000-4000-8000-000000000001", nil }})
	spec, err := adapter.PrepareLaunch(context.Background(), agentruntime.StartRequest{ID: "s1", Workdir: "/tmp/work"})
	if err != nil {
		t.Fatal(err)
	}
	for _, arg := range spec.Args {
		if arg == "--settings" {
			t.Fatalf("unexpected --settings in %q", spec.Args)
		}
	}
	if len(spec.CleanupPaths) != 0 {
		t.Fatalf("cleanup paths %v", spec.CleanupPaths)
	}
}

// runWaiter runs the generated waiter command the way Claude would, with the
// hook JSON on stdin, and returns exit code, stderr and the request the
// mailbox saw.
func runWaiter(t *testing.T, endpoint string, hook map[string]any) (int, string) {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available")
	}
	stdin, _ := json.Marshal(hook)
	cmd := exec.Command("sh", "-c", messageWaiterCommand())
	cmd.Stdin = strings.NewReader(string(stdin))
	cmd.Env = append(envWithoutAgentRuntimeSessionID(), "AGENTRUNTIME_HOOK_ENDPOINT="+endpoint, "AGENTRUNTIME_SESSION_ID=hiv-1")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if exitErr, ok := err.(*exec.ExitError); ok {
		code = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("run waiter: %v", err)
	}
	return code, stderr.String()
}

func TestMessageWaiterExits2WithTextAndRetriesTransportErrors(t *testing.T) {
	var seen struct {
		body   map[string]any
		bodies []map[string]any
		calls  int
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.calls++
		if r.URL.Path != "/claude/wait" || r.Method != http.MethodPost {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		seen.bodies = append(seen.bodies, body)
		if seen.calls == 1 {
			// A dropped connection must be retried in-process.
			hj, _ := w.(http.Hijacker)
			conn, _, _ := hj.Hijack()
			_ = conn.Close()
			return
		}
		seen.body = body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"m1","text":"Hiveryn message m1: Ticket X concluded"}`))
	}))
	defer server.Close()
	code, stderr := runWaiter(t, server.URL, map[string]any{"hook_event_name": "Stop", "session_id": "native-1"})
	if code != 2 || stderr != "Hiveryn message m1: Ticket X concluded\n" {
		t.Fatalf("code %d stderr %q", code, stderr)
	}
	if seen.calls != 2 || seen.body["env"].(map[string]any)["AGENTRUNTIME_SESSION_ID"] != "hiv-1" || seen.body["hook"].(map[string]any)["session_id"] != "native-1" {
		t.Fatalf("calls %d body %+v", seen.calls, seen.body)
	}
	// The retry is the same waiter reconnecting: same start, next attempt.
	first, retry := seen.bodies[0]["waiter"].(map[string]any), seen.bodies[1]["waiter"].(map[string]any)
	if first["attempt"] != float64(0) || retry["attempt"] != float64(1) || first["started"] != retry["started"] {
		t.Fatalf("waiter identity: first %+v retry %+v", first, retry)
	}
	if started, _ := first["started"].(float64); started <= 0 || time.Since(time.UnixMilli(int64(started))) > time.Minute {
		t.Fatalf("waiter start %v", first["started"])
	}
}

func TestMessageWaiterExits0WhenSupersededOrUnknownOrNoEndpoint(t *testing.T) {
	for _, status := range []int{http.StatusNoContent, http.StatusNotFound} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) }))
		code, stderr := runWaiter(t, server.URL, map[string]any{"hook_event_name": "SessionStart", "session_id": "n"})
		server.Close()
		if code != 0 || stderr != "" {
			t.Fatalf("status %d: code %d stderr %q", status, code, stderr)
		}
	}
	code, stderr := runWaiter(t, "", map[string]any{"hook_event_name": "Stop"})
	if code != 0 || stderr != "" {
		t.Fatalf("no endpoint: code %d stderr %q", code, stderr)
	}
}

func TestConfirmMessageReadsTranscript(t *testing.T) {
	adapter := New(DefaultOptions())
	dir := t.TempDir()
	path := filepath.Join(dir, "t.jsonl")
	lines := []string{
		`{"type":"user","timestamp":"2026-10-20T08:00:00.000Z","message":{"role":"user","content":"do the task"},"turnOrigin":"human"}`,
		`{"type":"assistant","timestamp":"2026-10-20T08:00:05.000Z","message":{"role":"assistant","content":[{"type":"text","text":"done"}]}}`,
		`{"type":"queue-operation","op":"enqueue","content":"Hiveryn message 5d1e-aaa: Ticket X"}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	receipt, err := adapter.ConfirmMessage(context.Background(), agentruntime.ConfirmRequest{TranscriptHandle: path, MessageID: "5d1e-aaa"})
	if err != nil || receipt.Appeared || receipt.Acknowledged {
		t.Fatalf("queued only: %+v %v", receipt, err)
	}
	// The record reaches the newest timestamped entry (entries without one
	// do not move it).
	if want := time.Date(2026, 10, 20, 8, 0, 5, 0, time.UTC); !receipt.RecordedThrough.Equal(want) {
		t.Fatalf("recorded through %v, want %v", receipt.RecordedThrough, want)
	}
	lines = append(lines, `{"type":"user","timestamp":"2026-10-20T08:10:00.000Z","turnOrigin":"task_notification","message":{"role":"user","content":"<system-reminder>Hiveryn message 5d1e-aaa: Ticket X</system-reminder>"}}`)
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	receipt, err = adapter.ConfirmMessage(context.Background(), agentruntime.ConfirmRequest{TranscriptHandle: path, MessageID: "5d1e-aaa"})
	if err != nil || !receipt.Appeared || receipt.Acknowledged || !receipt.RecordedThrough.Equal(time.Date(2026, 10, 20, 8, 10, 0, 0, time.UTC)) {
		t.Fatalf("appeared: %+v %v", receipt, err)
	}
	lines = append(lines, `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"ACK"}]}}`)
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	receipt, err = adapter.ConfirmMessage(context.Background(), agentruntime.ConfirmRequest{TranscriptHandle: path, MessageID: "5d1e-aaa"})
	if err != nil || !receipt.Appeared || !receipt.Acknowledged {
		t.Fatalf("acknowledged: %+v %v", receipt, err)
	}
	if _, err := adapter.DeliverMessage(context.Background(), agentruntime.DeliverRequest{}); err == nil {
		t.Fatal("pull adapter accepted a push")
	}
	if cap := adapter.MessageCapability(); cap.Mode != agentruntime.MessageDeliveryPull || !cap.GateOnIdle {
		t.Fatalf("capability %+v", cap)
	}
}
