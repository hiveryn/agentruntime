# agentruntime Architecture

`agentruntime` is a reusable Go module for agent CLI launch/config primitives and hook-event normalization.

## Boundaries

- This repo owns launch specs, setup specs, native hook normalization, and in-process event ingest.
- Callers own process lifecycle, ptys, terminal rendering, persistence, auth, product workflows, and UI event streams.
- Do not add ticket, architect, collab, conclusion, or filesystem workflow concepts here (caller-owned concerns).

## Package Layout

- Root package: shared public types and the `Adapter` interface.
- `adapter/<agent>`: agent-specific launch synthesis, hook setup, and native event normalization. Installed hooks are endpoint-independent: each session's receiver comes from `StartRequest.HookEndpoint` via the `AGENTRUNTIME_HOOK_ENDPOINT` launch env (`agentruntime.SessionEnv`), so concurrent callers sharing one global hook config never redirect each other's sessions. Each adapter owns the env var naming its config dir, resolved via `Adapter.ConfigRoot(env)` — callers forward the variant env and never hardcode per-agent env-var names.
- `adapter/codex`: uses `AGENTRUNTIME_SESSION_ID` for caller-owned correlation; hook setup writes `hooks.json` in the Codex config dir (`ConfigRoot` reads `CODEX_HOME`, default `~/.codex`); `codex.HookCommand()` generates a self-contained node hook command. Resume: `codex resume` (bare = interactive picker) or `codex resume <id>` (specific); bare resume suppresses `Prompt` because `codex resume` treats the next positional as `SESSION_ID`. A resumed thread is recorded in a new rollout segment `rollout-<ts>-<thread>_<segment>.jsonl` (`rollout.go`): `LocateRolloutSegments` verifies each candidate's identity from its `session_meta` (never from the filename alone), excludes other threads and errors on unverifiable or duplicate files; `LocateTranscript` is the newest segment (cumulative token totals), `ConfirmMessage` reads all of them, since a receipt can sit in an older segment after a later resume.
- `adapter/claude`: uses `--session-id` UUID for native correlation; hook setup writes `settings.json` in the Claude config dir (`ConfigRoot` reads `CLAUDE_CONFIG_DIR`, default `~/.claude`) — critical for variants launched with a custom `CLAUDE_CONFIG_DIR`, which otherwise get no hooks; `claude.HookCommand()` generates a self-contained node hook command. Resume: `--resume` (bare) or `--resume <id>` (specific); skips `NewSessionID`; `--resume` is a managed arg. Auto-memory: `CLAUDE_CODE_DISABLE_AUTO_MEMORY` is always set (`1` by default, `0` with `StartRequest.ClaudeAutoMemory`) on new and resumed launches; a conflicting `Env` value is rejected.
- `adapter/opencode`: uses `OPENCODE_CONFIG_CONTENT` for per-session config; hook setup writes a TypeScript plugin to `~/.config/opencode/plugins/`; plugin POSTs events to `$AGENTRUNTIME_HOOK_ENDPOINT/opencode`. Resume: `--continue` (bare) or `--session <id>` (specific); specific resume suppresses `Prompt`; `--continue`, `-c`, `--session`, `-s` are managed args. `StartRequest.OpenCodeAgentConfig` merges named agent profile entries into the config `agent` section; `OpenCodeAgentConfig.Prompt` is the agent definition prompt, independent of `StartRequest.Prompt` (initial prompt) and `StartRequest.Instructions` (additive). Usage accounting reads opencode's SQLite database under its XDG data dir (`$XDG_DATA_HOME/opencode/opencode.db`); `ConfigRoot` stays `""` (config dir ≠ data dir), so `LocateTranscript` resolves the data dir itself and returns an opaque `"<dbPath>::<sessionID>"` handle. This is why the module depends on `modernc.org/sqlite` (pure-Go, no cgo) — its sole external dependency.
- Native questions and long MCP calls: `StartRequest.DisableNativeQuestions` removes each provider's ask-the-user tool (Claude `--disallowedTools=AskUserQuestion`, Codex `tools.experimental_request_user_input.enabled=false`, OpenCode `permission.question=deny`); Codex's model-catalog `request_user_input_async` cannot be disabled. `MCPServerConfig.ToolTimeout` sets the per-server tool-call deadline (Claude `timeout` ms + `CLAUDE_CODE_MCP_AUTO_BACKGROUND_MS=0`, Codex `tool_timeout_sec`, OpenCode `timeout` ms). Both are per-process and applied identically on resume. Evidence: `research/2026-09-29-mcp-long-wait/`.
- Message delivery (`messages.go`, `adapter/*/messages.go`, `ingest/mailbox.go`): hand a plain-text message into an existing session through the provider's native route, never by typing into its terminal. `StartRequest.MessageDelivery` adds the per-process launch plumbing on new and resumed launches: claude per-launch `--settings` with SessionStart/Stop `asyncRewake` command hooks whose node waiter long-polls `$AGENTRUNTIME_HOOK_ENDPOINT/claude/wait` (exit 2 with the text wakes the idle model; exit 0 when superseded, unknown or without endpoint; transport errors retried in-process, since nothing re-arms a session until its next hook event), opencode `--port` + `OPENCODE_SERVER_PASSWORD`, codex nothing. The optional `MessageDeliverer` interface: `MessageCapability` (mechanism, pull/push, whether the caller must gate on idle, coverage), `DeliverMessage` (push: codex `thread/queue/add` with the caller's id as `clientUserMessageId` after listing the queue, through an embedded `codex app-server`; opencode optional `MessagePreparer.PrepareMessage` checks the previous id and chooses an ascending `messageID` without sending; callers persist it before `DeliverMessage`, which checks for presence again and uses that exact id in `prompt_async`) and `ConfirmMessage` (claude transcript `user` entry carrying the id, codex rollout `client_id` across every verified segment of the thread plus queue state, opencode database row plus assistant `parentID`; every receipt carries `RecordedThrough`, the record's newest entry time, so the caller treats an absence in a record that ends before the hand-off as stale evidence rather than a loss). `ingest.Mailbox` is the caller side of pull delivery: one waiter per caller session, newest wins (204 to the superseded one), `Deliver` hands a message to the armed waiter, observers report arming and loss. Nothing here promises exactly-once; the caller owns durability, idle gating and the receipt check before redelivery. Evidence: `research/2026-10-20-message-delivery/`.
- `ingest`: reusable in-process event hub, primitive byte-ingest API, and optional local HTTP hook handler.

## Structured conversations

The optional `ConversationAdapter` prepares daemon-owned structured transports without starting processes or installing global hooks. Callers retain process ownership, durable queues and public API semantics. `Conversation.Turn` is single-submitter and emits visible items with native correlation; native permission requests remain blocked/unanswered. `ConversationReconciler` accepts only explicit terminal turn or UUID/parent-lineage evidence, never arbitrary later assistant text. Hidden reasoning/instructions are excluded. Chat resume requires a native ID; unsupported TUI settings are rejected instead of dropped. See the README's structured conversation section.

## Event Model

- `Event.ID` is caller-owned and is the primary subscription key.
- `Event.NativeID` is the runtime-native session ID that emitted the event.
- `Event.PrimaryNativeID` is the first native session observed for a caller session by `ingest.Receiver`.
- `Event.NativeSessionRole` is `primary`, `subsession`, or unknown; use it to avoid treating subagent `Stop`/`idle` events as caller-session idle.
- `Event.Status` is native-session scoped, not an aggregate caller-session status.
- `Event.Raw` preserves native diagnostic payloads; contents vary by adapter.
- Codex `Stop` and OpenCode `session.idle` mean the turn is idle, not that the process exited.
- OpenCode subagent events: only `session.created` carries `parent_session_id`; all subsequent subagent events (status, tool) leave `PrimaryNativeID` and `NativeSessionRole` unset so `ingest.Receiver` can classify them correctly from its stored mapping.
- OpenCode `question` tool (`tool.execute.before`) maps to `awaiting_input`; it is the primary permission/confirmation mechanism when the native `permission.asked` event does not fire.
- Codex `PermissionRequest` maps to `awaiting_input`: it fires as the approval prompt is shown.

## Design Rules

- Keep APIs small and transport-neutral where possible.
- Prefer adding behavior behind existing adapter seams over widening root interfaces.
- Keep HTTP as convenience only; daemon-style callers should be able to call primitive ingest directly.
- Tests must use local or captured fixtures, not live agent runs.

## Development

- Run `make tidy && git diff --exit-code -- go.mod go.sum` before merging.
- Run `make vet test build lint` for local preflight.


## Target filesystem preparation

`StartRequest.FileSystem` and `SetupRequest.FileSystem` optionally select the target environment for provider setup and launch artifacts. Nil retains local behavior. Callers own SSH/authentication/process lifecycle and must remove `LaunchSpec.CleanupPaths` on that same target. Never use laptop home/env/temp paths in target preparation. `FileSystem` supplies target home/config environment and private temporary writes; provider-specific config and hooks stay in their adapters. OpenCode bearer headers use `{env:NAME}`, with OAuth disabled when a bearer variable is supplied.
