package agentruntime

import "testing"

func TestSessionEnvSetsCorrelationAndHookEndpoint(t *testing.T) {
	env, err := SessionEnv(StartRequest{ID: "session-1", HookEndpoint: "http://127.0.0.1:4201/internal/agentruntime"})
	if err != nil {
		t.Fatal(err)
	}
	if env[SessionIDEnv] != "session-1" {
		t.Fatalf("%s: %q", SessionIDEnv, env[SessionIDEnv])
	}
	if env[HookEndpointEnv] != "http://127.0.0.1:4201/internal/agentruntime" {
		t.Fatalf("%s: %q", HookEndpointEnv, env[HookEndpointEnv])
	}
}

func TestSessionEnvBlanksHookEndpointWhenUnset(t *testing.T) {
	env, err := SessionEnv(StartRequest{ID: "session-1"})
	if err != nil {
		t.Fatal(err)
	}
	value, ok := env[HookEndpointEnv]
	if !ok || value != "" {
		t.Fatalf("expected %s to be present and empty so inherited values cannot route events, got %q (present=%v)", HookEndpointEnv, value, ok)
	}
}

func TestSessionEnvRejectsConflictingCallerEnv(t *testing.T) {
	for _, tc := range []struct {
		name string
		req  StartRequest
	}{
		{"session id", StartRequest{ID: "session-1", Env: map[string]string{SessionIDEnv: "other"}}},
		{"hook endpoint", StartRequest{ID: "session-1", HookEndpoint: "http://a", Env: map[string]string{HookEndpointEnv: "http://b"}}},
		{"hook endpoint without field", StartRequest{ID: "session-1", Env: map[string]string{HookEndpointEnv: "http://b"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := SessionEnv(tc.req); err == nil {
				t.Fatal("expected conflict error")
			}
		})
	}
}

func TestSessionEnvAllowsMatchingOrEmptyCallerEnv(t *testing.T) {
	req := StartRequest{ID: "session-1", HookEndpoint: "http://a", Env: map[string]string{SessionIDEnv: "session-1", HookEndpointEnv: ""}}
	if _, err := SessionEnv(req); err != nil {
		t.Fatal(err)
	}
	req.Env[HookEndpointEnv] = "http://a"
	if _, err := SessionEnv(req); err != nil {
		t.Fatal(err)
	}
}
