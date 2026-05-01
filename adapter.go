package agentruntime

import "context"

type Adapter interface {
	Agent() AgentKind
	PrepareLaunch(context.Context, StartRequest) (LaunchSpec, error)
	EnsureSetup(context.Context, SetupRequest) (SetupResult, error)
	RemoveSetup(context.Context, SetupRequest) (SetupResult, error)
	NormalizeEvent(context.Context, []byte) (*Event, error)
}
