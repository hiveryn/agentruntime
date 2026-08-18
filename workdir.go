package agentruntime

import (
	"fmt"
	"path/filepath"
	"strings"
)

// NormalizeAdditionalWorkdirs validates and normalizes StartRequest.AdditionalWorkdirs
// against the primary workdir. It trims whitespace and filepath.Clean()s each entry,
// requires every entry be absolute, and rejects duplicates (including duplicates of
// workdir itself, compared after the same normalization). It does not touch the
// filesystem — agentruntime does not own filesystem workflows.
func NormalizeAdditionalWorkdirs(workdir string, dirs []string) ([]string, error) {
	if len(dirs) == 0 {
		return nil, nil
	}

	normalizedWorkdir := filepath.Clean(strings.TrimSpace(workdir))

	out := make([]string, 0, len(dirs))
	seen := map[string]int{} // normalized -> first index seen at (-1 = the primary workdir)
	seen[normalizedWorkdir] = -1

	for i, raw := range dirs {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			return nil, fmt.Errorf("additional workdir at index %d is empty (raw value %q)", i, raw)
		}
		if !filepath.IsAbs(trimmed) {
			return nil, fmt.Errorf("additional workdir at index %d must be an absolute path, got %q", i, raw)
		}
		clean := filepath.Clean(trimmed)
		if firstIdx, ok := seen[clean]; ok {
			if firstIdx == -1 {
				return nil, fmt.Errorf("additional workdir at index %d (%q, normalized %q) duplicates the primary workdir %q", i, raw, clean, workdir)
			}
			return nil, fmt.Errorf("additional workdir at index %d (%q, normalized %q) duplicates additional workdir at index %d", i, raw, clean, firstIdx)
		}
		seen[clean] = i
		out = append(out, clean)
	}

	return out, nil
}

func NormalizeReadOnlyPaths(paths []string) ([]string, error) {
	out := make([]string, 0, len(paths))
	seen := make(map[string]int, len(paths))
	for i, raw := range paths {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || !filepath.IsAbs(trimmed) {
			return nil, fmt.Errorf("read-only path at index %d must be a non-empty absolute path, got %q", i, raw)
		}
		clean := filepath.Clean(trimmed)
		if first, ok := seen[clean]; ok {
			return nil, fmt.Errorf("read-only path at index %d duplicates index %d after normalization (%q)", i, first, clean)
		}
		seen[clean] = i
		out = append(out, clean)
	}
	return out, nil
}
