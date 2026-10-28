// Package chatwire contains bounded JSON framing, not provider or process policy.
package chatwire

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/hiveryn/agentruntime"
)

type Object = map[string]any

func String(m Object, k string) string { v, _ := m[k].(string); return v }
func Map(m Object, k string) Object    { v, _ := m[k].(map[string]any); return v }
func List(m Object, k string) []any    { v, _ := m[k].([]any); return v }

type result struct {
	value Object
	err   error
}
type Wire struct {
	in  chan result
	out *json.Encoder
	seq int
}

func New(ctx context.Context, r io.Reader, w io.Writer) *Wire {
	c := &Wire{in: make(chan result, 32), out: json.NewEncoder(w)}
	go func() {
		defer close(c.in)
		s := bufio.NewScanner(r)
		s.Buffer(make([]byte, 4096), 16<<20)
		for s.Scan() {
			var m Object
			err := json.Unmarshal(s.Bytes(), &m)
			select {
			case c.in <- result{m, err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
		err := s.Err()
		if err == nil {
			err = io.EOF
		}
		select {
		case c.in <- result{err: err}:
		case <-ctx.Done():
		}
	}()
	return c
}
func (w *Wire) Send(v any) error { return w.out.Encode(v) }
func (w *Wire) Read(ctx context.Context) (Object, error) {
	select {
	case r, ok := <-w.in:
		if !ok {
			return nil, io.EOF
		}
		return r.value, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (w *Wire) Call(ctx context.Context, method string, params any, observe func(Object) error) (Object, error) {
	w.seq++
	id := w.seq
	if err := w.Send(Object{"id": id, "method": method, "params": params}); err != nil {
		return nil, err
	}
	for {
		m, err := w.Read(ctx)
		if err != nil {
			return nil, err
		}
		if m["id"] == float64(id) && m["method"] == nil {
			if e := m["error"]; e != nil {
				return nil, fmt.Errorf("%s: %v", method, e)
			}
			return Map(m, "result"), nil
		}
		if observe != nil {
			if err := observe(m); err != nil {
				return nil, err
			}
		}
	}
}

// TextContent extracts only visible text blocks from native tool content. It
// deliberately does not stringify arbitrary payloads (reasoning, binary data,
// metadata and instruction/config objects are not conversation output).
func TextContent(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case []any:
		parts := []string{}
		for _, raw := range v {
			part, ok := raw.(map[string]any)
			if ok && String(part, "type") == "text" {
				parts = append(parts, String(part, "text"))
			}
		}
		return strings.Join(parts, "\n")
	default:
		return ""
	}
}

// AcceptOnFirst marks the first event observed in a turn with the acceptance
// evidence name; later events pass through unchanged.
func AcceptOnFirst(observe func(agentruntime.ConversationEvent) error, evidence string) func(agentruntime.ConversationEvent) error {
	accepted := false
	return func(e agentruntime.ConversationEvent) error {
		if !accepted {
			accepted = true
			e.Acceptance = evidence
		}
		return observe(e)
	}
}
