package claude

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hiveryn/agentruntime"
)

var _ agentruntime.AttentionDetector = (*Adapter)(nil)

// The screens are rendered from terminal output captured from real Claude
// Code 2.1.281 sessions in an isolated config dir (paths anonymized): a first
// launch, and conversations resumed with --resume after the process was
// killed mid-turn.
func TestInspectScreen(t *testing.T) {
	cases := []struct {
		fixture string
		want    string // attention reason, "" for none
	}{
		{"screen_folder_trust.txt", AttentionFolderTrust},
		{"screen_bypass_permissions.txt", AttentionBypassPermissions},
		// Killed during a foreground tool call, then resumed.
		{"screen_resume_interrupted.txt", AttentionConversationInterrupted},
		// The same, while the composer's transient effort hint is still shown.
		{"screen_resume_interrupted_effort_hint.txt", AttentionConversationInterrupted},
		// A message typed but not yet sent has not answered the notice.
		{"screen_resume_interrupted_typing.txt", AttentionConversationInterrupted},
		// Once sent, the notice is left in history and must not match.
		{"screen_resume_interrupted_answered.txt", ""},
		// Killed while streaming a reply: Claude drops the partial reply and
		// shows no notice, so the wait is not detectable.
		{"screen_resume_after_streaming_kill.txt", ""},
		// Killed with a background command running: the stopped-command notice
		// is informational, not an interruption prompt.
		{"screen_resume_background_shell_stopped.txt", ""},
		// A quiet, working agent is not waiting.
		{"screen_working.txt", ""},
	}
	adapter := New(DefaultOptions())
	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			got := adapter.InspectScreen(readScreen(t, tc.fixture))
			switch {
			case tc.want == "" && got != nil:
				t.Fatalf("expected no attention, got %+v", got)
			case tc.want != "" && (got == nil || got.Reason != tc.want || got.Message == ""):
				t.Fatalf("expected %s, got %+v", tc.want, got)
			}
		})
	}
}

func TestInspectScreenIgnoresOtherScreens(t *testing.T) {
	adapter := New(DefaultOptions())
	// The trust dialog's text left above the prompt after it was answered.
	answered := append(readScreen(t, "screen_folder_trust.txt"), "", "> ", "  ? for shortcuts")
	if got := adapter.InspectScreen(answered); got != nil {
		t.Fatalf("expected no attention, got %+v", got)
	}
	// An agent message quoting the interrupted notice above a working composer.
	quoted := append([]string{"⏺ Claude shows:", "  ⎿  Interrupted · What should Claude do instead?", "⏺ continuing"}, readScreen(t, "screen_working.txt")...)
	if got := adapter.InspectScreen(quoted); got != nil {
		t.Fatalf("expected no attention for quoted text, got %+v", got)
	}
	if got := adapter.InspectScreen([]string{"", ""}); got != nil {
		t.Fatalf("expected no attention on a blank screen, got %+v", got)
	}
}

func readScreen(t *testing.T, name string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimRight(string(data), "\n"), "\n")
}
