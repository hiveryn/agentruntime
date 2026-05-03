package claude

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
	if hc.StatusMessage != "agentruntime claude hook" {
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
		`agent:"claude"`,
		"http://127.0.0.1:9000/claude",
	} {
		if !strings.Contains(cmd, want) {
			t.Errorf("command missing %q\n%s", want, cmd)
		}
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

	sessionID := "test-claude-session-42"
	hookStdin := `{"hook_event_name":"SessionStart","session_id":"native-abc-123","source":"startup","model":"test-model","cwd":"/tmp/test"}`

	hc := HookCommand(server.URL)

	cmd := exec.Command("sh", "-c", hc.Command)
	cmd.Env = append(os.Environ(), "AGENTRUNTIME_SESSION_ID="+sessionID)
	cmd.Stdin = strings.NewReader(hookStdin)

	if err := cmd.Run(); err != nil {
		t.Fatalf("hook command failed: %v", err)
	}

	if receivedPath != "/claude" {
		t.Errorf("path: got %q want /claude", receivedPath)
	}

	var envelope map[string]any
	if err := json.Unmarshal(receivedPayload, &envelope); err != nil {
		t.Fatalf("decode envelope: %v\nbody: %s", err, receivedPayload)
	}

	if agent, _ := envelope["agent"].(string); agent != "claude" {
		t.Errorf("agent: got %q want claude", agent)
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
	sessionID := "correlate-claude-1"

	hookStdin := `{"hook_event_name":"SessionStart","session_id":"native-claude-456","source":"startup"}`

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
	if event.NativeID != "native-claude-456" {
		t.Errorf("NativeID: got %q", event.NativeID)
	}
	if event.Agent != agentruntime.AgentClaude {
		t.Errorf("Agent: got %q", event.Agent)
	}
	if event.Status != agentruntime.StatusStarting {
		t.Errorf("Status: got %q", event.Status)
	}
	if event.NativeSessionRole != agentruntime.NativeSessionRolePrimary {
		t.Errorf("NativeSessionRole: got %q want primary", event.NativeSessionRole)
	}
	if got, _ := event.Raw["agent"].(string); got != "claude" {
		t.Errorf("Raw agent: got %q", got)
	}
}

func TestHookCommandPermissionRequestAwaitingInput(t *testing.T) {
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

	hookStdin := `{"hook_event_name":"PermissionRequest","session_id":"native-claude-789","tool_name":"ApplyPatch","permission_mode":"default"}`

	hc := HookCommand(server.URL)

	cmd := exec.Command("sh", "-c", hc.Command)
	cmd.Env = append(os.Environ(), "AGENTRUNTIME_SESSION_ID=correlate-claude-2")
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
	if event.Status != agentruntime.StatusAwaitingInput {
		t.Errorf("Status: got %q want awaiting_input", event.Status)
	}
	if event.Tool != "ApplyPatch" {
		t.Errorf("Tool: got %q", event.Tool)
	}
}

func TestHookCommandStopFailureMapsToError(t *testing.T) {
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

	hookStdin := `{"hook_event_name":"StopFailure","session_id":"native-claude-999","error":"rate_limit"}`

	hc := HookCommand(server.URL)

	cmd := exec.Command("sh", "-c", hc.Command)
	cmd.Env = append(os.Environ(), "AGENTRUNTIME_SESSION_ID=correlate-claude-3")
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
	if event.Status != agentruntime.StatusError {
		t.Errorf("Status: got %q want error", event.Status)
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
