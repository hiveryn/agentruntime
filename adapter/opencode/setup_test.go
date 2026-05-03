package opencode

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hiveryn/agentruntime"
)

func baseCfg(t *testing.T) agentruntime.SetupRequest {
	t.Helper()
	return agentruntime.SetupRequest{
		Marker: "test-marker",
		Hook: agentruntime.HookCommand{
			Command: "http://127.0.0.1:9000",
		},
	}
}

func pluginPathForTest(configRoot, marker string) string {
	return filepath.Join(configRoot, ".config", "opencode", "plugins", "agentruntime-"+marker+".ts")
}

func TestEnsureSetup_EmptyDir(t *testing.T) {
	a := New(DefaultOptions())
	root := t.TempDir()
	req := baseCfg(t)
	req.ConfigRoot = root

	res, err := a.EnsureSetup(context.Background(), req)
	if err != nil {
		t.Fatalf("EnsureSetup: %v", err)
	}
	if !res.Changed {
		t.Error("expected Changed=true on first install")
	}
	if len(res.Paths) == 0 {
		t.Error("expected non-empty Paths")
	}

	path := pluginPathForTest(root, req.Marker)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read plugin: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, managedMarker) {
		t.Error("plugin missing managed marker")
	}
	if !strings.Contains(content, "@agentruntime-marker: test-marker") {
		t.Error("plugin missing agentruntime-marker")
	}
	if !strings.Contains(content, "http://127.0.0.1:9000/opencode") {
		t.Error("plugin missing endpoint URL")
	}
	if !strings.Contains(content, "AGENTRUNTIME_SESSION_ID") {
		t.Error("plugin missing AGENTRUNTIME_SESSION_ID reference")
	}
}

func TestEnsureSetup_Idempotent(t *testing.T) {
	a := New(DefaultOptions())
	root := t.TempDir()
	req := baseCfg(t)
	req.ConfigRoot = root

	if _, err := a.EnsureSetup(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	res, err := a.EnsureSetup(context.Background(), req)
	if err != nil {
		t.Fatalf("second EnsureSetup: %v", err)
	}
	if res.Changed {
		t.Error("expected Changed=false on idempotent re-install")
	}
}

func TestEnsureSetup_EndpointChanged(t *testing.T) {
	a := New(DefaultOptions())
	root := t.TempDir()
	req := baseCfg(t)
	req.ConfigRoot = root

	if _, err := a.EnsureSetup(context.Background(), req); err != nil {
		t.Fatal(err)
	}

	req.Hook.Command = "http://127.0.0.1:9001"
	res, err := a.EnsureSetup(context.Background(), req)
	if err != nil {
		t.Fatalf("EnsureSetup with new endpoint: %v", err)
	}
	if !res.Changed {
		t.Error("expected Changed=true when endpoint changed")
	}

	path := pluginPathForTest(root, req.Marker)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "http://127.0.0.1:9001/opencode") {
		t.Error("plugin not updated with new endpoint")
	}
}

func TestEnsureSetup_RefusesNonManaged(t *testing.T) {
	a := New(DefaultOptions())
	root := t.TempDir()
	req := baseCfg(t)
	req.ConfigRoot = root

	path := pluginPathForTest(root, req.Marker)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("// some user plugin"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := a.EnsureSetup(context.Background(), req)
	if err == nil {
		t.Error("expected error when non-managed file exists")
	}
}

func TestEnsureSetup_AgentFilter(t *testing.T) {
	a := New(DefaultOptions())
	root := t.TempDir()
	req := baseCfg(t)
	req.ConfigRoot = root
	req.Agents = []agentruntime.AgentKind{agentruntime.AgentClaude}

	res, err := a.EnsureSetup(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Error("expected no-op when agents filter excludes opencode")
	}
	path := pluginPathForTest(root, req.Marker)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("plugin file should not exist when filtered out")
	}
}

func TestEnsureSetup_MissingMarker(t *testing.T) {
	a := New(DefaultOptions())
	root := t.TempDir()
	req := baseCfg(t)
	req.ConfigRoot = root
	req.Marker = ""

	_, err := a.EnsureSetup(context.Background(), req)
	if err == nil {
		t.Error("expected error for missing marker")
	}
}

func TestEnsureSetup_MissingCommand(t *testing.T) {
	a := New(DefaultOptions())
	root := t.TempDir()
	req := baseCfg(t)
	req.ConfigRoot = root
	req.Hook.Command = ""

	_, err := a.EnsureSetup(context.Background(), req)
	if err == nil {
		t.Error("expected error for missing hook command")
	}
}

func TestRemoveSetup_Removes(t *testing.T) {
	a := New(DefaultOptions())
	root := t.TempDir()
	req := baseCfg(t)
	req.ConfigRoot = root

	if _, err := a.EnsureSetup(context.Background(), req); err != nil {
		t.Fatal(err)
	}

	res, err := a.RemoveSetup(context.Background(), req)
	if err != nil {
		t.Fatalf("RemoveSetup: %v", err)
	}
	if !res.Changed {
		t.Error("expected Changed=true on removal")
	}

	path := pluginPathForTest(root, req.Marker)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("plugin file should not exist after removal")
	}
}

func TestRemoveSetup_NotFound(t *testing.T) {
	a := New(DefaultOptions())
	root := t.TempDir()
	req := baseCfg(t)
	req.ConfigRoot = root

	res, err := a.RemoveSetup(context.Background(), req)
	if err != nil {
		t.Fatalf("RemoveSetup on absent file: %v", err)
	}
	if res.Changed {
		t.Error("expected Changed=false when file absent")
	}
}

func TestRemoveSetup_RefusesNonManaged(t *testing.T) {
	a := New(DefaultOptions())
	root := t.TempDir()
	req := baseCfg(t)
	req.ConfigRoot = root

	path := pluginPathForTest(root, req.Marker)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("// user plugin"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := a.RemoveSetup(context.Background(), req)
	if err == nil {
		t.Error("expected error when non-managed file exists")
	}
}
