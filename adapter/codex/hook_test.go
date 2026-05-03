package codex

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/hiveryn/agentruntime"
)

func TestHookCommandReturnsPopulatedHookCommand(t *testing.T) {
	hc := HookCommand("http://127.0.0.1:9000")
	if hc.Command == "" {
		t.Fatal("expected non-empty command")
	}
	if hc.Endpoint != "http://127.0.0.1:9000" {
		t.Fatalf("endpoint: %q", hc.Endpoint)
	}
	if hc.Timeout != 10*time.Second {
		t.Fatalf("timeout: %v", hc.Timeout)
	}
	if hc.StatusMessage != "agentruntime codex hook" {
		t.Fatalf("status message: %q", hc.StatusMessage)
	}
}

func TestHookCommandContainsExpectedComponents(t *testing.T) {
	hc := HookCommand("http://127.0.0.1:9000")
	cmd := hc.Command

	for _, want := range []string{
		"node -e",
		"Content-Type",
		"application/json",
		"received_at",
		"AGENTRUNTIME_SESSION_ID",
		"hook_cwd",
		"hook:h",
		"process.cwd()",
		"http://127.0.0.1:9000/codex",
	} {
		if !strings.Contains(cmd, want) {
			t.Errorf("command missing %q\n%s", want, cmd)
		}
	}

	if strings.Contains(cmd, "agent:") && !strings.Contains(cmd, `agent:"claude"`) {
		t.Errorf("codex command should not include agent field\n%s", cmd)
	}
}

func TestHookCommandPostsCorrectEnvelope(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available")
	}

	var receivedPayload []byte
	var receivedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPayload, _ = io.ReadAll(r.Body)
		receivedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sessionID := "test-codex-session-42"
	hookStdin := `{"hook_event_name":"SessionStart","session_id":"native-abc-123","source":"startup","model":"test-model","cwd":"/tmp/test"}`

	hc := HookCommand(server.URL)

	cmd := exec.Command("sh", "-c", hc.Command)
	cmd.Env = append(os.Environ(), "AGENTRUNTIME_SESSION_ID="+sessionID)
	cmd.Stdin = strings.NewReader(hookStdin)

	if err := cmd.Run(); err != nil {
		t.Fatalf("hook command failed: %v", err)
	}

	if receivedPath != "/codex" {
		t.Errorf("path: got %q want /codex", receivedPath)
	}

	var envelope map[string]any
	if err := json.Unmarshal(receivedPayload, &envelope); err != nil {
		t.Fatalf("decode envelope: %v\nbody: %s", err, receivedPayload)
	}

	hook, ok := envelope["hook"].(map[string]any)
	if !ok {
		t.Fatalf("missing hook in envelope: %v", envelope)
	}
	if hook["hook_event_name"] != "SessionStart" {
		t.Errorf("hook_event_name: got %q", hook["hook_event_name"])
	}
	if hook["session_id"] != "native-abc-123" {
		t.Errorf("session_id: got %q", hook["session_id"])
	}

	env, ok := envelope["env"].(map[string]any)
	if !ok {
		t.Fatalf("missing env in envelope: %v", envelope)
	}
	if env["AGENTRUNTIME_SESSION_ID"] != sessionID {
		t.Errorf("AGENTRUNTIME_SESSION_ID: got %q want %q", env["AGENTRUNTIME_SESSION_ID"], sessionID)
	}

	if _, ok := envelope["received_at"].(string); !ok {
		t.Errorf("missing or non-string received_at")
	}

	if _, ok := envelope["hook_cwd"].(string); !ok {
		t.Errorf("missing or non-string hook_cwd")
	}
}

func TestHookCommandEnvelopeCorrelatesWithNormalizeEvent(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available")
	}

	var receivedPayload []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPayload, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	adapter := New(DefaultOptions())
	sessionID := "correlate-codex-1"

	hookStdin := `{"hook_event_name":"SessionStart","session_id":"native-abc-456","source":"startup"}`

	hc := HookCommand(server.URL)

	cmd := exec.Command("sh", "-c", hc.Command)
	cmd.Env = append(os.Environ(), "AGENTRUNTIME_SESSION_ID="+sessionID)
	cmd.Stdin = strings.NewReader(hookStdin)

	if err := cmd.Run(); err != nil {
		t.Fatalf("hook command failed: %v", err)
	}

	event, err := adapter.NormalizeEvent(t.Context(), receivedPayload)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if event == nil {
		t.Fatal("expected event")
	}
	if event.ID != sessionID {
		t.Errorf("ID: got %q want %q", event.ID, sessionID)
	}
	if event.NativeID != "native-abc-456" {
		t.Errorf("NativeID: got %q", event.NativeID)
	}
	if event.Agent != agentruntime.AgentCodex {
		t.Errorf("Agent: got %q", event.Agent)
	}
	if event.Status != agentruntime.StatusStarting {
		t.Errorf("Status: got %q", event.Status)
	}
}

func TestHookCommandPreservesToolNameForEnvelope(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available")
	}

	var receivedPayload []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPayload, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	adapter := New(DefaultOptions())

	hookStdin := `{"hook_event_name":"PreToolUse","session_id":"native-abc-789","tool_name":"Bash","tool_input":{"command":"echo hi"},"turn_id":"turn-1"}`

	hc := HookCommand(server.URL)

	cmd := exec.Command("sh", "-c", hc.Command)
	cmd.Env = append(os.Environ(), "AGENTRUNTIME_SESSION_ID=correlate-codex-2")
	cmd.Stdin = strings.NewReader(hookStdin)

	if err := cmd.Run(); err != nil {
		t.Fatalf("hook command failed: %v", err)
	}

	event, err := adapter.NormalizeEvent(t.Context(), receivedPayload)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if event == nil {
		t.Fatal("expected event")
	}
	if event.Tool != "Bash" {
		t.Errorf("Tool: got %q want Bash", event.Tool)
	}
	if event.Status != agentruntime.StatusWorking {
		t.Errorf("Status: got %q", event.Status)
	}
	if event.Metadata["turn_id"] != "turn-1" {
		t.Errorf("Metadata turn_id: got %q", event.Metadata["turn_id"])
	}
}

func TestHookCommandFallsBackToNativeIDWhenCallerIDMissing(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available")
	}

	var receivedPayload []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPayload, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	adapter := New(DefaultOptions())

	hookStdin := `{"hook_event_name":"Stop","session_id":"native-fallback-1"}`

	hc := HookCommand(server.URL)

	cmd := exec.Command("sh", "-c", hc.Command)
	cmd.Env = os.Environ()
	cmd.Stdin = strings.NewReader(hookStdin)

	if err := cmd.Run(); err != nil {
		t.Fatalf("hook command failed: %v", err)
	}

	event, err := adapter.NormalizeEvent(t.Context(), receivedPayload)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if event == nil {
		t.Fatal("expected event")
	}
	if event.ID != "native-fallback-1" {
		t.Errorf("ID: got %q want native-fallback-1", event.ID)
	}
}
