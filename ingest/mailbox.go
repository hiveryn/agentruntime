package ingest

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// Mailbox is the caller side of pull-mode message delivery (Claude's
// asyncRewake hooks). Each SessionStart and Stop hook of a session arms one
// long-polling waiter here; the newest waiter supersedes the previous one
// (which is answered 204 so its hook exits 0). A message handed to the armed
// waiter is written as the 200 response; the hook prints it to stderr and
// exits 2, which wakes the idle model with the text.
//
// The mailbox holds no messages: durability, the idle gate and the decision to
// hand over belong to the caller, which learns about waiters through the
// observers. A waiter whose connection drops is forgotten, so Armed reflects
// what a hand-off can actually reach.
type Mailbox struct {
	mu      sync.Mutex
	waiters map[string]*mailboxWaiter
	seq     uint64
	armed   func(Waiter)
	gone    func(Waiter)
}

// Waiter is one armed hook waiting for a message.
type Waiter struct {
	// SessionID is the caller-owned session (AGENTRUNTIME_SESSION_ID).
	SessionID string
	// NativeSessionID is the provider's session id from the hook payload.
	NativeSessionID string
	// HookEvent is the hook that armed it: SessionStart or Stop.
	HookEvent string
	// Source is SessionStart's source (startup, resume, clear, compact);
	// empty for Stop.
	Source  string
	ArmedAt time.Time
	seq     uint64
}

// MailboxMessage is what the waiter receives and prints for the model.
type MailboxMessage struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

type mailboxWaiter struct {
	waiter Waiter
	ch     chan MailboxMessage
}

type waitEnvelope struct {
	Hook map[string]any    `json:"hook"`
	Env  map[string]string `json:"env"`
}

func NewMailbox() *Mailbox {
	return &Mailbox{waiters: map[string]*mailboxWaiter{}}
}

// SetObservers registers callbacks run (outside the lock) when a waiter is
// armed and when one leaves without a message (superseded or disconnected).
// Either may be nil.
func (m *Mailbox) SetObservers(armed, gone func(Waiter)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.armed, m.gone = armed, gone
}

// Handler serves the waiter's long poll: POST with the hook envelope
// ({"hook": {...native hook JSON...}, "env": {"AGENTRUNTIME_SESSION_ID": ...}}).
// It answers 200 with a MailboxMessage, 204 when superseded, 400 when the
// envelope names no session.
func (m *Mailbox) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		defer func() { _ = req.Body.Close() }()
		var env waitEnvelope
		if err := json.NewDecoder(http.MaxBytesReader(w, req.Body, defaultMaxBodyBytes)).Decode(&env); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		waiter := Waiter{
			SessionID:       env.Env["AGENTRUNTIME_SESSION_ID"],
			NativeSessionID: hookString(env.Hook, "session_id"),
			HookEvent:       hookString(env.Hook, "hook_event_name"),
			Source:          hookString(env.Hook, "source"),
			ArmedAt:         time.Now(),
		}
		if waiter.SessionID == "" {
			waiter.SessionID = waiter.NativeSessionID
		}
		if waiter.SessionID == "" {
			http.Error(w, "missing session id", http.StatusBadRequest)
			return
		}

		entry := m.arm(waiter)
		select {
		case msg, ok := <-entry.ch:
			if !ok {
				// Superseded by a newer waiter, or the mailbox closed: the
				// hook exits 0 and the newer waiter carries on.
				w.WriteHeader(http.StatusNoContent)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(msg)
		case <-req.Context().Done():
			// The waiter is gone (hook timeout, process killed, link lost):
			// forget it unless a newer one already replaced it.
			if m.forget(entry) {
				m.notifyGone(entry.waiter)
			}
		}
	})
}

// arm registers the waiter, superseding the session's previous one.
func (m *Mailbox) arm(waiter Waiter) *mailboxWaiter {
	m.mu.Lock()
	m.seq++
	waiter.seq = m.seq
	entry := &mailboxWaiter{waiter: waiter, ch: make(chan MailboxMessage, 1)}
	previous := m.waiters[waiter.SessionID]
	m.waiters[waiter.SessionID] = entry
	armed := m.armed
	gone := m.gone
	m.mu.Unlock()
	if previous != nil {
		close(previous.ch) // its handler answers 204 and exits 0
		if gone != nil {
			gone(previous.waiter)
		}
	}
	if armed != nil {
		armed(waiter)
	}
	return entry
}

// forget drops the entry if it is still the session's current waiter.
func (m *Mailbox) forget(entry *mailboxWaiter) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.waiters[entry.waiter.SessionID] != entry {
		return false
	}
	delete(m.waiters, entry.waiter.SessionID)
	return true
}

func (m *Mailbox) notifyGone(w Waiter) {
	m.mu.Lock()
	gone := m.gone
	m.mu.Unlock()
	if gone != nil {
		gone(w)
	}
}

// Armed reports the session's current waiter.
func (m *Mailbox) Armed(sessionID string) (Waiter, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.waiters[sessionID]
	if !ok {
		return Waiter{}, false
	}
	return entry.waiter, true
}

// Deliver hands the message to the session's armed waiter and returns it. It
// returns false when no waiter is armed; the caller keeps the message.
func (m *Mailbox) Deliver(sessionID string, msg MailboxMessage) (Waiter, bool) {
	m.mu.Lock()
	entry, ok := m.waiters[sessionID]
	if ok {
		delete(m.waiters, sessionID)
	}
	m.mu.Unlock()
	if !ok {
		return Waiter{}, false
	}
	entry.ch <- msg
	close(entry.ch)
	return entry.waiter, true
}

// Close answers every armed waiter 204 so the hooks exit cleanly.
func (m *Mailbox) Close() {
	m.mu.Lock()
	entries := make([]*mailboxWaiter, 0, len(m.waiters))
	for id, entry := range m.waiters {
		delete(m.waiters, id)
		entries = append(entries, entry)
	}
	m.mu.Unlock()
	for _, entry := range entries {
		close(entry.ch)
	}
}

func hookString(hook map[string]any, key string) string {
	v, _ := hook[key].(string)
	return v
}
