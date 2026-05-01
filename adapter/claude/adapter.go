package claude

import (
	"crypto/rand"
	"fmt"

	"github.com/hiveryn/agentruntime"
)

type Options struct {
	AppendInstructions bool
	NewSessionID       func() (string, error)
}

type Adapter struct {
	options Options
}

func New(options Options) *Adapter {
	if options.NewSessionID == nil {
		options.NewSessionID = newSessionID
	}
	return &Adapter{options: options}
}

func DefaultOptions() Options {
	return Options{}
}

func (a *Adapter) Agent() agentruntime.AgentKind {
	return agentruntime.AgentClaude
}

func newSessionID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate session ID: %w", err)
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		raw[0:4],
		raw[4:6],
		raw[6:8],
		raw[8:10],
		raw[10:16],
	), nil
}
