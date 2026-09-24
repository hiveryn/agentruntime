package opencode

import (
	"testing"

	"github.com/hiveryn/agentruntime"
)

var _ agentruntime.AttentionDetector = (*Adapter)(nil)

func TestInspectScreenRecognizesNothing(t *testing.T) {
	a := New(DefaultOptions())
	if got := a.InspectScreen([]string{"Update Available", "Skip Confirm"}); got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
	if a.AttentionCoverage() == "" {
		t.Fatal("expected a coverage statement")
	}
}
