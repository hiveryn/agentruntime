#!/bin/sh
# Usage: run-probe.sh <claude|codex|opencode> <label> <seconds> [progress_every] [extra env/args...]
# Runs one headless CLI turn that calls wait_probe once, in a clean environment
# (no Hiveryn/agentruntime session env) with per-launch MCP config only.
set -eu
here=$(cd "$(dirname "$0")" && pwd)
provider=$1 label=$2 seconds=$3 every=${4:-0}
shift 3; [ $# -gt 0 ] && shift
out=${PROBE_OUT:-$here/runs}/$provider-$label
mkdir -p "$out/work"
log=$out/server.jsonl
: > "$log"
prompt="${PROMPT_PREFIX:-}Call the MCP tool wait_probe exactly once with arguments {\"seconds\": $seconds, \"progress_every\": $every}. Do not call any other tool. When it returns, reply with exactly the text it returned; if it fails or times out, reply with the exact error text you received."
cleanenv="env -i HOME=$HOME PATH=$PATH USER=$USER LOGNAME=$USER SHELL=/bin/zsh TERM=xterm-256color LANG=en_US.UTF-8 TMPDIR=${TMPDIR:-/tmp}"
server="$(command -v node)"
script="$here/probe-server.mjs"
date -u +%FT%TZ > "$out/started"
case $provider in
claude)
  tfield=""; [ -n "${SERVER_TIMEOUT_MS:-}" ] && tfield=",\"timeout\":$SERVER_TIMEOUT_MS"
  cat > "$out/mcp.json" <<JSON
{"mcpServers":{"probe":{"type":"stdio","command":"$server","args":["$script"],"env":{"PROBE_LOG":"$log"}$tfield}}}
JSON
  (cd "$out/work" && $cleanenv "$@" claude -p "$prompt" --mcp-config "$out/mcp.json" --strict-mcp-config \
     --allowedTools mcp__probe__wait_probe --output-format json --model sonnet ) > "$out/stdout" 2> "$out/stderr" || echo "exit=$?" >> "$out/stderr" ;;
codex)
  (cd "$out/work" && $cleanenv codex exec --skip-git-repo-check --json -s read-only \
     -c "mcp_servers.probe.command=\"$server\"" -c "mcp_servers.probe.args=[\"$script\"]" \
     -c "mcp_servers.probe.env={PROBE_LOG=\"$log\"}" -c "mcp_servers.probe.default_tools_approval_mode=\"approve\"" "$@" "$prompt" < /dev/null) > "$out/stdout" 2> "$out/stderr" || echo "exit=$?" >> "$out/stderr" ;;
opencode)
  cfg=$(printf '{"mcp":{"probe":{"type":"local","command":["%s","%s"],"environment":{"PROBE_LOG":"%s"}%s}}}' "$server" "$script" "$log" "${OC_EXTRA:-}")
  (cd "$out/work" && $cleanenv OPENCODE_CONFIG_CONTENT="$cfg" "$@" opencode run --format json "$prompt") > "$out/stdout" 2> "$out/stderr" || echo "exit=$?" >> "$out/stderr" ;;
esac
date -u +%FT%TZ > "$out/finished"
