package agentruntime

import (
	"fmt"
	"strings"
)

// IsPlan validates the mode and reports whether it requests plan mode.
// Empty mode is treated as build. Unknown values return an error.
func (m Mode) IsPlan() (bool, error) {
	switch m {
	case "", ModeBuild:
		return false, nil
	case ModePlan:
		return true, nil
	default:
		return false, fmt.Errorf("unsupported mode %q (want %q or %q)", m, ModeBuild, ModePlan)
	}
}

// FindManagedArg returns the first arg token that matches any of the given flag
// names, treating `--flag`, `--flag=value`, short `-f`, `-f=value`, and the
// attached short form `-fvalue` as matches. It is used by adapters to detect
// raw args that conflict with a first-class field (e.g. --model when Model is
// set), so the conflict can be rejected fail-fast instead of producing two
// contradictory flags.
func FindManagedArg(args []string, flags ...string) (string, bool) {
	for _, arg := range args {
		for _, f := range flags {
			if f == "" {
				continue
			}
			if arg == f || strings.HasPrefix(arg, f+"=") {
				return arg, true
			}
			// Attached short-flag value, e.g. -sworkspace-write for -s.
			if len(f) == 2 && f[0] == '-' && f[1] != '-' && len(arg) > 2 && strings.HasPrefix(arg, f) {
				return arg, true
			}
		}
	}
	return "", false
}
