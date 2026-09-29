#!/bin/sh
# Record the tool list OpenCode sends to a fake OpenAI-compatible endpoint (capture-server on :18765).
# Usage: opencode-tools.sh <label> <extra-config-json-fragment> [opencode run args...]
here=$(cd "$(dirname "$0")" && pwd); t=$here/runs/tools; mkdir -p "$t/ow"; cd "$t/ow"
label=$1 extra=$2; shift 2
echo "{\"label\":\"opencode-$label\"}" >> "$t/claude.jsonl"
cfg="{\"model\":\"cap/m\",\"provider\":{\"cap\":{\"npm\":\"@ai-sdk/openai-compatible\",\"options\":{\"baseURL\":\"http://127.0.0.1:18765/v1\",\"apiKey\":\"dummy\"},\"models\":{\"m\":{}}}}$extra}"
env -i HOME="$HOME" PATH="$PATH" USER="$USER" TERM=xterm-256color OPENCODE_CONFIG_CONTENT="$cfg" \
  opencode run "$@" hi < /dev/null >> "$t/opencode-out.txt" 2>&1
