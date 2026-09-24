package agentruntime

import "fmt"

const (
	// SessionIDEnv carries StartRequest.ID into the launched process so hook
	// payloads can be correlated with the caller-owned session.
	SessionIDEnv = "AGENTRUNTIME_SESSION_ID"
	// HookEndpointEnv carries StartRequest.HookEndpoint into the launched
	// process. Installed hooks and plugins read it at event time, so the shared
	// hook entry in a provider's global config is endpoint-independent and
	// several callers (e.g. daemons on different ports) can install it without
	// redirecting each other's sessions.
	HookEndpointEnv = "AGENTRUNTIME_HOOK_ENDPOINT"
)

// SessionEnv returns the adapter-managed correlation environment for req,
// rejecting caller-supplied Env values that conflict with it. HookEndpointEnv
// is always present, set to "" when HookEndpoint is empty, so a value inherited
// from the caller's own environment cannot route the session's hook events.
func SessionEnv(req StartRequest) (map[string]string, error) {
	managed := map[string]string{
		SessionIDEnv:    req.ID,
		HookEndpointEnv: req.HookEndpoint,
	}
	for key, want := range managed {
		if v, ok := req.Env[key]; ok && v != "" && v != want {
			return nil, fmt.Errorf("reserved env key %s is set to %q which conflicts with managed value %q", key, v, want)
		}
	}
	return managed, nil
}
