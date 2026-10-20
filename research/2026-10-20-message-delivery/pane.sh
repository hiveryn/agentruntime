#!/bin/sh
# Capture the visible pane of a tmux session: pane.sh <session> [file]
tmux capture-pane -p -t "$1" | sed -e 's/[[:space:]]*$//' | awk 'NF||p{print;p=NF}' | tee ${2:-/dev/null}
