#!/bin/sh
# Launch OpenCode TUI in tmux with isolated XDG config/data dirs (only auth.json copied) and
# its embedded server on a fixed port. Usage: opencode-tui.sh <label> <port> [opencode args...]
set -eu
here=$(cd "$(dirname "$0")" && pwd)
label=$1 port=$2; shift 2
R=$here/runs/opencode-$label; mkdir -p "$R/work" "$R/data/opencode" "$R/config/opencode"
[ -f "$R/data/opencode/auth.json" ] || cp "$HOME/.local/share/opencode/auth.json" "$R/data/opencode/auth.json"
tmux new-session -d -s "oc-$label" -x 200 -y 50 -c "$R/work" \
  env -i HOME="$HOME" PATH="$PATH" USER="$USER" TERM=xterm-256color LANG=en_US.UTF-8 \
  XDG_DATA_HOME="$R/data" XDG_CONFIG_HOME="$R/config" XDG_STATE_HOME="$R/state" XDG_CACHE_HOME="$R/cache" \
  ${OPENCODE_CONFIG_CONTENT:+OPENCODE_CONFIG_CONTENT=$OPENCODE_CONFIG_CONTENT} ${OPENCODE_SERVER_PASSWORD:+OPENCODE_SERVER_PASSWORD=$OPENCODE_SERVER_PASSWORD} \
  opencode --pure --port "$port" -m deepseek/deepseek-v4-flash "$@"
echo "session oc-$label dir=$R"
