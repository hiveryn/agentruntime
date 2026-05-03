# agentruntime

Reusable Go primitives for setting up agent CLI hooks/plugins, preparing agent
process launch specs, and normalizing the hook events those agents emit.

Current first-class adapters: `codex`, `claude`, and `opencode`.

## Status

`v0.x.y` - pre-v1. APIs may change while the module is being exercised by
downstream integrations.

## What This Library Does

Use `agentruntime` when you want a library to answer two startup questions for
supported agent CLIs:

- What persistent hook/plugin setup is required so this agent reports events?
- What command, args, env, workdir, and temp files are required to start this
  agent session?

The library returns specs. It does not run or supervise processes.

## Boundary

`agentruntime` owns:

- Marker-scoped hook/plugin setup and removal.
- Launch specs for supported agent CLIs.
- Native hook/plugin event normalization.
- In-process ingest helpers and an optional local HTTP handler.

Callers own process execution and lifecycle, PTYs or terminal rendering,
persistence, auth, product workflow, and any UI event stream.

## Setup And Launch

1. Create an adapter from `adapter/codex`, `adapter/claude`, or
   `adapter/opencode`.
2. Call `EnsureSetup` with a stable `SetupRequest.Marker` and hook target.
3. Call `PrepareLaunch` with a `StartRequest`.
4. Execute the returned `LaunchSpec` with your own process or PTY manager.
5. Remove every path in `LaunchSpec.CleanupPaths` after that process exits.

`EnsureSetup` is persistent setup and is safe to call on startup. It is
idempotent for the same marker and hook target. It writes:

- Codex: `~/.codex/hooks.json`
- Claude Code: `~/.claude/settings.json`
- OpenCode: `~/.config/opencode/plugins/agentruntime-<marker>.ts`

Codex and Claude Code setup use `codex.HookCommand(endpoint)` /
`claude.HookCommand(endpoint)` to generate canonical hook commands that post
enveloped native hook JSON to your receiver. OpenCode setup uses
`HookCommand.Endpoint`; the generated plugin POSTs to `<endpoint>/opencode`.

Call `RemoveSetup` when your integration is disabled, uninstalled, or changing
markers. It removes the persistent marker-scoped hook/plugin setup. It is not
part of normal per-launch cleanup.

`LaunchSpec.CleanupPaths` is per-launch cleanup. Adapters use it for temporary
files created by `PrepareLaunch`, such as Claude Code MCP config files or
OpenCode instruction files.

## Launch Inputs

`StartRequest` is intentionally small:

- `ID`: caller-owned session/correlation key. Normalized events use this as
  `Event.ID`.
- `Agent`: optional adapter guard, for example `agentruntime.AgentCodex`.
- `Command`: executable override. If empty, the adapter uses its default CLI
  name.
- `Args`: ordinary runtime CLI arguments to append after synthesized arguments.
- `Env`: optional additional environment for the launched process.
- `Workdir`: required working directory.
- `Prompt`: initial prompt when the runtime supports it.
- `Instructions`: runtime-specific instruction/system-prompt input.
- `MCPServers`: stdio or HTTP MCP servers the adapter must synthesize into the
  runtime's supported config shape.
- `OpenCodeProfile`: OpenCode only. Selects the OpenCode agent profile
  (`--agent <profile>`). Leave empty to use the OpenCode default.

Ordinary runtime options should be passed through `Command`, `Args`, and `Env`.
The library only models behavior it must synthesize for portability, such as
MCP config, instruction injection, session correlation, and hook/plugin setup.

Some keys and arguments are adapter-managed. `AGENTRUNTIME_SESSION_ID` is
generated into `LaunchSpec.Env` for all adapters and must not conflict with
`StartRequest.ID`. OpenCode also manages `OPENCODE_CONFIG_CONTENT`.
Adapter-managed CLI arguments, such as Claude Code's `--session-id` or
OpenCode's `--prompt` and `--agent`, are rejected when passed through `Args`.

When you execute a `LaunchSpec`, include `LaunchSpec.Env` in the child process
environment. It contains generated correlation/config values even when
`StartRequest.Env` is empty.

## Event Ingest

Adapters normalize native payloads into `Event`. `Event.ID` is caller-owned;
`Event.NativeID` is the runtime-native session ID. `ingest.Receiver` tracks the
first native session seen for each caller ID as `PrimaryNativeID` and classifies
events as `primary` or `subsession` when possible.

`Event.Status` is scoped to the native session that emitted the event. For
example, Codex `Stop` and OpenCode `session.idle` mean that turn is idle; they
do not mean the process exited.

Use `Receiver.Handler(agent)` for the convenience HTTP path, or call
`Receiver.Ingest(ctx, agent, data)` with raw hook bytes. Subscribe to normalized
events through `receiver.Hub().Subscribe(ingest.Filter{...})`. If you already
have your own ingest pipeline, call `adapter.NormalizeEvent(ctx, data)`
directly.

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
	mux.Handle("/codex", receiver.Handler(agentruntime.AgentCodex))
	go func() {
		log.Fatal(http.ListenAndServe("127.0.0.1:9000", mux))
	}()

	_, err := adapter.EnsureSetup(ctx, agentruntime.SetupRequest{
		Marker: "example",
		Hook:  codex.HookCommand("http://127.0.0.1:9000"),
	})
	if err != nil {
		log.Fatal(err)
	}

	spec, err := adapter.PrepareLaunch(ctx, agentruntime.StartRequest{
		ID:           "session-1",
		Agent:        agentruntime.AgentCodex,
		Env:          map[string]string{},
		Workdir:      "/tmp/work",
		Instructions: "Be concise and prefer bullet points.",
		Prompt:       "Summarize this repository.",
	})
	if err != nil {
		log.Fatal(err)
	}

	_ = spec // Run spec.Command, spec.Args, spec.Env, and spec.Workdir.
	// Remove spec.CleanupPaths after the agent process exits.
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
	mux.Handle("/claude", receiver.Handler(agentruntime.AgentClaude))
	go func() {
		log.Fatal(http.ListenAndServe("127.0.0.1:9000", mux))
	}()

	_, err := adapter.EnsureSetup(ctx, agentruntime.SetupRequest{
		Marker: "example",
		Hook:  claude.HookCommand("http://127.0.0.1:9000"),
	})
	if err != nil {
		log.Fatal(err)
	}

	spec, err := adapter.PrepareLaunch(ctx, agentruntime.StartRequest{
		ID:           "session-1",
		Agent:        agentruntime.AgentClaude,
		Env:          map[string]string{},
		Workdir:      "/tmp/work",
		Instructions: "Be concise and prefer bullet points.",
		Prompt:       "Summarize this repository.",
		MCPServers: []agentruntime.MCPServerConfig{{
			Name:    "my-tools",
			Command: "my-mcp-server",
			Args:    []string{"--port", "8080"},
		}},
	})
	if err != nil {
		log.Fatal(err)
	}

	_ = spec // Run spec.Command, spec.Args, spec.Env, and spec.Workdir.
	// Remove spec.CleanupPaths after the agent process exits.
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
	mux.Handle("/opencode", receiver.Handler(agentruntime.AgentOpenCode))
	go func() {
		log.Fatal(http.ListenAndServe("127.0.0.1:9000", mux))
	}()

	_, err := adapter.EnsureSetup(ctx, agentruntime.SetupRequest{
		Marker: "example",
		Hook: agentruntime.HookCommand{
			Endpoint: "http://127.0.0.1:9000",
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	spec, err := adapter.PrepareLaunch(ctx, agentruntime.StartRequest{
		ID:           "session-1",
		Agent:        agentruntime.AgentOpenCode,
		Env:          map[string]string{},
		Workdir:      "/tmp/work",
		Instructions: "Be concise and prefer bullet points.",
		Prompt:       "Summarize this repository.",
		MCPServers: []agentruntime.MCPServerConfig{{
			Name:    "my-tools",
			Command: "my-mcp-server",
			Args:    []string{"--port", "8080"},
		}},
	})
	if err != nil {
		log.Fatal(err)
	}

	_ = spec // Run spec.Command, spec.Args, spec.Env, and spec.Workdir.
	// Remove spec.CleanupPaths after the agent process exits.
}
```
