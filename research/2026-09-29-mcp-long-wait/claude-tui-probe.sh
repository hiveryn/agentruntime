#!/bin/sh
# Interactive Claude probe: TUI in a detached tmux session (real login, clean env,
# per-launch --mcp-config) asked to call wait_probe once.
# Usage: claude-tui-probe.sh <label> <seconds> <progress_every> [VAR=value ...]
set -eu
here=$(cd "$(dirname "$0")" && pwd)
label=$1 seconds=$2 every=$3; shift 3
out=$here/runs/claude-tui-$label; mkdir -p "$out/work"; log=$out/server.jsonl; : > "$log"
tfield=""; [ -n "${SERVER_TIMEOUT_MS:-}" ] && tfield=",\"timeout\":$SERVER_TIMEOUT_MS"
cat > "$out/mcp.json" <<JSON
{"mcpServers":{"probe":{"type":"stdio","command":"$(command -v node)","args":["$here/probe-server.mjs"],"env":{"PROBE_LOG":"$log"}$tfield}}}
JSON
prompt="Call the MCP tool wait_probe exactly once with arguments {\"seconds\": $seconds, \"progress_every\": $every}. Do not call any other tool, do not poll, and do not check on it. When it returns, reply with exactly the text it returned; if it fails or times out, reply with the exact error text you received."
date -u +%FT%TZ > "$out/started"
tmux new-session -d -s "ctui-$label" -x 220 -y 60 -c "$out/work" \
  env -i HOME="$HOME" PATH="$PATH" USER="$USER" TERM=xterm-256color LANG=en_US.UTF-8 "$@" \
  claude --model sonnet --mcp-config "$out/mcp.json" --strict-mcp-config --allowedTools mcp__probe__wait_probe --session-id "$(uuidgen | tr A-Z a-z)" "$prompt"
