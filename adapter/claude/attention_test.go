package claude

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hiveryn/agentruntime"
)

var _ agentruntime.AttentionDetector = (*Adapter)(nil)

// The screens are rendered from terminal output captured from a real Claude
// Code 2.1.281 first launch in an isolated config dir (paths anonymized).
func TestInspectScreen(t *testing.T) {
	adapter := New(DefaultOptions())
	for fixture, want := range map[string]string{
		"screen_folder_trust.txt":       AttentionFolderTrust,
		"screen_bypass_permissions.txt": AttentionBypassPermissions,
	} {
		got := adapter.InspectScreen(readScreen(t, fixture))
		if got == nil || got.Reason != want || got.Message == "" {
			t.Fatalf("%s: expected %s, got %+v", fixture, want, got)
		}
	}
}

func TestInspectScreenIgnoresOtherScreens(t *testing.T) {
	adapter := New(DefaultOptions())
	// The trust dialog's text left above the prompt after it was answered.
	answered := append(readScreen(t, "screen_folder_trust.txt"), "", "> ", "  ? for shortcuts")
	if got := adapter.InspectScreen(answered); got != nil {
		t.Fatalf("expected no attention, got %+v", got)
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
