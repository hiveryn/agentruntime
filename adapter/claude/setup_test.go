package claude

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hiveryn/agentruntime"
)

func TestEnsureSetupInstallsAndUpdatesMarkerScopedHooks(t *testing.T) {
	adapter := New(DefaultOptions())
	root := t.TempDir()

	req := agentruntime.SetupRequest{
		Marker:     "hiveryn",
		ConfigRoot: root,
		Hook: agentruntime.HookCommand{
			Command:       "hiveryn-claude-hook --endpoint http://127.0.0.1:9000/hook",
			Timeout:       12 * time.Second,
			StatusMessage: "Hiveryn status capture",
		},
	}

	result, err := adapter.EnsureSetup(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed {
		t.Fatal("expected initial setup to change settings file")
	}

	path := filepath.Join(root, "settings.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"SessionEnd", "StopFailure", "agentruntimeMarker", "Hiveryn status capture"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("settings file missing %q\n%s", want, data)
		}
	}

	req.Hook.Command = "hiveryn-claude-hook --endpoint http://127.0.0.1:9001/hook"
	result, err = adapter.EnsureSetup(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed {
		t.Fatal("expected command update to change settings file")
	}

	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), "agentruntimeMarker") != len(hookEvents) {
		t.Fatalf("expected one marker per hook event\n%s", data)
	}
	if strings.Contains(string(data), "9000") || !strings.Contains(string(data), "9001") {
		t.Fatalf("unexpected command contents\n%s", data)
	}
}

// TestEnsureSetupCollapsesStrippedMarkerDuplicates simulates the real-world bug:
// Claude rewrites settings.json and drops the agentruntimeMarker field, so prior
// runs left many duplicate hook groups carrying the real signature-bearing command
// but no marker. EnsureSetup must self-heal them down to one group per event.
func TestEnsureSetupCollapsesStrippedMarkerDuplicates(t *testing.T) {
	adapter := New(DefaultOptions())
	root := t.TempDir()
	path := filepath.Join(root, "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}

	hook := HookCommand("http://127.0.0.1:4201/internal/agentruntime")
	if !strings.Contains(hook.Command, managedHookSignature) {
		t.Fatalf("expected generated command to contain signature %q", managedHookSignature)
	}

	// Seed 50 duplicate groups per event, each WITHOUT a marker field (stripped),
	// mimicking a bloated config that grew unbounded across launches.
	hooks := map[string]any{}
	for _, event := range hookEvents {
		groups := make([]any, 0, 50)
		for range 50 {
			groups = append(groups, map[string]any{
				"matcher": "",
				"hooks": []any{map[string]any{
					"type":          "command",
					"command":       hook.Command,
					"timeout":       10,
					"statusMessage": "agentruntime claude hook",
				}},
			})
		}
		hooks[event] = groups
	}
	seed, err := json.Marshal(map[string]any{"hooks": hooks})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, seed, 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := adapter.EnsureSetup(context.Background(), agentruntime.SetupRequest{
		Marker:     "hiveryn-daemon",
		ConfigRoot: root,
		Hook:       hook,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed {
		t.Fatal("expected collapsing duplicates to change settings file")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := countHookEntries(t, data); got != len(hookEvents) {
		t.Fatalf("expected exactly one hook entry per event (%d), got %d\n%s", len(hookEvents), got, data)
	}
	if strings.Count(string(data), markerField) != len(hookEvents) {
		t.Fatalf("expected the fresh hooks to carry the marker field\n%s", data)
	}
}

// countHookEntries totals the individual command entries across all events in a
// settings/hooks file, so duplicate groups can be detected regardless of marker.
func countHookEntries(t *testing.T, data []byte) int {
	t.Helper()
	var root struct {
		Hooks map[string][]struct {
			Hooks []map[string]any `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, groups := range root.Hooks {
		for _, g := range groups {
			total += len(g.Hooks)
		}
	}
	return total
}

func TestRemoveSetupOnlyRemovesMatchingMarker(t *testing.T) {
	adapter := New(DefaultOptions())
	root := t.TempDir()
	path := filepath.Join(root, "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}

	seed := `{
  "hooks": {
    "SessionStart": [
      {
        "matcher": "",
        "hooks": [
          {
            "type": "command",
            "command": "keep-me",
            "timeout": 10,
            "statusMessage": "keep",
            "agentruntimeMarker": "someone-else"
          },
          {
            "type": "command",
            "command": "remove-me",
            "timeout": 10,
            "statusMessage": "remove",
            "agentruntimeMarker": "hiveryn"
          }
        ]
      }
    ]
  }
}`
	if err := os.WriteFile(path, []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := adapter.RemoveSetup(context.Background(), agentruntime.SetupRequest{
		Marker:     "hiveryn",
		ConfigRoot: root,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed {
		t.Fatal("expected removal to change settings file")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "remove-me") {
		t.Fatalf("expected matching marker to be removed\n%s", data)
	}
	if !strings.Contains(string(data), "keep-me") {
		t.Fatalf("expected non-matching marker to remain\n%s", data)
	}
}
