# agentruntime

Reusable Go module for agent CLI launch/config primitives and hook-event
normalization.

Current first-class adapters: `codex`, `claude`, `opencode`.

## Status

`v0.x.y` — pre-v1. APIs may change while the module is exercised by downstream
integrations.

## Overview

`agentruntime` answers two questions for supported agent CLIs:

- What hook/plugin setup does this agent need to report runtime events?
- What command, args, env, workdir, and temp files does this agent session need
  to launch?

The library returns specs. It does not run or supervise processes.

### Boundary

`agentruntime` owns marker-scoped hook/plugin setup and removal, launch specs,
native event normalization, and in-process ingest helpers (including an optional
local HTTP handler).

Callers own process execution and lifecycle, PTYs or terminal rendering,
persistence, auth, product workflow, and any UI event stream.

## Quick Start

1. Create an adapter from `adapter/codex`, `adapter/claude`, or
   `adapter/opencode`.
2. Call `EnsureSetup` with a persistent `SetupRequest.Marker` and hook target.
3. Call `PrepareLaunch` with a `StartRequest`.
4. Execute `LaunchSpec` with your own process or PTY manager: merge
   `LaunchSpec.Env`, run `LaunchSpec.Command` with `LaunchSpec.Args` as a direct
   `argv` array (lossless, never shell-join), and set the working directory to
   `LaunchSpec.Workdir`.
5. Remove every path in `LaunchSpec.CleanupPaths` after the process exits.

## Setup Details

`EnsureSetup` is idempotent for the same marker and hook target. It writes
marker-scoped files:

- Codex: `$CODEX_HOME/hooks.json` (default `~/.codex/hooks.json`)
- Claude Code: `$CLAUDE_CONFIG_DIR/settings.json` (default `~/.claude/settings.json`)
- OpenCode: `~/.config/opencode/plugins/agentruntime-<marker>.ts`

Each adapter resolves its config directory from the variant environment via
`Adapter.ConfigRoot(env)`, so a variant launched with a custom `CLAUDE_CONFIG_DIR`
or `CODEX_HOME` gets its hooks installed in the right place.

For Claude/Codex, each call removes every agentruntime-managed hook and reinstalls
one fresh group per event, so repeated launches can never accumulate duplicates and
already-bloated configs self-heal. Managed hooks are identified by a stable signature
in the command string (not the human-readable `agentruntimeMarker` field), because
Claude/Codex rewrite their settings files and may drop unknown fields.

Call `RemoveSetup` when your integration is disabled, uninstalled, or changing
markers. This is distinct from per-launch cleanup.

`LaunchSpec.CleanupPaths` is per-launch cleanup. Adapters use it for temporary
files created by `PrepareLaunch` (Claude MCP config files, OpenCode instruction
files).

### Hook Commands

Codex and Claude Code use self-contained Node.js hook commands that POST
enveloped native hook JSON to your receiver:

```go
codex.HookCommand("http://127.0.0.1:9000")   // POSTs to /codex
claude.HookCommand("http://127.0.0.1:9000")  // POSTs to /claude
```

OpenCode writes a TypeScript plugin that POSTs to `<endpoint>/opencode` from
within the runtime process. Use `HookCommand.Endpoint`:

```go
agentruntime.HookCommand{Endpoint: "http://127.0.0.1:9000"}
```

## StartRequest Reference

- **`ID`** — caller-owned stable session/correlation ID.
- **`Agent`** — optional adapter guard (`AgentCodex`, `AgentClaude`,
  `AgentOpenCode`).
- **`Command`** — executable override. If empty, the adapter uses its default CLI
  name.
- **`Args`** — additional CLI arguments appended after synthesized arguments.
- **`Env`** — optional extra environment for the launched process.
- **`Workdir`** — required working directory.
- **`Prompt`** — initial prompt when the runtime supports it.
- **`Instructions`** — runtime-specific instruction/system-prompt input.
- **`MCPServers`** — stdio or HTTP MCP servers synthesized into the runtime's
  config shape.
- **`OpenCodeAgentConfig`** — (OpenCode only) agent profile definitions merged
  into the `agent` section of `OPENCODE_CONFIG_CONTENT`. Each key is the profile
  name; the value is an `OpenCodeAgentConfig` with `Description`, `Mode`,
  `Prompt` (agent definition prompt — independent of `StartRequest.Prompt` and
  `StartRequest.Instructions`), and optional `Permission`. Ignored when nil or
  empty; nil per-agent `Permission` values are omitted so OpenCode receives
  `undefined`, not JSON `null`.
- **`Resume`** — when `true`, resume an existing session instead of starting
  fresh.
- **`ResumeID`** — native session ID to resume when `Resume` is `true`. If empty,
  the adapter performs an id-less resume — an interactive session picker where the
  CLI supports one (Claude, Codex), otherwise continuing the most recent session
  (OpenCode).

Ordinary runtime options should be passed through `Command`, `Args`, and `Env`.
The library only models behavior it must synthesize for portability: MCP config,
instruction injection, session correlation, and hook/plugin setup.

## Consumer Contract

These rules define the migration surface for downstream consumers. The library
guarantees this contract at the `v0` line.

### Session Identity

- `StartRequest.ID` is the caller-owned stable session/correlation ID.
- `LaunchSpec.Env["AGENTRUNTIME_SESSION_ID"]` carries that same ID into the
  launched process.
- `Event.ID` (matching `StartRequest.ID`) is the primary cache and subscription
  key for consumers. Subscribe, deduplicate, and index on `Event.ID`.
- `Event.NativeID`, `Event.PrimaryNativeID`, and `Event.NativeSessionRole` are
  diagnostic and subsession metadata — do not use them as the primary consumer
  session key.

### Launch Execution

When executing a `LaunchSpec`, callers must:

- Merge `LaunchSpec.Env` into the child process environment.
- Run `LaunchSpec.Command` with `LaunchSpec.Args` as a direct `argv` array
  (lossless, never shell-join).
- Set the working directory to `LaunchSpec.Workdir`.
- Remove every path in `LaunchSpec.CleanupPaths` after the process exits.
- Not mutate adapter-managed environment keys or CLI arguments.

### Adapter-Managed Environment

| Key                       | Adapters                | Notes                                          |
|---------------------------|-------------------------|------------------------------------------------|
| `AGENTRUNTIME_SESSION_ID` | Claude, Codex, OpenCode | Set to `StartRequest.ID`; rejected if conflicting |
| `OPENCODE_CONFIG_CONTENT` | OpenCode                | Rejected if non-empty in `StartRequest.Env`     |

### Adapter-Managed Arguments

These CLI arguments are synthesized by the adapter and rejected when passed
through `StartRequest.Args`:

| Adapter  | Managed Args                                                                     |
|----------|----------------------------------------------------------------------------------|
| Claude   | `--append-system-prompt`, `--system-prompt`, `--mcp-config`, `--session-id`, `--resume` |
| Codex    | (none — top-level args pass through; `resume` is a subcommand)                   |
| OpenCode | `--prompt`, `--continue`, `-c`, `--session`, `-s`                                |

### Resume Behavior

Each adapter synthesizes resume in its own native shape:

| Adapter  | Bare resume (`ResumeID=""`) | Specific resume (`ResumeID=<id>`) | Prompt suppressed when |
|----------|-----------------------------|-----------------------------------|------------------------|
| Claude   | `--resume` (picker)         | `--resume <id>`                   | never                  |
| Codex    | `resume` (picker)           | `resume <id>`                     | bare resume only       |
| OpenCode | `--continue` (last; no picker) | `--session <id>`               | specific resume only   |

Bare resume opens an interactive picker for Claude and Codex. OpenCode has no
launch-time picker, so `--continue` (resume the last session) is the closest
fallback.

In resume mode, Claude skips generating a new `--session-id`.
`AGENTRUNTIME_SESSION_ID` is always set to `StartRequest.ID` regardless of
resume mode.

## Event Ingest

Adapters normalize native payloads into `Event`. `Event.ID` is the primary
subscription and cache key (see [Consumer Contract](#consumer-contract) for
identity rules).

`ingest.Receiver` tracks the first native session seen for each `Event.ID` and
exposes it as `Event.PrimaryNativeID`. It classifies events as `primary` or
`subsession` via `Event.NativeSessionRole`. Use these fields to avoid treating
subagent `Stop`/`idle` events as caller-session idle — not as primary keys.

`Event.Status` is scoped to the native session that emitted the event. Codex
`Stop` and OpenCode `session.idle` mean the turn is idle, not that the process
exited.

### Ingestion Paths

- **Convenience HTTP** — `receiver.Handler(agent)` accepts native hook JSON.
- **Direct ingest** — `receiver.Ingest(ctx, agent, data)` processes raw hook
  bytes through normalization and classification.
- **Standalone** — `adapter.NormalizeEvent(ctx, data)` normalizes without the
  receiver.
- **Subscription** — `receiver.Hub().Subscribe(ingest.Filter{ID: "session-1"})`
  returns a channel of normalized events.

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

	_ = spec // Execute spec following the Consumer Contract.
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

	_ = spec // Execute spec following the Consumer Contract.
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

	_ = spec // Execute spec following the Consumer Contract.
}
```

## Current Consumers

- [Cortex](https://github.com/kareemaly/cortex)
