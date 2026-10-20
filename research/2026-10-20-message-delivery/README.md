# Programmatic message delivery: Claude, Codex, OpenCode

Research for ticket `2026-10-20-0328-prove-reliable-programmatic-message-delivery-across-claude-codex-and-opencode`, run on 2026-10-20 on macOS (darwin arm64).
It covers feasibility, not production behaviour.

**Scope and safety**
- All probes used isolated sessions and synthetic messages.
- Live Hiveryn sessions, the live Codex home and its queue, and live provider configs were not touched.
- Claude ran in clean-env tmux sessions with the real login, as in `../2026-09-29-mcp-long-wait`. Its probe transcripts under `~/.claude/projects` were deleted afterwards.
- Codex and OpenCode ran with scratch homes and data directories that held only a copied `auth.json`. Those directories were deleted afterwards. `runs/` keeps credential-free excerpts only.

| CLI | Version | Model in probes | Login |
|---|---|---|---|
| Claude Code | 2.1.304 | `--model sonnet` | Claude **Team** (both `~/.claude` and `~/.claude-work`) |
| Codex CLI | 0.163.0 | `gpt-5.6-sol`, low effort | ChatGPT account |
| OpenCode | 1.17.18 | `deepseek/deepseek-v4-flash` (one retry used `deepseek-v4-pro`) | DeepSeek API key |

Evidence labels:
- **probe**: measured here; raw excerpts are in `runs/`
- **src**: read in the provider source at the installed tag (`openai/codex` `rust-v0.163.0`, `sst/opencode` `v1.17.18`) or in the installed Claude bundle
- **doc**: official documentation, read 2026-10-20

## Verdict

Each provider has one native, non-typing route. Each delivers into an existing session, preserves the user's draft, and survives a restart once the agent runs again. **None of them gives exactly-once delivery by itself.** The daemon can reach effectively-once delivery with three pieces:
- a durable outbox
- a stable message ID carried into the provider
- a receipt check against the provider's own transcript or session before any retry

The documented windows below remain.

| Route | Claude Code: `asyncRewake` hook | Codex: durable thread queue | OpenCode: session server |
|---|---|---|---|
| Mechanism | Per-launch `--settings` hooks on SessionStart and Stop with `asyncRewake: true`. The hook long-polls the daemon. Exit 2 with the text on stderr wakes the model (doc, probe) | `thread/queue/add` (experimental app-server JSON-RPC) or `codex queue --thread --message`. The item lands in `$CODEX_HOME/queue_1.sqlite` and the process that owns the thread dispatches it (src, probe) | `POST /session/:id/prompt_async` with a caller `messageID` on the TUI's embedded server (doc, src, probe) |
| Targeting | Native session ID (`--session-id`) in the hook's stdin `session_id` | Thread UUID plus `CODEX_HOME` | Session ID plus the server URL of that TUI process |
| Idle | Wakes at once, ~1–3 s (probe) | Dispatched within ~2 s (probe). An external writer is picked up by the owner's 10 s poll (src) | Starts at once (probe) |
| Busy (tool running) | Not interrupted, but attached at the next tool boundary **inside the current turn**. The model answered the notification and dropped the task's own final reply (probe, rw1) | **Held** until the turn completes, then runs as its own turn (`turn_trigger: "queue"`) (probe) | Not interrupted, but inserted into the running loop. The final answer became the notification's and the task's reply was dropped (probe) |
| Awaiting permission | Dialog stays intact. Same merge into the current turn after approval (probe, rw3) | Held, because the thread is not idle while the turn waits (src; inferred from the busy probe, not run live) | Dialog stays intact. Same merge after approval. `/session/status` reports `busy` throughout (probe) |
| Hiveryn `askQuestion` / long MCP call | Same as busy: a tool call is running (inferred) | Held (inferred) | Same as busy (inferred) |
| User draft in composer | Preserved (probe, rw2) | Preserved (probe) | Preserved (probe) |
| After Esc interrupt | Not probed. Idle-gated delivery waits for the next Stop/SessionStart | **Stuck.** Idle dispatch is skipped after `Interrupted`. It ran only after the user's next turn completed (src, probe) | Not probed |
| Agent not running | Stays in the daemon. Delivered at SessionStart of `--resume` (probe, rw2) | Persisted in SQLite. Dispatched when `codex resume <id>` loads the thread (probe) | No server exists. Must be sent after relaunch, which worked against the relaunched server (probe) |
| Transport acceptance | Hand-off to the hook, which then exits 2 | `thread/queue/add` returns a `queuedSubmission` | HTTP 204. **Not processing:** one 204 was followed by a `prompt_async failed` session error (probe) |
| Appears in conversation | Transcript entry `type:"user"` with `turnOrigin:"task_notification"` that contains the message ID (probe) | Rollout `UserMessage` with `client_id = <caller id>` in a `turn_trigger:"queue"` turn (probe) | `GET /session/:id/message/:messageID` exists (probe) |
| Agent acknowledgement | Assistant reply after that entry, then Stop | `AgentMessage` in that turn | Assistant message whose `parentID` is the message ID (src). Replies were observed (probe) |
| Duplicates | Daemon-controlled. Claude itself never replayed or duplicated a message | `thread/queue/add` does **not** dedupe. List-then-add by `clientUserMessageId` prevents a second copy only while the first is still queued (probe). Narrow source-level window: dispatch starts the turn, then deletes the row, so a crash between the two redispatches it (src) | Re-sending the same `messageID` **upserts** the message and appends a duplicate text part. No second reply was observed (probe). Never resend once the ID exists |
| Lost-message case | **Proven:** Claude logged `queue-operation enqueue` and was killed before the turn. `--resume` did **not** replay it. Redelivery under the same ID produced exactly one `user` entry (probe, rw4) | None observed | A 204 with no reply after a model error. The message persisted; a retry under the same ID got one reply plus a duplicated part (probe) |
| Requirements and caveats | Hooks need workspace trust, which Hiveryn already relies on. `timeout` **is enforced** on `asyncRewake`: an expired waiter is killed and nothing re-arms it until the next hook event (probe, rw5). `rewakeMessage`/`rewakeSummary` are `@internal` schema fields; without them the model sees "Stop hook blocking error from command …" | `thread/queue/*` require `experimentalApi`. `codex queue` mints a random client ID per call. The shared daemon's control socket speaks JSON-RPC over **WebSocket** on a Unix socket (src) | The TUI server is unauthenticated on loopback by default. `OPENCODE_SERVER_PASSWORD` enables basic auth (probe: 401/200). Hiveryn does not currently know the port; the plugin's `serverUrl` input does (src) |

## Claude: channels are not usable here; `asyncRewake` is

- **MCP Channels are blocked.** Both configured logins are Claude Team accounts, and the startup notice says `--dangerously-load-development-channels blocked by org policy … Inbound messages will be silently dropped` (probe: `runs/claude-idle/startup-team.txt`, `runs/claude-work/channel.jsonl`).
  - The channel server's notification write succeeded, but nothing reached the model, and Claude showed "Channels are not enabled for your org".
  - Using channels would need a Team admin to set `channelsEnabled`, plus the research-preview allowlist bypass.
  - Two more caveats:
    - A full-screen warning dialog appears on every interactive launch. The installed bundle skips it only under specific conditions (non-first-party provider or policy settings) (src).
    - A server named in `--mcp-config` (Hiveryn's launch path) did not match `server:<name>` ("no MCP server configured with that name"), although the same server did match from `.mcp.json` (probe).
- **The working route is `asyncRewake` hooks.** "An `asyncRewake` hook that exits with code 2 wakes Claude immediately even when the session is idle" (doc, hooks › Run hooks in the background).
  - The probe passes hooks per launch with `--settings`:
    - SessionStart and Stop: `rewake-hook.mjs`, which long-polls `mailbox.mjs`
    - UserPromptSubmit: marks the session busy
  - The daemon must keep exactly one waiter per session. The newest waiter supersedes the older ones, because each Stop arms another.
  - Gating hand-off on "idle" (the latest event was Stop or SessionStart) turned the busy merge into a separate turn after the task's own reply (rw2: LOOPED, then ACK). Without gating, the notification displaces the in-progress answer (rw1, rw3).
- **Hook timeout must cover the longest expected idle time.**
  - 20 s killed the waiter and stranded the next message (rw5).
  - With `timeout: 86400`, one waiter armed at SessionStart stayed alive 11 min 19 s, past the 600 s default. It survived a mailbox restart at minute 1 by reconnecting after 3 retries, then woke the idle session with ACK-C009 (probe, rw6).
  - A rewake-started turn also fires UserPromptSubmit, so busy tracking covers it.
- **The waiter must survive its own transport.** Once the hook process exits, nothing re-arms the session until its next hook event.
  - A first long-idle attempt died at exactly 5:00. Node's `fetch` has a 300 s headers timeout, so the hook exited 0 and stranded the session (probe).
  - `rewake-hook.mjs` now uses `node:http` and retries errors in-process. It exits only on a message (exit 2) or when superseded (exit 0), so a daemon restart does not strand the session.

## Codex: native durable queue

- **What the queue is.** `codex queue` / `thread/queue/add` writes `queued_items` in `$CODEX_HOME/queue_1.sqlite`. The app server that has the thread loaded dispatches it with `start_turn_if_idle` from `on_thread_idle`, which is skipped when the cause is `Interrupted`. A watcher also polls every 10 s for external writers, and runs after resume (src `ext/queue/src/service.rs`).
- **Hiveryn's sessions run an embedded app server.** They pass `-c` overrides and `--dangerously-bypass-hook-trust`, which rule out the shared daemon (src `tui/src/daemon_startup.rs`). Queueing still worked through every writer, because all of them share the SQLite file (probe):
  - `codex queue` with no daemon running
  - `codex queue` through a running daemon
  - an embedded `codex app-server` stdio writer (`codex-queue.mjs`) while a daemon ran
- **`codex-queue.mjs` is the idempotent-enqueue prototype.** It sets `clientUserMessageId` to the caller's ID, lists the queue first, and returns `already_queued` on retry (probe). After dispatch the ID survives in the rollout as `client_id` (probe: `runs/codex-idle/rollout-excerpt.jsonl`), so the agentruntime transcript locator can confirm receipt.
- **The model provider rate-limited this account.** Two turns waited about 210–235 s on 429 back-off (`runs/codex-idle/rate-limit-429.log`). Delivery semantics were unaffected: the thread counted as busy, so queued items waited.

## OpenCode: session server

- **Idle path.** `prompt_async` accepts a caller `messageID` in OpenCode's ascending format: `msg_` + 12 hex digits of `ms*4096+counter` + 14 base62 characters (`oc-send.mjs`).
  - The format matters. The run loop compares IDs lexicographically, so an old ID re-sent after newer messages is stored but not run.
  - The receipt check is `GET /session/:id/message/:messageID`.
- **Busy and permission cases.** They merge into the running loop, as with ungated Claude. The daemon should send only when `/session/status` (or the plugin's `session.idle`) shows the session idle.
  - A user can still submit in the gap between the status check and the send. Treat that as a small, accepted race.
- **Launch changes needed.** Hiveryn currently launches the TUI on a random port without a password, so the daemon must learn the port. Either:
  - pass `--port` with a daemon-chosen port, or
  - have the agentruntime plugin report `serverUrl` in its events.

  It should also set a per-session `OPENCODE_SERVER_PASSWORD`. Without one, any local process can drive the session today.

## Recommended minimal contract (follow-up)

**Delivery semantics.** At-least-once from a durable daemon outbox, with a provider receipt check before every retry, which makes it effectively-once outside the windows above. Messages are plain text with a stable ID, and the ID is embedded in the text. They are delivered only while the session is idle, and they never interrupt or launch a session.

### agentruntime (provider capability, no ticket concepts)

- **`StartRequest.MessageDelivery bool`.** It makes the adapter add whatever launch plumbing it needs, on both new and resumed launches:
  - Claude: per-launch `--settings` with the SessionStart/Stop `asyncRewake` waiter plus a UserPromptSubmit busy marker. The waiter long-polls `$AGENTRUNTIME_HOOK_ENDPOINT/claude/messages?session=…`. It has no client-side timeout, retries transport errors in-process, and exits 0 when no endpoint is set.
  - OpenCode: a server password env and an explicit or reported server URL.
  - Codex: nothing at launch.
- **An optional adapter interface, e.g. `MessageDeliverer`, with three parts:**
  - `MessageCapability()`: mechanism, whether the caller must gate on idle (Claude, OpenCode) or the provider queues natively (Codex), known gaps, and minimum tested version.
  - `DeliverMessage(ctx, {Env, FileSystem, NativeID, ServerURL, MessageID, Text})`: push providers only.
    - Codex: idempotent `thread/queue/add` via an embedded `codex app-server`, or via the daemon WebSocket once supported.
    - OpenCode: `GET` the message ID first, then `prompt_async`.
  - `ConfirmMessage(ctx, transcriptHandle, MessageID) (appeared, answered bool)`: reads the Claude transcript, the Codex rollout `client_id`, or the OpenCode message and its `parentID`, through the existing `LocateTranscript` handles.
- **`ingest` gains a small in-memory mailbox for pull providers (Claude).** It keeps one waiter per session, with newest-wins supersession and a `Offer(sessionID, msg)` / wait handler. Durability stays with the caller.

### daemon (durable routing, retry, ticket semantics)

- **New migration: `agent_messages`.** Columns:
  - `id` (UUID)
  - `architect_key`
  - `kind`
  - `dedupe_key UNIQUE`
  - `body`
  - `state`: `pending_commit → pending → handed → appeared → answered`, or `failed`
  - `target_session_id`, `native_id`
  - `attempts`, `last_error`
  - timestamps
- **Router.** Resolve `architect_key` to the single running architect session, using the existing one-active-per-architect invariant.
  - With no running session, rows stay `pending`. The next architect session's first idle point delivers them; nothing is launched automatically.
  - Deliver when `session_runs.agent_status` is idle, or for Codex at any time. Codex's native hold covers its busy case, but not the post-interrupt case.
  - After hand-off, run `ConfirmMessage` on the next Stop/idle event and on a timer. Redeliver under the same ID only when it has not appeared. Back off with visible `last_error` when it fails.
- **Desktop/ticket semantics.** The text reads "Ticket <id> has been concluded. Outcome: <outcome>. Use readTicket to review its conclusion." and carries the message ID.

### Conclusion persistence boundary

Today `concludeTicketSession` runs its Exec only on approve or timeout-approve; deny never runs it. Inside Exec, these steps are separate and non-transactional:
1. `conclusion.md` is written in place
2. `ticket.md` is rewritten
3. the directory is renamed `progress → done`, the only atomic step
4. `MarkRunCompleted`
5. `AppendSessionEvent`
6. terminal kill
7. `DeleteSession`, which cascade-deletes the intent's session events

The intent ID and its replay cache live only in memory, and no startup step repairs a half-finished conclusion (daemon code read: `sessionruntime/service.go:1122,1416-1500`, `architectfs/tickets.go:281-362`, `intents_service.go`).

To notify reliably without announcing denied or unpersisted conclusions:
1. **Generate the identity first.** Inside Exec, before the filesystem writes, generate `concluded_at` and insert `agent_messages(state=pending_commit, dedupe_key="ticket_concluded:<ticket>:<concluded_at>")`. Denied intents never reach Exec, so they never create a row.
2. **Promote after the move.** After `ConcludeTicket` returns (ticket in `done`), promote the row to `pending`.
3. **Reconcile at startup.** Promote `pending_commit` rows whose ticket is in `done` with a `conclusion.md` carrying that `concluded_at`. Delete those whose conclusion did not persist. This also covers the existing restore path that can move a `done` ticket back to `backlog`.
4. **Prevent repeats.** The UNIQUE `dedupe_key` stops repeated announcements across retries and replays. Per-message `state` plus the provider receipt check stops repeated delivery.

Optional hardening is out of scope for the notification work but noticed: write `conclusion.md`/`ticket.md` via temp file and rename.

## Gaps needing a product decision or more evidence

1. **Claude dependency.** Rely on `asyncRewake` (documented) with the `@internal` `rewakeMessage`/`rewakeSummary` wording, or accept the default "Stop hook blocking error" framing. Channels would need Team admin enablement, so they are not recommended.
2. **Codex after an Esc interrupt.** Queued messages wait for the user's next completed turn. The daemon cannot start a turn in the TUI's embedded server from outside. Decide whether to accept this or show a desktop hint.
3. **Idle-only delivery can wait indefinitely while the user keeps an architect busy.** This is intended per the ticket policy; confirm.
4. **Architect sessions that start fresh.** Pending notifications are delivered as messages to a new conversation. Alternatively, fold them into the new session's first-message context instead.
5. **Claude idle longer than the hook `timeout`.** The waiter expires and delivery waits for the next turn or resume. Choose the bound; a 24 h `timeout` was verified to hold only for 11 minutes.
6. **Remote (SSH) sessions are not tested.**
   - Claude waiters can use the existing tunneled hook endpoint.
   - Codex needs `codex app-server` executed on the remote host.
   - OpenCode needs its loopback server tunneled.
7. **Codex `thread/queue/*` is experimental**, verified on 0.163.0 only. The WebSocket client for the shared daemon is untested; the embedded stdio writer was used.
8. **Not probed live:**
   - Codex approval-pending (inferred from source and the busy probe)
   - Claude and OpenCode after an Esc interrupt
   - an MCP `askQuestion` wait (inferred from busy-tool probes)
   - concurrent senders

## Files

| File | Purpose |
|---|---|
| `claude-rewake.sh` | Claude TUI with per-launch `asyncRewake` waiter, busy marker, `HOOK_TIMEOUT`, `REWAKE_MESSAGE`/`REWAKE_SUMMARY` |
| `rewake-hook.mjs` | The waiter (long-poll, then exit 2) and the busy marker. rw1–rw5 ran an earlier `fetch` variant with the same protocol but no retry loop |
| `mailbox.mjs`, `start-mailbox.sh` | Stand-in for the daemon side: one waiter per session, newest wins, optional `HOLD_WHILE_BUSY=1` idle gating |
| `channel-server.mjs`, `claude-tui.sh` | Claude MCP Channel probe (blocked by org policy here) |
| `codex-env.sh`, `codex-tui.sh`, `cx.sh` | Isolated Codex home and TUI launched as agentruntime does |
| `codex-queue.mjs` | Idempotent `thread/queue/add` prototype with a caller `clientUserMessageId` |
| `opencode-tui.sh`, `oc-send.mjs`, `oc-msgs.sh` | Isolated OpenCode TUI on a fixed port, `prompt_async` with caller `messageID`, message listing |
| `pane.sh` | tmux screen capture |
| `runs/` | Excerpts: mailbox and hook logs, pane captures, Claude transcript and Codex rollout excerpts, OpenCode message listings |
