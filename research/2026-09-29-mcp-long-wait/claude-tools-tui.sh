#!/bin/sh
# Interactive variant: run the Claude TUI in a detached tmux session against the
# capture server so the recorded request shows the interactive tool list.
# Usage: claude-tools-tui.sh <label> [claude args...]
here=$(cd "$(dirname "$0")" && pwd); t=$here/runs/tools; mkdir -p "$t/w"
label=$1; shift
echo "{\"label\":\"tui-$label\"}" >> "$t/claude.jsonl"
tmux new-session -d -s "cprobe-$label" -x 200 -y 50 -c "$t/w" \
  env -i HOME="$HOME" PATH="$PATH" USER="$USER" TERM=xterm-256color ANTHROPIC_BASE_URL=http://127.0.0.1:18765 ANTHROPIC_AUTH_TOKEN=dummy \
  claude --model sonnet "$@" hi
