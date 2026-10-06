# Native question tools and one-hour MCP waits: Claude, Codex, OpenCode

Research for ticket `2026-09-29-1352-research-native-question-tool-controls-and-one-hour-mcp-waits-in-claude-codex-and-opencode`, run 2026-09-29 on macOS (darwin arm64).
This covers feasibility only. No production behaviour, live provider configuration, credentials or running Hiveryn sessions were changed.

Installed CLIs:

| CLI | Version | Default model used in probes |
|---|---|---|
| Claude Code | 2.1.283 at the start, auto-updated to **2.1.284** during the session (the TUI probes ran on 2.1.284) | `--model sonnet` (Sonnet 5.5) |
| Codex CLI | codex-cli **0.158.0** | `gpt-6-astra` from the user's config, plus `-m gpt-5.5` |
| OpenCode | **1.17.18** | user default (DeepSeek V4 Pro) |

Evidence labels used below: **doc** (official documentation, accessed 2026-09-29), **src** (source at the installed tag, or the installed binary/bundle), **probe** (measured in this session; raw logs are in `runs/`).

## Verdict

| | Claude Code | Codex CLI | OpenCode |
|---|---|---|---|
| Native question tool can be removed while MCP stays | **Yes**: `--disallowedTools=AskUserQuestion`, per launch | **Partly**: `-c tools.experimental_request_user_input.enabled=false` removes `request_user_input`. `request_user_input_async` (non-blocking, model-catalog gated) remains for `gpt-6-astra` with no user switch | **Yes**: `permission.question = "deny"` in `OPENCODE_CONFIG_CONTENT`, per launch |
| Default limit on one pending MCP call | 30 min idle for stdio (`CLAUDE_CODE_MCP_TOOL_IDLE_TIMEOUT`). In the TUI the call also moves to a background task after 2 min | 300 s (`tool_timeout_sec`) | 60 s (MCP SDK default) |
| One-hour call returns the answer or Hiveryn's timeout text | **Verified (probe)** with per-server `"timeout":3900000` (or `CLAUDE_CODE_MCP_TOOL_IDLE_TIMEOUT=0`) plus `CLAUDE_CODE_MCP_AUTO_BACKGROUND_MS=0` in the TUI. Fails at 30 min by default | **Verified (probe)** with `-c mcp_servers.<name>.tool_timeout_sec=3900`. Fails at 300 s by default. With `gpt-6-astra` the model polls `functions.wait` while waiting | **Verified (probe)** with `mcp.<name>.timeout=3900000`, or progress notifications every < 60 s. Fails at 60 s by default |
| Progress notifications extend the limit | They reset the idle timer only; the wall-clock `MCP_TOOL_TIMEOUT`/per-server `timeout` is not extended | No (only logged) | Yes (`resetTimeoutOnProgress: true`, no `maxTotalTimeout`) |
| Client sends `notifications/cancelled` | On Esc: yes (`AbortError: user-cancel`). On idle timeout: none observed | Never (timeout or Esc) | Yes (timeout and Esc) |
| Conversation continues after timeout/Esc | Yes | Yes | Yes |

**Summary.** The V1 contract is feasible on all three CLIs, with two conditions:

1. **Per-launch client configuration is required.** No provider's default lets a one-hour call succeed. Claude defaults to a 30-minute stdio idle abort and moves the call to the background at 2 minutes. Codex defaults to 300 s and OpenCode to 60 s.
2. **Codex with `gpt-6-astra` polls instead of truly blocking.** The single long MCP call succeeded, but the model re-polled through `functions.wait` 57 times in the TUI, about 1.09 M input tokens (97.6 % cached).

Every configured one-hour probe returned the server's text at 3600 s. The client deadline was set to 3900 s (65 min), which leaves room for Hiveryn's own 60-minute timeout reply to arrive.

## 1. Native question tools

### Claude Code: `AskUserQuestion`

- **What it is.** It shows a multiple-choice dialog in the TUI and needs no permission (doc: [tools reference](https://code.claude.com/docs/en/tools-reference)). It is only offered in interactive sessions. The `-p` requests captured here did not contain it (probe).
- **How to disable it:**
  - **Flag, per launch:** `--disallowedTools AskUserQuestion`. "A bare tool name removes the matching tools from Claude's context" (doc: [CLI reference](https://code.claude.com/docs/en/cli-reference)).
  - **Settings:** `permissions.deny: ["AskUserQuestion"]`, either in a settings file or per launch through `--settings <file|json>` (doc: [permissions](https://code.claude.com/docs/en/permissions)). "A tool denied at any level can't be allowed by another level."
  - **Headless only:** `--permission-prompts none` (v2.1.259+) (doc).
- **Probe.** The TUI was pointed at a local capture endpoint (`capture-server.mjs`) through `ANTHROPIC_BASE_URL`, which records the exact `tools` array:

  | Launch | `AskUserQuestion` sent to the model | `mcp__probe__wait_probe` |
  |---|---|---|
  | TUI, no flag | present | present |
  | TUI, `--disallowedTools=AskUserQuestion` | **absent** | present |
  | TUI, `--settings deny.json` (`permissions.deny`) | **absent** | – |
  | TUI, `--resume <id>` without the flag (session created with the flag) | **present again** | present |
  | TUI, `--resume <id> --disallowedTools=AskUserQuestion` | **absent** | present |

  The disable is therefore a **per-process** setting, not stored with the session. Every launch, new or resumed, must pass it. agentruntime already rebuilds the full argv on resume, so adding it there covers both cases.
- **Pitfall found (probe).** `--disallowedTools`, `--allowedTools`, `--mcp-config` and `--add-dir` are variadic. A positional prompt placed after them is consumed as a value. `claude --disallowedTools AskUserQuestion "hi"` sent no prompt. Use the `--flag=value` form, or keep a non-variadic flag between the variadic flag and the prompt. The agentruntime Claude adapter already puts `--mcp-config <path>` before `--session-id`/`--resume`, so the prompt is not affected today.
- **Not question tools:** permission prompts, `ExitPlanMode` plan approval, the folder-trust dialog, the "custom API key" and hook-trust dialogs, MCP elicitation, and the "Interrupted · What should Claude do instead?" notice. `askUserQuestionTimeout` / `CLAUDE_AFK_TIMEOUT_MS` affect only `AskUserQuestion` (doc).

### Codex CLI: `request_user_input` and `request_user_input_async`

- **What they are (src, probe).** With `gpt-6-astra`, the tool list captured from `codex exec` contains `functions.exec`, `functions.wait`, **`functions.request_user_input`**, **`functions.request_user_input_async`**, `clock.sleep` and the `collaboration.*` tools. With `-m gpt-5.5` it contains `request_user_input` but not the async tool. The tool surface is decided by the model catalog, not by a user setting.
- **`request_user_input` (blocking dialog):**
  - Registered by default (src: `codex-rs/core/src/config/mod.rs:2693-2699`, `tools.experimental_request_user_input.enabled` defaults to true).
  - Usable only in **Plan** mode, or in Default mode when the under-development feature `default_mode_request_user_input` is on (src: `codex-rs/tools/src/tool_config.rs:17-26`).
  - Probe: in the Default-mode TUI, a call returned `request_user_input is unavailable in Default mode`. In `codex exec` it returns "not supported in exec mode" (src).
  - The user can switch the TUI to Plan mode, and the agentruntime adapter rejects `mode: plan` for Codex. So in Hiveryn the tool matters only after a manual mode switch, unless it is removed.
- **How to disable `request_user_input` (probe):** `-c tools.experimental_request_user_input.enabled=false`. It was absent from the captured tool list for both models. This is per process, so it applies equally to `codex resume`. The key is not in the published config reference (doc: [config reference](https://learn.chatgpt.com/docs/config-file/config-reference)). It is experimental and could be renamed.
- **Keys that did not remove it (probe):**
  - `-c tools.request_user_input=false`
  - `-c features.request_user_input=false`
  - `-c features.collaboration_modes=false`
  - `-c include_collaboration_mode_instructions=false`
  - `features.default_mode_request_user_input=false` (already the default)
- **`request_user_input_async` (probe):**
  - It is non-blocking. In the TUI it rendered "Pick a color / Red / Blue" in the transcript and returned `{"accepted":true}` at once. The answer arrives as the user's next message.
  - It is registered from the model catalog's `experimental_supported_tools` (src: `spec_plan.rs:1179-1200`). No user config switch was found.
  - For `gpt-6-astra` it can only be discouraged through instructions, not disabled. It is not a blocking dialog, so it does not compete with a pending `askQuestion` call.
- **Not question tools:** exec/patch approvals, `PermissionRequest`, MCP tool approval (`default_tools_approval_mode`), MCP elicitation, the folder-trust dialog and the "Hooks need review" dialog. Both of the last two appeared in probes. The trust decision is saved to `~/.codex/config.toml` and cannot be suppressed per launch with `-c projects."<dir>".trust_level="trusted"` (probe). Both dialogs were declined or skipped; `config.toml` was verified unchanged.

### OpenCode: `question`

- **Registration (src):**
  - Registered when `OPENCODE_CLIENT` ∈ {app, cli, desktop} (default `cli`) or `OPENCODE_ENABLE_QUESTION_TOOL` is set (`tool/registry.ts:202,228`).
  - The base permission default is `deny`. The `build` and `plan` agents set it to `allow` (`agent/agent.ts:126,148,163`).
  - `opencode run` without `-i` also denies it (`cli/cmd/run.ts:431-445`).
  - A deny on `*` hides the tool from the model (`permission/index.ts:204-219`).
- **Probe (captured tool lists):**

  | Launch | `question` | `probe_wait_probe` |
  |---|---|---|
  | TUI, no permission config | present | – |
  | TUI, `"permission":{"question":"deny"}` | **absent** | – |
  | TUI, `"permission":{"*":"allow","question":"deny"}` + MCP | **absent** | present |
  | TUI, `"permission":"allow"` (what agentruntime emits for `Yolo`) + MCP | **present** | present |
  | TUI, `OPENCODE_CLIENT=acp` | absent | – |
  | `opencode run` and `opencode run -i` | absent | – |

- **Supported disable:** `permission.question = "deny"` in `OPENCODE_CONFIG_CONTENT`, per process and per launch. It can also go under `agent.<name>.permission`. It applies to `--session <id>` and `--continue` resumes because the config comes from the launch env (doc: [permissions](https://opencode.ai/docs/permissions/) documents the `question` key).
- **Adoption note.** agentruntime's opencode adapter currently writes `permission: "allow"` (a string) for `Yolo`. That keeps `question` enabled. It would need the object form `{"*":"allow","question":"deny"}`.
- **Avoid `OPENCODE_CLIENT=acp`:** it also changes other client-dependent behaviour.
- **Not question tools:** `permission.asked` approval prompts, plan enter/exit.

## 2. How long an MCP call can stay pending

### Timeout layers per provider

**Claude Code** (doc: [MCP](https://code.claude.com/docs/en/mcp), [env vars](https://code.claude.com/docs/en/env-vars); src: installed bundle)

| Layer | Default | Setting | Progress resets it? |
|---|---|---|---|
| Overall tool timeout | 100 000 000 ms (~28 h) | `MCP_TOOL_TIMEOUT` env, or per-server `"timeout"` (ms) in the `--mcp-config` JSON | No |
| Idle timeout (no response and no progress) | **1 800 000 ms for stdio**, 300 000 ms for network | `CLAUDE_CODE_MCP_TOOL_IDLE_TIMEOUT` (0 disables). A per-server `"timeout"` ≥ 1000 raises that server's idle floor | **Yes** |
| HTTP/SSE per-request timer | 60 s | raised by `MCP_TOOL_TIMEOUT` or per-server `timeout` > 60000 | – (stdio has none) |
| Auto-backgrounding | 120 000 ms in the TUI; off in `-p` unless `CLAUDE_AUTO_BACKGROUND_TASKS=1` | `CLAUDE_CODE_MCP_AUTO_BACKGROUND_MS` (0 disables) | – |
| Startup | 30 000 ms | `MCP_TIMEOUT` | – |

**Codex CLI** (src at `rust-v0.158.0`; the docs still say 60 s / 10 s)

| Layer | Default | Setting | Progress resets it? |
|---|---|---|---|
| Tool call | **300 s** (`codex-mcp/src/rmcp_client.rs:103-104`) | `mcp_servers.<name>.tool_timeout_sec` (float), settable per launch with `-c`. No maximum found | **No** (only logged) |
| Startup | 30 s | `startup_timeout_sec` / `startup_timeout_ms` | – |
| Code-mode cell (`gpt-6-astra`) | The `exec` cell yields after ~30 s. The model then calls `functions.wait` with a model-chosen `yield_time_ms` | none (model behaviour) | – |

**OpenCode** (src at `v1.17.18`; the docs say the per-server `timeout` covers only fetching tools, which is misleading)

| Layer | Default | Setting | Progress resets it? |
|---|---|---|---|
| Tool call | **60 000 ms** (`DEFAULT_REQUEST_TIMEOUT_MSEC` of MCP SDK 1.29.0) | `mcp.<name>.timeout` (ms), falling back to `experimental.mcp_timeout`, per launch through `OPENCODE_CONFIG_CONTENT` | **Yes** (`resetTimeoutOnProgress: true`, no `maxTotalTimeout`; `mcp/catalog.ts:53-66`) |
| Connect/list tools | 30 000 ms | same `timeout` | – |

### Measured waits (probe)

Each run called `wait_probe` once. `probe-server.mjs` holds the call for the requested time, stands in for Hiveryn's askQuestion, and logs every JSON-RPC message with timestamps. Times are UTC, 2026-09-29.

**Short and default-limit runs:**

| Run (`runs/<dir>`) | Mode | Setting | Wait | Outcome |
|---|---|---|---|---|
| `claude-d90` | `-p` | defaults | 90 s | returned at 90.0 s |
| `claude-tui-bg150` | TUI | defaults | 150 s | at 120 s "moved to the background as task …". The turn ended; the result arrived at 150 s as "MCP task … completed" and started a new turn |
| `claude-h1-default` | `-p` | defaults | 3600 s | **aborted at 1806 s**: `…sent no response or progress for 1800s; aborting…` |
| `claude-h1-env` | `-p` | `MCP_TOOL_TIMEOUT=3900000` | 3600 s | **aborted at 1806 s**, same idle error. `MCP_TOOL_TIMEOUT` does not lift the idle timeout |
| `claude-tui-h1-default` | TUI | defaults | 3600 s | backgrounded at 2 min, then **failed at 30 min** with the idle error delivered as a task notification |
| `codex-d90` / `codex-d90p` | exec | defaults (± progress every 10 s) | 90 s | returned at 90.0 s |
| `codex-h1-default` | exec | defaults | 3600 s | **timed out at 300 s**: `timed out awaiting tools/call after 300s`. No cancel reached the server |
| `opencode-d90` | run | defaults | 90 s | **timed out at 60.0 s**: `MCP error -32001: Request timed out`, with a cancel sent |
| `opencode-d90p` | run | defaults, progress every 10 s | 90 s | returned at 90.0 s (progress reset the 60 s timer) |
| `opencode-t120` | run | `"timeout":120000` | 90 s | returned at 90.0 s (the per-server `timeout` covers tool calls) |

**One-hour runs with configuration.** The probe server held the call for exactly 3600 s:

| Run (`runs/<dir>`) | Mode | Setting | Outcome |
|---|---|---|---|
| `claude-tui-h1-cfg` | TUI | `MCP_TOOL_TIMEOUT=3900000 CLAUDE_CODE_MCP_TOOL_IDLE_TIMEOUT=0 CLAUDE_CODE_MCP_AUTO_BACKGROUND_MS=0` | **`PROBE_DONE after 3600.0s`**, foreground the whole time (04:09:19 → 05:09:19) |
| `claude-tui-h1-servertimeout` | TUI | per-server `"timeout":3900000` + `CLAUDE_CODE_MCP_AUTO_BACKGROUND_MS=0` | **`PROBE_DONE after 3600.0s`**, foreground (04:33:48 → 05:33:48). The per-server field alone lifts the idle limit |
| `claude-h1-servertimeout` | `-p` | per-server `"timeout":3900000` | **`PROBE_DONE after 3600.0s`**, `subtype: success`, 3606 s |
| `claude-tui-h1-progress` | TUI | defaults, progress every 60 s | **`PROBE_DONE after 3600.0s`**, but only after backgrounding at 2 min. The turn ended and the result came back as a new turn |
| `codex-h1-cfg` | exec, gpt-6-astra | `-c mcp_servers.probe.tool_timeout_sec=3900` | **`PROBE_DONE after 3600.0s`**. The model polled twice (`yield_time_ms` 1000, then 3600000); 64 k input tokens |
| `codex-tui-h1-cfg` | TUI, gpt-6-astra | same | **`PROBE_DONE after 3600.0s`**. **57 `functions.wait` polls** at `yield_time_ms=60000`; 1 087 633 input tokens (1 061 248 cached) |
| `codex-h1-gpt55` | exec, `-m gpt-5.5` | same | **`PROBE_DONE after 3600.0s`**. A direct MCP call reached through `tool_search`, no polling; 45 k input tokens. A first attempt without a hint answered "MCP tool wait_probe is not available" because the model did not search for it |
| `opencode-h1-cfg` | run | `"timeout":3900000` | **`PROBE_DONE after 3600.0s`** |
| `opencode-h1-progress` | run | defaults, progress every 30 s | **`PROBE_DONE after 3600.0s`** (progress alone kept it alive) |

A one-hour pending call is therefore **verified** on all three CLIs with these settings: one run each for Claude TUI, Claude `-p`, Codex TUI, Codex exec and `opencode run`. None of this establishes an upper bound beyond ~1 h. The 3900 s client deadlines were not themselves reached.

### Codex polling caveat (probe)

- With `gpt-6-astra` (the configured default model), the model does not call MCP tools directly. It writes a JavaScript cell for `functions.exec` (`await tools[...]({...})`). The cell returns "Script running with cell ID 1" after ~31 s, and the model must call `functions.wait(cell_id, yield_time_ms)`.
- In the headless default-timeout run the model chose `yield_time_ms=60000` and polled five times. Each poll is a model request: 129 k input tokens (mostly cached) for a 5-minute wait. The model also posted "still running" messages.
- In the one-hour headless run it chose `yield_time_ms=3600000` and waited once.
- In the one-hour **TUI** run it chose 60000 and polled **57 times**, about 1.09 M input tokens.
- Whether a one-hour question costs one wait or sixty is therefore up to the model, not something Hiveryn configures. No setting was found that makes Codex expose MCP tools directly for this model: `features.code_mode_host=false` and `features.code_mode=false` changed nothing. `-m gpt-5.5` does call MCP tools directly, reached through `tool_search`.

### Timeout, cancellation and continuation (probe unless marked)

- **Claude:**
  - Esc on a pending foreground call sent `notifications/cancelled` with reason `AbortError: user-cancel`. The UI showed "Interrupted · What should Claude do instead?" and the next prompt worked (`ALIVE`).
  - The idle timeout returned this tool error to the model: `MCP server "probe" tool "wait_probe" sent no response or progress for 1800s; aborting. …`. No `notifications/cancelled` reached the server. The turn ended normally (`-p` reported `subtype: success`), and the conversation stays usable.
  - With auto-backgrounding the turn **ends after 2 min**. The TUI shows "1 MCP task still running". The result or failure comes back later as a task notification that starts a new turn.
- **Codex:**
  - At the 300 s default the model received `tool call error: tool call failed for 'probe/wait_probe' … timed out awaiting tools/call after 300s`, and the turn completed.
  - Esc marked the call `Error: interrupted` / "Conversation interrupted", and the next prompt worked.
  - **In neither case was `notifications/cancelled` sent.** After Esc the probe server ran the abandoned call to completion (300 s) and its response was dropped (src: `core/src/tools/parallel.rs`, rmcp future dropped without `cancel()`).
- **OpenCode:**
  - At the 60 s default the SDK sent `notifications/cancelled` (`McpError: MCP error -32001: Request timed out`). The model saw `MCP error -32001: Request timed out`.
  - Esc sent `notifications/cancelled` (`AbortError: The operation was aborted.`). The part showed "interrupted", and the next prompt worked.
  - Quirk: OpenCode also sends a stray `notifications/cancelled` for the request id just *after* a successful result. Hiveryn must ignore cancels for already-finished requests.

## 3. Hiveryn-side constraints (daemon read-only reference)

- **Hiveryn MCP transport.** `hiverynd mcp` is a stdio go-sdk v1.2.0 server (`daemon/internal/mcp/server.go:91-92`). It forwards to the daemon over HTTP with an **untimed** `http.DefaultClient` (`internal/mcp/client.go:97-100`). The daemon's `http.Server` sets only `ReadHeaderTimeout: 5s` (`internal/server/server.go:18-21`), so neither side limits a one-hour request. As stdio, Claude's 60 s HTTP per-request timer does not apply.
- **Stale assumption.** The `intentWaitWindow` comment says the window "must stay safely under the smallest agent-runtime tool-call ceiling (~60s across Claude/codex/opencode)" (`internal/sessionruntime/intents_policy.go:35-41`). The ceilings are actually 30 min idle / 300 s / 60 s by default, and can be configured past one hour. The default `intent_wait_timeout` is 20 s.
- **Cancellation.**
  - go-sdk turns `notifications/cancelled` into cancellation of the handler context, which aborts the HTTP request.
  - `awaitIntent` runs its policy detached from the agent's ctx, so a cancelled call leaves the intent pending until its own timer fires.
  - A future askQuestion should withdraw or mark the desktop dialog when the call's ctx ends. Codex never cancels, and Claude's idle timeout does not either. So the daemon must also treat "the client stopped waiting" as a normal outcome and expire the dialog by its own 1 h timer.
  - An answer submitted after the provider gave up cannot reach the model. It needs a visible "the agent stopped waiting" state.
- **Keeping the call inside provider limits.** Hiveryn's 60-minute timeout must fire before every client deadline. Recommended per-launch client settings leave margin, for example 65 min:
  - **Claude:** per-server `"timeout": 3900000` in the generated `--mcp-config` (it raises the stdio idle floor too) and `CLAUDE_CODE_MCP_AUTO_BACKGROUND_MS=0` in the launch env. Alternatively send MCP progress notifications more often than every 30 min (this keeps the idle timer alive; it does not prevent backgrounding).
  - **Codex:** `-c mcp_servers.hiveryn.tool_timeout_sec=3900` (the adapter already emits per-server `-c` keys).
  - **OpenCode:** `"timeout": 3900000` on the `mcp.hiveryn` entry in `OPENCODE_CONFIG_CONTENT`. Periodic progress also keeps it alive indefinitely.

  These apply to every tool on the Hiveryn server (per-server granularity). Claude's per-server `timeout` also becomes the idle floor for all of them, so a hung Hiveryn tool would take up to 65 min to fail.
- **Attention/idle semantics.**
  - During a foreground pending call all three providers report the session as busy in a tool (Claude TUI "Calling probe…", Codex "Working", OpenCode spinner). Existing attention detection must not treat a long-pending askQuestion as a stall.
  - If Claude auto-backgrounding is left on, the turn ends (Stop hook → idle) while the question is still pending.
- **Codex `gpt-6-astra` polling** (above) means "no repeated agent polling" is not guaranteed for that model.

## 4. Unknowns and limitations

- The durations in the measured table are single runs on one machine. Provider behaviour beyond the measured duration, and "unlimited" maxima, are not established. No run exceeded ~1 h.
- Behaviour tested headless versus TUI:
  - **Codex:** headless and TUI.
  - **Claude:** headless and TUI.
  - **OpenCode:** one-hour waits headless only (`opencode run`). Esc/cancel was tested in the TUI. The timeout code path is shared (src), but a one-hour TUI run was not measured.
- Codex in the TUI and `exec` ran with `gpt-6-astra` (code mode). The `gpt-5.5` one-hour run was headless only.
- The exact model-visible text for a Claude wall-clock (`MCP_TOOL_TIMEOUT`) expiry, as opposed to the idle expiry, was not observed.
- Whether a Claude call moved to the background sends `notifications/cancelled` when stopped from the task list was not tested.
- Codex's under-development 2026-07-28 MCP protocol path may change cancellation behaviour (src, unverified).
- `tools.experimental_request_user_input.enabled` is an undocumented, experimental key.
- No phone notification or desktop dialog was built or exercised. `probe-server.mjs` only stands in for the pending call.
- Claude auto-updated from 2.1.283 to 2.1.284 mid-session. The headless Claude one-hour runs that started at 04:00 may have used 2.1.283.

## 5. Addendum 2026-10-06: adapter launch controls

Ticket `2026-10-06-1144-add-provider-launch-controls-for-hiveryn-questions-and-one-hour-mcp-waits` turned these findings into `StartRequest.DisableNativeQuestions` and `MCPServerConfig.ToolTimeout` (see the repository README, "Native Questions and Long MCP Calls"). These checks ran on launch specs produced by the adapters' own `PrepareLaunch` with a scratch driver that is not kept. Installed versions: Claude Code **2.1.290**, codex-cli **0.160.0**, OpenCode **1.17.18**.

- **Tool lists** (`runs/adapter-tools-2026-10-06/summary.txt`, same capture endpoint as section 1):
  - Claude TUI: `AskUserQuestion` absent on new and `--resume <id>` launches, present without the control. A `--disallowedTools` passed by the caller combines with the adapter's flag. Claude 2.1.290 connects MCP servers asynchronously, so the probe tool is in the first request only some of the time, with or without the control.
  - Codex exec: `request_user_input` absent for `gpt-6-astra` and `gpt-5.5`; `request_user_input_async` still present for `gpt-6-astra`. The `tool_timeout_sec` key was accepted.
  - OpenCode TUI: `question` absent with `{"*":"allow","question":"deny"}` (Yolo), with `{"question":"deny"}`, and on `--session <id>` resume; `probe_wait_probe` present in every case.
- **OpenCode TUI one-hour call, closing the gap in section 4** (`runs/opencode-tui-h1-adapter/`): TUI launched from the adapter spec (`Yolo`, `DisableNativeQuestions`, `ToolTimeout` 65 min → `"timeout":3900000`), user default model DeepSeek V4 Pro. `tools/call` at 00:55:36Z, server result at 01:55:36Z (`elapsed 3600.0`), and the TUI showed the tool output and the model's reply `PROBE_DONE after 3600.0s` (`screen.txt`, after 1h 0m). The stray post-result `notifications/cancelled` from section 2 appeared again, 136 ms after the result.
- Found while checking resume: headless Codex resume (`codex exec resume`) rejected the adapter's `--cd`. Fixed in the same ticket by emitting `--cd`/`--add-dir` before `resume`.

Side effects: Claude transcripts under `~/.claude/projects/*runs-tools-w*`, Codex sessions under `~/.codex/sessions/2026/10/06/`, and OpenCode sessions in its database. No provider configuration was changed. The Codex TUI was not captured because it opened a model-announcement dialog whose answer would be saved to `~/.codex/config.toml`.

## Reproducing

- `probe-server.mjs`: dependency-free stdio MCP server with one tool, `wait_probe({seconds, progress_every})`. It logs to `$PROBE_LOG`.
- `run-probe.sh <claude|codex|opencode> <label> <seconds> [progress_every] [...]`: one headless run in a clean env (`env -i`, so no Hiveryn/agentruntime session variables) with per-launch MCP config only. `SERVER_TIMEOUT_MS` adds the Claude per-server `timeout`; `OC_EXTRA` adds JSON to the OpenCode server entry.
- `claude-tui-probe.sh <label> <seconds> <progress_every> [VAR=value ...]`: the same for the Claude TUI in a detached tmux session.
- `capture-server.mjs`, `claude-tools.sh`, `claude-tools-tui.sh`, `codex-tools.sh`, `opencode-tools.sh`, `opencode-tools-tui.sh`: point each CLI at a local fake model endpoint and record the tool names it offers (responses are HTTP 400; no model is called).
- `runs/`: raw server logs, CLI stdout/stderr and TUI screen captures (`screen.txt`) from this session. `runs/tools/claude.jsonl` holds every provider's captured tool lists, one `{"label":…}` line before each launch.

Side effects of the probes: session transcripts were written to `~/.claude/projects/-Users-kareemelbahrawy-hiveryn-agentruntime-research-2026-09-29-mcp-long-wait-*`, `~/.codex/sessions/2026/09/29/` and OpenCode's session database. Codex TUI probes ran read-only in the already-trusted `~/architects/hiveryn/lab/agentruntime-resume-e2e` so that no trust decision had to be saved.
