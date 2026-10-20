#!/bin/sh
# Isolated Codex home for message-delivery probes. Source it: . ./codex-env.sh <label>
# Copies only auth.json from ~/.codex; never writes the live home.
here=$(cd "$(dirname "$0")" 2>/dev/null && pwd || pwd)
label=${1:?label}
R=$here/runs/codex-$label
CH=$R/home
W=$R/work
mkdir -p "$CH" "$W"
[ -f "$CH/auth.json" ] || cp "$HOME/.codex/auth.json" "$CH/auth.json"
cat > "$CH/config.toml" <<TOML
model = "gpt-5.6-sol"
model_reasoning_effort = "low"
[projects."$W"]
trust_level = "trusted"
TOML
CLEAN="env -i HOME=$HOME PATH=$PATH USER=$USER LOGNAME=$USER TERM=xterm-256color LANG=en_US.UTF-8 CODEX_HOME=$CH"
export R CH W CLEAN
