#!/bin/sh
# Launch Claude Code TUI in tmux with the channel probe as a per-launch MCP server.
# Usage: claude-tui.sh <label> <port> [extra claude args...]
# Uses the real login (~/.claude) in a clean env and a scratch working directory.
set -eu
here=$(cd "$(dirname "$0")" && pwd)
label=$1 port=$2; shift 2
R=$here/runs/claude-$label; mkdir -p "$R/work"
cat > "$R/mcp.json" <<JSON
{"mcpServers":{"hiveryn":{"type":"stdio","command":"$(command -v node)","args":["$here/channel-server.mjs"],"env":{"PROBE_LOG":"$R/channel.jsonl","CHANNEL_PORT":"$port"}}}}
JSON
tmux new-session -d -s "cl-$label" -x 200 -y 50 -c "$R/work" \
  env -i HOME="$HOME" PATH="$PATH" USER="$USER" TERM=xterm-256color LANG=en_US.UTF-8 \
  claude --model sonnet --mcp-config "$R/mcp.json" --strict-mcp-config \
  --dangerously-load-development-channels server:hiveryn "$@"
echo "session cl-$label dir=$R"
