# agentruntime Architecture

`agentruntime` is a reusable Go module for agent CLI launch/config primitives and hook-event normalization.

## Boundaries

- This repo owns launch specs, setup specs, native hook normalization, and in-process event ingest.
- Callers own process lifecycle, ptys, terminal rendering, persistence, auth, product workflows, and UI event streams.
- Do not add ticket, architect, collab, conclusion, or filesystem workflow concepts here (caller-owned concerns).

## Package Layout

- Root package: shared public types and the `Adapter` interface.
- `adapter/<agent>`: agent-specific launch synthesis, hook setup, and native event normalization.
- `adapter/codex`: uses `AGENTRUNTIME_SESSION_ID` for caller-owned correlation; hook setup writes `~/.codex/hooks.json`.
- `adapter/claude`: uses `--session-id` UUID for native correlation; hook setup writes `~/.claude/settings.json`.
- `adapter/opencode`: uses `OPENCODE_CONFIG_CONTENT` for per-session config; hook setup writes a TypeScript plugin to `~/.config/opencode/plugins/`; plugin POSTs events to `<endpoint>/opencode`.
- `ingest`: reusable in-process event hub, primitive byte-ingest API, and optional local HTTP hook handler.

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

## Design Rules

- Keep APIs small and transport-neutral where possible.
- Prefer adding behavior behind existing adapter seams over widening root interfaces.
- Keep HTTP as convenience only; daemon-style callers should be able to call primitive ingest directly.
- Tests must use local or captured fixtures, not live agent runs.

## Development

- Run `make tidy && git diff --exit-code -- go.mod go.sum` before merging.
- Run `make vet test build lint` for local preflight.
