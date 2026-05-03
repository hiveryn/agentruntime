# agentruntime

Reusable Go primitives for launching agent CLIs and consuming their hook events.

Current first-class adapters: `codex`, `claude`, and `opencode`.

## Status

`v0.x.y` - pre-v1. APIs may change while the module is being exercised by downstream integrations.

## Examples

### Codex

```go
package main

import (
	"context"
	"log"
	"net/http"

	"github.com/hiveryn/agentruntime"
	"github.com/hiveryn/agentruntime/adapter/codex"
	"github.com/hiveryn/agentruntime/ingest"
)

func main() {
	ctx := context.Background()
	adapter := codex.New(codex.DefaultOptions())
	receiver := ingest.NewReceiver(adapter)

	sub := receiver.Hub().Subscribe(ingest.Filter{ID: "session-1"})
	defer sub.Close()
	go func() {
		for event := range sub.Events {
			log.Printf("status=%s role=%s native=%s tool=%s", event.Status, event.NativeSessionRole, event.NativeID, event.Tool)
		}
	}()

	mux := http.NewServeMux()
	mux.Handle("/hook", receiver.Handler(agentruntime.AgentCodex))
	go func() {
		log.Fatal(http.ListenAndServe("127.0.0.1:9000", mux))
	}()

	_, err := adapter.EnsureSetup(ctx, agentruntime.SetupRequest{
		Agents: []agentruntime.AgentKind{agentruntime.AgentCodex},
		Marker: "example",
		Hook: agentruntime.HookCommand{
			Command: "curl -s -X POST --data-binary @- http://127.0.0.1:9000/hook",
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	spec, err := adapter.PrepareLaunch(ctx, agentruntime.StartRequest{
		ID:           "session-1",
		Agent:        agentruntime.AgentCodex,
		Workdir:      "/tmp/work",
		Instructions: "Be concise and prefer bullet points.",
		Prompt:       "Summarize this repository.",
	})
	if err != nil {
		log.Fatal(err)
	}

	_ = spec // Run this command in your own process or pty manager.
}
```

### Claude Code

```go
package main

import (
	"context"
	"log"
	"net/http"

	"github.com/hiveryn/agentruntime"
	"github.com/hiveryn/agentruntime/adapter/claude"
	"github.com/hiveryn/agentruntime/ingest"
)

func main() {
	ctx := context.Background()
	adapter := claude.New(claude.DefaultOptions())
	receiver := ingest.NewReceiver(adapter)

	sub := receiver.Hub().Subscribe(ingest.Filter{ID: "session-1"})
	defer sub.Close()
	go func() {
		for event := range sub.Events {
			log.Printf("status=%s role=%s native=%s tool=%s", event.Status, event.NativeSessionRole, event.NativeID, event.Tool)
		}
	}()

	mux := http.NewServeMux()
	mux.Handle("/hook", receiver.Handler(agentruntime.AgentClaude))
	go func() {
		log.Fatal(http.ListenAndServe("127.0.0.1:9000", mux))
	}()

	// EnsureSetup writes hook entries into ~/.claude/settings.json scoped to
	// the given marker — safe to call on every startup (idempotent).
	_, err := adapter.EnsureSetup(ctx, agentruntime.SetupRequest{
		Agents: []agentruntime.AgentKind{agentruntime.AgentClaude},
		Marker: "example",
		Hook: agentruntime.HookCommand{
			Command: "curl -s -X POST --data-binary @- http://127.0.0.1:9000/hook",
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	spec, err := adapter.PrepareLaunch(ctx, agentruntime.StartRequest{
		ID:           "session-1",
		Agent:        agentruntime.AgentClaude,
		Workdir:      "/tmp/work",
		Instructions: "Be concise and prefer bullet points.",
		Prompt:       "Summarize this repository.",
		// Optional: attach stdio or HTTP MCP servers.
		MCPServers: []agentruntime.MCPServerConfig{{
			Name:    "my-tools",
			Command: "my-mcp-server",
			Args:    []string{"--port", "8080"},
		}},
	})
	if err != nil {
		log.Fatal(err)
	}

	_ = spec // Run this command in your own process or pty manager.
	// Remove spec.CleanupPaths after the agent process exits (MCP config files).
}
```

### OpenCode

```go
package main

import (
	"context"
	"log"
	"net/http"

	"github.com/hiveryn/agentruntime"
	"github.com/hiveryn/agentruntime/adapter/opencode"
	"github.com/hiveryn/agentruntime/ingest"
)

func main() {
	ctx := context.Background()
	adapter := opencode.New(opencode.DefaultOptions())
	receiver := ingest.NewReceiver(adapter)

	sub := receiver.Hub().Subscribe(ingest.Filter{ID: "session-1"})
	defer sub.Close()
	go func() {
		for event := range sub.Events {
			log.Printf("status=%s role=%s native=%s tool=%s", event.Status, event.NativeSessionRole, event.NativeID, event.Tool)
		}
	}()

	mux := http.NewServeMux()
	// OpenCode plugin POSTs to /opencode — mount the handler there.
	mux.Handle("/opencode", receiver.Handler(agentruntime.AgentOpenCode))
	go func() {
		log.Fatal(http.ListenAndServe("127.0.0.1:9000", mux))
	}()

	// EnsureSetup writes a marker-scoped TypeScript plugin into
	// ~/.config/opencode/plugins/ — safe to call on every startup (idempotent).
	_, err := adapter.EnsureSetup(ctx, agentruntime.SetupRequest{
		Agents: []agentruntime.AgentKind{agentruntime.AgentOpenCode},
		Marker: "example",
		Hook: agentruntime.HookCommand{
			Command: "http://127.0.0.1:9000",
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	spec, err := adapter.PrepareLaunch(ctx, agentruntime.StartRequest{
		ID:           "session-1",
		Agent:        agentruntime.AgentOpenCode,
		Workdir:      "/tmp/work",
		Instructions: "Be concise and prefer bullet points.",
		Prompt:       "Summarize this repository.",
		// Optional: attach stdio or HTTP MCP servers.
		MCPServers: []agentruntime.MCPServerConfig{{
			Name:    "my-tools",
			Command: "my-mcp-server",
			Args:    []string{"--port", "8080"},
		}},
	})
	if err != nil {
		log.Fatal(err)
	}

	_ = spec // Run this command in your own process or pty manager.
	// Remove spec.CleanupPaths after the agent process exits (instructions files).
}
```
