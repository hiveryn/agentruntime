package codex

import (
	"context"
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
			Command:       "hiveryn-codex-hook --endpoint http://127.0.0.1:9000/hook",
			Timeout:       12 * time.Second,
			StatusMessage: "Hiveryn status capture",
		},
	}

	result, err := adapter.EnsureSetup(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed {
		t.Fatal("expected initial setup to change hooks file")
	}

	path := filepath.Join(root, "hooks.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"SessionStart", "PermissionRequest", "agentruntimeMarker", "Hiveryn status capture"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("hooks file missing %q\n%s", want, data)
		}
	}

	req.Hook.Command = "hiveryn-codex-hook --endpoint http://127.0.0.1:9001/hook"
	result, err = adapter.EnsureSetup(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed {
		t.Fatal("expected command update to change hooks file")
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

func TestRemoveSetupOnlyRemovesMatchingMarker(t *testing.T) {
	adapter := New(DefaultOptions())
	root := t.TempDir()
	path := filepath.Join(root, "hooks.json")

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
		t.Fatal("expected removal to change hooks file")
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
