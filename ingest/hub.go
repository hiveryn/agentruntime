package ingest

import (
	"sync"

	"github.com/hiveryn/agentruntime"
)

const defaultSubscriptionBuffer = 16

type Filter struct {
	ID       string
	NativeID string
	Agent    agentruntime.AgentKind
	Status   agentruntime.Status
	Tool     string
}

type Hub struct {
	mu     sync.RWMutex
	nextID uint64
	subs   map[uint64]*subscriber
	buffer int
}

type Subscription struct {
	Events <-chan agentruntime.Event

	hub  *Hub
	id   uint64
	once sync.Once
}

type subscriber struct {
	filter Filter
	events chan agentruntime.Event
}

func NewHub() *Hub {
	return &Hub{
		subs:   map[uint64]*subscriber{},
		buffer: defaultSubscriptionBuffer,
	}
}

func (h *Hub) Subscribe(filter Filter) *Subscription {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.nextID++
	id := h.nextID
	events := make(chan agentruntime.Event, h.buffer)
	h.subs[id] = &subscriber{
		filter: filter,
		events: events,
	}

	return &Subscription{
		Events: events,
		hub:    h,
		id:     id,
	}
}

func (h *Hub) Publish(event agentruntime.Event) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, sub := range h.subs {
		if !sub.filter.matches(event) {
			continue
		}
		select {
		case sub.events <- event:
		default:
		}
	}
}

func (s *Subscription) Close() {
	s.once.Do(func() {
		s.hub.remove(s.id)
	})
}

func (h *Hub) remove(id uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()

	sub := h.subs[id]
	if sub == nil {
		return
	}
	delete(h.subs, id)
	close(sub.events)
}

func (f Filter) matches(event agentruntime.Event) bool {
	if f.ID != "" && event.ID != f.ID {
		return false
	}
	if f.NativeID != "" && event.NativeID != f.NativeID {
		return false
	}
	if f.Agent != "" && event.Agent != f.Agent {
		return false
	}
	if f.Status != "" && event.Status != f.Status {
		return false
	}
	if f.Tool != "" && event.Tool != f.Tool {
		return false
	}
	return true
}
