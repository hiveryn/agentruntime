package ingest

import (
	"context"
	"fmt"
	"sync"

	"github.com/hiveryn/agentruntime"
)

type Receiver struct {
	adapters          map[agentruntime.AgentKind]agentruntime.Adapter
	hub               *Hub
	mu                sync.Mutex
	primaryNativeByID map[receiverKey]string
}

type receiverKey struct {
	agent agentruntime.AgentKind
	id    string
}

func NewReceiver(adapters ...agentruntime.Adapter) *Receiver {
	indexed := make(map[agentruntime.AgentKind]agentruntime.Adapter, len(adapters))
	for _, adapter := range adapters {
		if adapter == nil {
			continue
		}
		indexed[adapter.Agent()] = adapter
	}

	return &Receiver{
		adapters:          indexed,
		hub:               NewHub(),
		primaryNativeByID: map[receiverKey]string{},
	}
}

func (r *Receiver) Hub() *Hub {
	if r == nil {
		return nil
	}
	return r.hub
}

func (r *Receiver) Ingest(ctx context.Context, agent agentruntime.AgentKind, data []byte) (*agentruntime.Event, error) {
	if r == nil {
		return nil, fmt.Errorf("nil receiver")
	}

	adapter := r.adapters[agent]
	if adapter == nil {
		return nil, fmt.Errorf("unsupported agent %q", agent)
	}

	event, err := adapter.NormalizeEvent(ctx, data)
	if err != nil {
		return nil, err
	}
	if event == nil {
		return nil, nil
	}

	r.classifyNativeSession(event)
	r.hub.Publish(*event)
	return event, nil
}

func (r *Receiver) classifyNativeSession(event *agentruntime.Event) {
	if event == nil || event.ID == "" || event.NativeID == "" {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	key := receiverKey{agent: event.Agent, id: event.ID}
	primary := r.primaryNativeByID[key]
	if event.PrimaryNativeID != "" {
		if primary == "" {
			primary = event.PrimaryNativeID
			r.primaryNativeByID[key] = primary
		}
	}
	if primary == "" {
		primary = event.NativeID
		r.primaryNativeByID[key] = primary
	}

	if event.PrimaryNativeID == "" {
		event.PrimaryNativeID = primary
	}
	if event.NativeSessionRole != agentruntime.NativeSessionRoleUnknown {
		return
	}
	if event.NativeID == event.PrimaryNativeID {
		event.NativeSessionRole = agentruntime.NativeSessionRolePrimary
		return
	}
	event.NativeSessionRole = agentruntime.NativeSessionRoleSubsession
}
