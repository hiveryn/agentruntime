#!/bin/sh
# TUI variant of opencode-tools.sh: runs the OpenCode TUI in tmux for 20s.
# Usage: opencode-tools-tui.sh <label> <extra-config-json-fragment> [VAR=value ...]
here=$(cd "$(dirname "$0")" && pwd); t=$here/runs/tools; mkdir -p "$t/ow"
label=$1 extra=$2; shift 2
echo "{\"label\":\"opencode-tui-$label\"}" >> "$t/claude.jsonl"
cfg="{\"model\":\"cap/m\",\"provider\":{\"cap\":{\"npm\":\"@ai-sdk/openai-compatible\",\"options\":{\"baseURL\":\"http://127.0.0.1:18765/v1\",\"apiKey\":\"dummy\"},\"models\":{\"m\":{}}}}$extra}"
printf '%s' "$cfg" > "$t/ow/cfg-$label.json"
tmux new-session -d -s "octools-$label" -x 200 -y 50 -c "$t/ow" \
  env -i HOME="$HOME" PATH="$PATH" USER="$USER" TERM=xterm-256color OPENCODE_CONFIG_CONTENT="$cfg" "$@" opencode --prompt hi
sleep 20; tmux kill-session -t "octools-$label"
