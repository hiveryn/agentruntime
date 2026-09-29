#!/bin/sh
# Record the tool list Codex sends to a fake Responses endpoint (capture-server on :18765).
# Usage: codex-tools.sh <label> [codex exec args...]
here=$(cd "$(dirname "$0")" && pwd); t=$here/runs/tools; mkdir -p "$t/w"; cd "$t/w"
label=$1; shift
echo "{\"label\":\"codex-$label\"}" >> "$t/claude.jsonl"
env -i HOME="$HOME" PATH="$PATH" USER="$USER" TERM=xterm-256color CAPKEY=dummy \
  codex exec --skip-git-repo-check -c model_provider=cap -c 'model_providers.cap.name="cap"' \
  -c 'model_providers.cap.base_url="http://127.0.0.1:18765/v1"' -c 'model_providers.cap.wire_api="responses"' \
  -c 'model_providers.cap.env_key="CAPKEY"' -c model_providers.cap.request_max_retries=0 -c model_providers.cap.stream_max_retries=0 \
  "$@" hi < /dev/null >> "$t/codex-out.txt" 2>&1
