#!/bin/sh
# Run codex against a probe's isolated home with a clean env: cx.sh <label> <codex args...>
here=$(cd "$(dirname "$0")" && pwd)
. "$here/codex-env.sh" "$1"; shift
exec $CLEAN codex "$@"
