package codex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hiveryn/agentruntime"
)

var _ agentruntime.AttentionDetector = (*Adapter)(nil)

// The screens are rendered from terminal output captured from real Codex CLI
// 0.156.1 sessions (paths anonymized).
func TestInspectScreen(t *testing.T) {
	cases := []struct {
		fixture string
		want    string // attention reason, "" for none
	}{
		{"screen_folder_trust.txt", AttentionFolderTrust},
		{"screen_resume_interrupted.txt", AttentionConversationInterrupted},
		// A message typed but not yet sent has not answered the banner.
		{"screen_resume_interrupted_typing.txt", AttentionConversationInterrupted},
		// Once sent, the banner is left in history and must not match.
		{"screen_resume_interrupted_sent_working.txt", ""},
		{"screen_resume_interrupted_answered.txt", ""},
		// A quiet, working agent is not waiting.
		{"screen_working.txt", ""},
		// Approval prompts are reported by the PermissionRequest hook.
		{"screen_approval.txt", ""},
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

func TestInspectScreenIgnoresQuotedDialogText(t *testing.T) {
	lines := readScreen(t, "screen_working.txt")
	// An agent message quoting the trust dialog above a working composer.
	quoted := append([]string{"• Folder access", "  Trust this folder?", "› 1. Trust and continue", "  enter continue · esc quit", "■ Conversation interrupted - tell the model what to do differently.", "• continuing"}, lines...)
	if got := New(DefaultOptions()).InspectScreen(quoted); got != nil {
		t.Fatalf("expected no attention, got %+v", got)
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
