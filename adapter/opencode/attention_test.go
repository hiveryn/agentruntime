package opencode

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hiveryn/agentruntime"
)

var _ agentruntime.AttentionDetector = (*Adapter)(nil)

// The screens are rendered from terminal output captured from real OpenCode
// 1.17.18 sessions in isolated XDG dirs (paths anonymized).
func TestInspectScreenRecognizesNothing(t *testing.T) {
	a := New(DefaultOptions())
	for _, fixture := range []string{
		// The startup update modal does not block the agent: the initial
		// prompt ran a tool and finished while it was shown.
		"screen_update_available.txt",
		// Resumed with --session after being killed mid-tool: the tool is
		// still drawn as running, but OpenCode shows no interruption notice.
		"screen_resume_interrupted.txt",
	} {
		if got := a.InspectScreen(readScreen(t, fixture)); got != nil {
			t.Fatalf("%s: expected nil, got %+v", fixture, got)
		}
	}
	if a.AttentionCoverage() == "" {
		t.Fatal("expected a coverage statement")
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
