#!/bin/sh
# Launch Claude Code TUI in tmux with per-launch asyncRewake hooks (SessionStart + Stop)
# pointing at mailbox.mjs, plus a UserPromptSubmit hook marking the session busy.
# Usage: claude-rewake.sh <label> <mailbox-port> [claude args...]
set -eu
here=$(cd "$(dirname "$0")" && pwd)
label=$1 port=$2; shift 2
R=$here/runs/claude-$label; mkdir -p "$R/work"
hook="$(command -v node) $here/rewake-hook.mjs"
busy="{\"type\":\"command\",\"command\":\"$hook busy\"}"
# HOOK_TIMEOUT (seconds, default 86400) bounds each waiting hook.
# REWAKE_MESSAGE/REWAKE_SUMMARY set the (schema-internal) rewakeMessage/rewakeSummary fields.
extra=""
[ -n "${REWAKE_MESSAGE:-}" ] && extra="$extra,\"rewakeMessage\":\"$REWAKE_MESSAGE\""
[ -n "${REWAKE_SUMMARY:-}" ] && extra="$extra,\"rewakeSummary\":\"$REWAKE_SUMMARY\""
h="{\"type\":\"command\",\"command\":\"$hook\",\"asyncRewake\":true,\"timeout\":${HOOK_TIMEOUT:-86400}$extra}"
cat > "$R/settings.json" <<JSON
{"hooks":{"SessionStart":[{"hooks":[$h]}],"Stop":[{"hooks":[$h]}],"UserPromptSubmit":[{"hooks":[$busy]}]}}
JSON
tmux new-session -d -s "cl-$label" -x 200 -y 50 -c "$R/work" \
  env -i HOME="$HOME" PATH="$PATH" USER="$USER" TERM=xterm-256color LANG=en_US.UTF-8 \
  MAILBOX_URL="http://127.0.0.1:$port" PROBE_LOG_HOOK="$R/hook.jsonl" ${CLAUDE_CONFIG_DIR:+CLAUDE_CONFIG_DIR=$CLAUDE_CONFIG_DIR} \
  claude --model sonnet --settings "$R/settings.json" "$@"
echo "session cl-$label dir=$R"
