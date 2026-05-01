# agentruntime

`agentruntime` is a small Go module for preparing agent CLI sessions.

The package is intentionally narrow:

- prepare agent-specific launch specs for a caller-owned process/pty
- install and remove agent hook setup
- normalize native hook/plugin events into a small shared event model

It does not own process lifecycle, terminal rendering, ticket systems, or filesystem product workflow.

## Status

This repository is in early scaffold state.

- the shared request/event/setup types live in the root `agentruntime` package
- adapters live in subpackages under `adapter/`
- `adapter/codex` is the first implementation target

## Example

```go
package main

import (
	"context"

	"github.com/hiveryn/agentruntime"
	"github.com/hiveryn/agentruntime/adapter/codex"
)

func main() {
	adapter := codex.New(codex.DefaultOptions())
	_, _ = adapter.PrepareLaunch(context.Background(), agentruntime.StartRequest{
		ID:      "session-1",
		Workdir: "/tmp/work",
		Prompt:  "Summarize the repository status.",
	})
}
```

## Development

```sh
go test ./...
```
