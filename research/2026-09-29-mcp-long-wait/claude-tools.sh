#!/bin/sh
# Record the tool list Claude Code offers the model under each disable option.
# Needs capture-server.mjs listening on 127.0.0.1:18765 writing runs/tools/claude.jsonl.
here=$(cd "$(dirname "$0")" && pwd); t=$here/runs/tools; mkdir -p "$t/w"; cd "$t/w"
echo '{"permissions":{"deny":["AskUserQuestion"]}}' > "$t/deny.json"
c() { label=$1; shift; echo "{\"label\":\"$label\"}" >> "$t/claude.jsonl"
  env -i HOME="$HOME" PATH="$PATH" USER="$USER" TERM=xterm-256color ANTHROPIC_BASE_URL=http://127.0.0.1:18765 ANTHROPIC_API_KEY=sk-dummy \
    claude --model sonnet "$@" < /dev/null >> "$t/claude-out.txt" 2>&1; }
c base -p hi
c disallowed -p hi --disallowedTools AskUserQuestion
c deny-settings -p hi --settings "$t/deny.json"
c tools-default -p hi --tools default
