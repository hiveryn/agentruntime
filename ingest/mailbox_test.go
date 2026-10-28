package ingest

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func waitBody(event, source, session string) []byte {
	hook := map[string]any{"hook_event_name": event, "session_id": "native-" + session}
	if source != "" {
		hook["source"] = source
	}
	b, _ := json.Marshal(map[string]any{"hook": hook, "env": map[string]string{"AGENTRUNTIME_SESSION_ID": session}})
	return b
}

type observed struct {
	mu    sync.Mutex
	armed []Waiter
	gone  []Waiter
}

func (o *observed) snapshot() (armed, gone []Waiter) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]Waiter(nil), o.armed...), append([]Waiter(nil), o.gone...)
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met")
}

func TestMailboxHandsMessageToArmedWaiter(t *testing.T) {
	m := NewMailbox()
	obs := &observed{}
	m.SetObservers(func(w Waiter) { obs.mu.Lock(); obs.armed = append(obs.armed, w); obs.mu.Unlock() },
		func(w Waiter) { obs.mu.Lock(); obs.gone = append(obs.gone, w); obs.mu.Unlock() })
	server := httptest.NewServer(m.Handler())
	defer server.Close()

	type result struct {
		status int
		body   string
	}
	results := make(chan result, 1)
	go func() {
		resp, err := http.Post(server.URL, "application/json", bytes.NewReader(waitBody("Stop", "", "s1")))
		if err != nil {
			results <- result{status: -1, body: err.Error()}
			return
		}
		defer func() { _ = resp.Body.Close() }()
		b, _ := io.ReadAll(resp.Body)
		results <- result{resp.StatusCode, string(b)}
	}()
	waitFor(t, func() bool { _, ok := m.Armed("s1"); return ok })
	w, _ := m.Armed("s1")
	if w.HookEvent != "Stop" || w.NativeSessionID != "native-s1" {
		t.Fatalf("waiter = %+v", w)
	}
	armed, _ := obs.snapshot()
	if len(armed) != 1 || armed[0].SessionID != "s1" {
		t.Fatalf("armed observer = %+v", armed)
	}

	if _, ok := m.Deliver("other", MailboxMessage{ID: "m0", Text: "x"}); ok {
		t.Fatal("delivered to a session without a waiter")
	}
	delivered, ok := m.Deliver("s1", MailboxMessage{ID: "m1", Text: "Hiveryn message m1: hello"})
	if !ok || delivered.HookEvent != "Stop" {
		t.Fatalf("deliver = %+v %v", delivered, ok)
	}
	r := <-results
	if r.status != http.StatusOK {
		t.Fatalf("status %d body %s", r.status, r.body)
	}
	var msg MailboxMessage
	if err := json.Unmarshal([]byte(r.body), &msg); err != nil || msg.ID != "m1" || msg.Text != "Hiveryn message m1: hello" {
		t.Fatalf("body %q err %v", r.body, err)
	}
	if _, ok := m.Armed("s1"); ok {
		t.Fatal("waiter still armed after hand-off")
	}
}

func TestMailboxNewestWaiterSupersedesWith204AndDisconnectForgets(t *testing.T) {
	m := NewMailbox()
	obs := &observed{}
	m.SetObservers(nil, func(w Waiter) { obs.mu.Lock(); obs.gone = append(obs.gone, w); obs.mu.Unlock() })
	server := httptest.NewServer(m.Handler())
	defer server.Close()

	first := make(chan int, 1)
	go func() {
		resp, err := http.Post(server.URL, "application/json", bytes.NewReader(waitBody("SessionStart", "startup", "s1")))
		if err != nil {
			first <- -1
			return
		}
		_ = resp.Body.Close()
		first <- resp.StatusCode
	}()
	waitFor(t, func() bool { w, ok := m.Armed("s1"); return ok && w.HookEvent == "SessionStart" })

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	second := make(chan error, 1)
	go func() {
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, server.URL, bytes.NewReader(waitBody("Stop", "", "s1")))
		_, err := http.DefaultClient.Do(req)
		second <- err
	}()
	waitFor(t, func() bool { w, ok := m.Armed("s1"); return ok && w.HookEvent == "Stop" })
	if status := <-first; status != http.StatusNoContent {
		t.Fatalf("superseded waiter status = %d", status)
	}
	_, gone := obs.snapshot()
	if len(gone) != 1 || gone[0].HookEvent != "SessionStart" || gone[0].Source != "startup" {
		t.Fatalf("gone observer = %+v", gone)
	}

	cancel()
	if err := <-second; err == nil {
		t.Fatal("cancelled request returned without error")
	}
	waitFor(t, func() bool { _, ok := m.Armed("s1"); return !ok })
	_, gone = obs.snapshot()
	if len(gone) != 2 || gone[1].HookEvent != "Stop" {
		t.Fatalf("gone observer after disconnect = %+v", gone)
	}
}

func TestMailboxRejectsEnvelopeWithoutSession(t *testing.T) {
	m := NewMailbox()
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/claude/wait", bytes.NewReader([]byte(`{"hook":{"hook_event_name":"Stop"}}`))))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/claude/wait", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestMailboxCloseAnswersEveryWaiter(t *testing.T) {
	m := NewMailbox()
	server := httptest.NewServer(m.Handler())
	defer server.Close()
	statuses := make(chan int, 2)
	for _, s := range []string{"a", "b"} {
		go func(s string) {
			resp, err := http.Post(server.URL, "application/json", bytes.NewReader(waitBody("Stop", "", s)))
			if err != nil {
				statuses <- -1
				return
			}
			_ = resp.Body.Close()
			statuses <- resp.StatusCode
		}(s)
	}
	waitFor(t, func() bool { _, a := m.Armed("a"); _, b := m.Armed("b"); return a && b })
	m.Close()
	for i := 0; i < 2; i++ {
		if status := <-statuses; status != http.StatusNoContent {
			t.Fatalf("status %d", status)
		}
	}
}

// Interrupt answers a retryable 503, so the hook re-arms instead of exiting,
// and leaves other sessions' waiters armed.
func TestMailboxInterruptReleasesForRetry(t *testing.T) {
	m := NewMailbox()
	server := httptest.NewServer(m.Handler())
	defer server.Close()
	defer m.Close()
	post := func(session string) <-chan int {
		out := make(chan int, 1)
		go func() {
			resp, err := http.Post(server.URL, "application/json", strings.NewReader(`{"hook":{"hook_event_name":"Stop","session_id":"n"},"env":{"AGENTRUNTIME_SESSION_ID":"`+session+`"}}`))
			if err != nil {
				out <- -1
				return
			}
			_ = resp.Body.Close()
			out <- resp.StatusCode
		}()
		return out
	}
	a, b := post("a"), post("b")
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, okA := m.Armed("a")
		_, okB := m.Armed("b")
		if okA && okB {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("waiters not armed")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !m.Interrupt("a") || m.Interrupt("missing") {
		t.Fatal("interrupt result")
	}
	if status := <-a; status != http.StatusServiceUnavailable {
		t.Fatalf("interrupted waiter status %d", status)
	}
	if _, ok := m.Armed("b"); !ok {
		t.Fatal("other session's waiter released")
	}
	m.Close()
	if status := <-b; status != http.StatusNoContent {
		t.Fatalf("closed waiter status %d", status)
	}
}

func waitBodyStarted(event, session string, started int64, attempt int) []byte {
	b, _ := json.Marshal(map[string]any{
		"hook":   map[string]any{"hook_event_name": event, "session_id": "native-" + session},
		"env":    map[string]string{"AGENTRUNTIME_SESSION_ID": session},
		"waiter": map[string]any{"started": started, "attempt": attempt},
	})
	return b
}

// A waiter whose post was delayed (lost connection, restarting receiver)
// arrives after a waiter whose hook started later: it is answered 204 and the
// later one stays armed, unobserved by the armed observer.
func TestMailboxKeepsTheLaterStartedWaiterOverADelayedRetry(t *testing.T) {
	m := NewMailbox()
	obs := &observed{}
	m.SetObservers(func(w Waiter) { obs.mu.Lock(); obs.armed = append(obs.armed, w); obs.mu.Unlock() }, nil)
	server := httptest.NewServer(m.Handler())
	defer server.Close()
	defer m.Close()
	post := func(body []byte) <-chan int {
		out := make(chan int, 1)
		go func() {
			resp, err := http.Post(server.URL, "application/json", bytes.NewReader(body))
			if err != nil {
				out <- -1
				return
			}
			_ = resp.Body.Close()
			out <- resp.StatusCode
		}()
		return out
	}
	newer := post(waitBodyStarted("Stop", "s1", 2000, 0))
	waitFor(t, func() bool { w, ok := m.Armed("s1"); return ok && w.HookStartedAt.Equal(time.UnixMilli(2000)) })

	if status := <-post(waitBodyStarted("Stop", "s1", 1000, 3)); status != http.StatusNoContent {
		t.Fatalf("delayed older retry status %d", status)
	}
	if w, ok := m.Armed("s1"); !ok || !w.HookStartedAt.Equal(time.UnixMilli(2000)) || w.Attempt != 0 {
		t.Fatalf("armed = %+v, %v", w, ok)
	}
	if armed, _ := obs.snapshot(); len(armed) != 1 {
		t.Fatalf("armed observer = %+v", armed)
	}

	// The later one retrying (same start) and a later hook still supersede.
	m.Interrupt("s1")
	if status := <-newer; status != http.StatusServiceUnavailable {
		t.Fatalf("interrupted status %d", status)
	}
	retry := post(waitBodyStarted("Stop", "s1", 2000, 1))
	waitFor(t, func() bool { w, ok := m.Armed("s1"); return ok && w.Attempt == 1 })
	later := post(waitBodyStarted("Stop", "s1", 3000, 0))
	if status := <-retry; status != http.StatusNoContent {
		t.Fatalf("superseded retry status %d", status)
	}
	waitFor(t, func() bool { w, ok := m.Armed("s1"); return ok && w.HookStartedAt.Equal(time.UnixMilli(3000)) })
	m.Close()
	<-later
}
