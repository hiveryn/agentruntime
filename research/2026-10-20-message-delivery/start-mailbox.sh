#!/bin/sh
# start-mailbox.sh <label> <port>: run mailbox.mjs in the background, logging to runs/claude-<label>.
here=$(cd "$(dirname "$0")" && pwd)
mkdir -p "$here/runs/claude-$1"
PROBE_LOG="$here/runs/claude-$1/mailbox.jsonl" MAILBOX_PORT=$2 nohup node "$here/mailbox.mjs" >/dev/null 2>&1 &
echo $! > "$here/runs/claude-$1/mailbox.pid"
