# agentruntime Architecture

`agentruntime` is a reusable Go module for agent CLI launch/config primitives and hook-event normalization.

## Boundaries

- This repo owns launch specs, setup specs, native hook normalization, and in-process event ingest.
- Callers own process lifecycle, ptys, terminal rendering, persistence, auth, product workflows, and UI event streams.
- Do not add Hiveryn ticket, architect, collab, conclusion, or filesystem workflow concepts here.

## Package Layout

- Root package: shared public types and the `Adapter` interface.
- `adapter/<agent>`: agent-specific launch synthesis, hook setup, and native event normalization.
- `adapter/codex`: first concrete adapter; uses `HIVERYN_SESSION_ID` for caller-owned correlation and preserves Codex `session_id` as `NativeID`.
- `ingest`: reusable in-process event hub, primitive byte-ingest API, and optional local HTTP hook handler.

## Event Model

- `Event.ID` is caller-owned and is the primary subscription key.
- `Event.NativeID` is the runtime-native session ID that emitted the event.
- `Event.PrimaryNativeID` is the first native session observed for a caller session by `ingest.Receiver`.
- `Event.NativeSessionRole` is `primary`, `subsession`, or unknown; use it to avoid treating subagent `Stop` events as caller-session idle.
- `Event.Status` is native-session scoped, not an aggregate caller-session status.
- `Event.Raw` preserves native diagnostic payloads; for Codex this is the full hook envelope.
- Codex `Stop` means the turn is idle, not that the process exited.

## Design Rules

- Keep APIs small and transport-neutral where possible.
- Prefer adding behavior behind existing adapter seams over widening root interfaces.
- Keep HTTP as convenience only; daemon-style callers should be able to call primitive ingest directly.
- Tests must use local or captured fixtures, not live agent runs.

## Development

- Run `make tidy && git diff --exit-code -- go.mod go.sum` before merging.
- Run `make vet test build lint` for local preflight.
