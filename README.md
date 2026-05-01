# agentruntime

Reusable Go primitives for launching agent CLIs and consuming their hook events.

## Example

```go
package main

import (
	"context"
	"log"
	"net/http"

	"github.com/hiveryn/agentruntime"
	"github.com/hiveryn/agentruntime/adapter/codex"
	"github.com/hiveryn/agentruntime/ingest"
)

func main() {
	ctx := context.Background()
	adapter := codex.New(codex.DefaultOptions())
	receiver := ingest.NewReceiver(adapter)

	sub := receiver.Hub().Subscribe(ingest.Filter{ID: "session-1"})
	defer sub.Close()
	go func() {
		for event := range sub.Events {
			log.Printf("status=%s role=%s native=%s tool=%s", event.Status, event.NativeSessionRole, event.NativeID, event.Tool)
		}
	}()

	mux := http.NewServeMux()
	mux.Handle("/hook", receiver.Handler(agentruntime.AgentCodex))
	go func() {
		log.Fatal(http.ListenAndServe("127.0.0.1:9000", mux))
	}()

	_, err := adapter.EnsureSetup(ctx, agentruntime.SetupRequest{
		Agents: []agentruntime.AgentKind{agentruntime.AgentCodex},
		Marker: "example",
		Hook: agentruntime.HookCommand{
			Command: "hiveryn-codex-hook --endpoint http://127.0.0.1:9000/hook",
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	spec, err := adapter.PrepareLaunch(ctx, agentruntime.StartRequest{
		ID:      "session-1",
		Agent:   agentruntime.AgentCodex,
		Workdir: "/tmp/work",
		Instructions: "Be concise and prefer bullet points.",
		Prompt:       "Summarize this repository.",
	})
	if err != nil {
		log.Fatal(err)
	}

	_ = spec // Run this command in your own process or pty manager.
}
```
