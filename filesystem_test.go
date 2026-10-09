package agentruntime_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	ar "github.com/hiveryn/agentruntime"
	"github.com/hiveryn/agentruntime/adapter/claude"
	"github.com/hiveryn/agentruntime/adapter/codex"
	"github.com/hiveryn/agentruntime/adapter/opencode"
)

type targetFS struct {
	files    map[string][]byte
	sequence int
}

func (f *targetFS) ReadFile(p string) ([]byte, error) {
	b, ok := f.files[p]
	if !ok {
		return nil, os.ErrNotExist
	}
	return b, nil
}
func (f *targetFS) WriteFile(p string, b []byte, _ os.FileMode) error {
	if !strings.HasPrefix(p, "/target/") {
		return fmt.Errorf("not a target path: %s", p)
	}
	f.files[p] = b
	return nil
}
func (*targetFS) MkdirAll(string, os.FileMode) error { return nil }
func (f *targetFS) Remove(p string) error            { delete(f.files, p); return nil }
func (*targetFS) UserHomeDir() (string, error)       { return "/target/home", nil }
func (*targetFS) Getenv(string) string               { return "" }
func (f *targetFS) WriteTemp(pattern string, b []byte) (string, error) {
	f.sequence++
	p := fmt.Sprintf("/target/tmp/%d-%s", f.sequence, pattern)
	f.files[p] = b
	return p, nil
}

func TestProviderPreparationUsesTargetFilesystem(t *testing.T) {
	adapters := []ar.Adapter{claude.New(claude.DefaultOptions()), codex.New(codex.DefaultOptions()), opencode.New(opencode.DefaultOptions())}
	for _, adapter := range adapters {
		t.Run(string(adapter.Agent()), func(t *testing.T) {
			fs := &targetFS{files: map[string][]byte{}}
			hook := claude.HookCommand()
			if adapter.Agent() == ar.AgentCodex {
				hook = codex.HookCommand()
			}
			setup := ar.SetupRequest{Marker: "fixture", Hook: hook, FileSystem: fs}
			result, err := adapter.EnsureSetup(context.Background(), setup)
			if err != nil {
				t.Fatal(err)
			}
			if !result.Changed || len(fs.files) != 1 {
				t.Fatalf("setup: %+v files %v", result, fs.files)
			}
			for _, p := range result.Paths {
				if !strings.HasPrefix(p, "/target/home/") {
					t.Fatal(p)
				}
			}
			spec, err := adapter.PrepareLaunch(context.Background(), ar.StartRequest{ID: "worker", Agent: adapter.Agent(), Workdir: "/target/repo", FileSystem: fs, Instructions: "target instructions", MCPServers: []ar.MCPServerConfig{{Name: "fixture", URL: "http://127.0.0.1:34567/mcp", BearerTokenEnvVar: "TOKEN"}}})
			if err != nil {
				t.Fatal(err)
			}
			for _, p := range spec.CleanupPaths {
				if !strings.HasPrefix(p, "/target/tmp/") {
					t.Fatal(p)
				}
				if _, ok := fs.files[p]; !ok {
					t.Fatalf("artifact missing: %s", p)
				}
			}
			if _, err := adapter.RemoveSetup(context.Background(), setup); err != nil {
				t.Fatal(err)
			}
		})
	}
}
