#!/bin/sh
# Launch Codex TUI in tmux the way agentruntime does (hooks enabled, -c overrides => embedded
# app server). Usage: codex-tui.sh <label> [extra codex args...]
set -eu
here=$(cd "$(dirname "$0")" && pwd)
. "$here/codex-env.sh" "$1"; shift
tmux new-session -d -s "cx-$(basename $R)" -x 200 -y 50 -c "$W" \
  $CLEAN codex --enable hooks --dangerously-bypass-hook-trust --cd "$W" \
  --config tools.experimental_request_user_input.enabled=false "$@"
echo "session cx-$(basename $R) home=$CH"
