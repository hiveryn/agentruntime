// Package screen holds the structural matching the adapters' InspectScreen
// implementations share. It matches a provider's own dialog or banner in its
// live position on the rendered screen, never loose text anywhere on it.
package screen

import (
	"regexp"
	"strings"
)

// Rows normalizes rendered terminal rows: each row trimmed of surrounding
// whitespace, with inner whitespace runs collapsed to single spaces.
func Rows(lines []string) []string {
	rows := make([]string, len(lines))
	for i, line := range lines {
		rows[i] = strings.Join(strings.Fields(line), " ")
	}
	return rows
}

// LastNonBlank returns the index of the last non-blank row, or -1.
func LastNonBlank(rows []string) int {
	for i := len(rows) - 1; i >= 0; i-- {
		if rows[i] != "" {
			return i
		}
	}
	return -1
}

var numberedOption = regexp.MustCompile(`^\d+\. `)

// Dialog reports whether rows (normalized by Rows) end with a dialog: its
// footer is the last non-blank row, its title is a row of exactly that text
// above the footer, and every option is an entry between the two. An option
// entry may carry a selection marker and a "N. " number. Anchoring on the
// footer as the screen's last row keeps text quoted in the conversation above
// a live prompt from matching.
func Dialog(rows []string, title, footer string, options ...string) bool {
	last := LastNonBlank(rows)
	if last < 0 || rows[last] != footer {
		return false
	}
	top := -1
	for i := last - 1; i >= 0; i-- {
		if rows[i] == title {
			top = i
			break
		}
	}
	if top < 0 {
		return false
	}
	for _, option := range options {
		found := false
		for _, row := range rows[top+1 : last] {
			if optionText(row) == option {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func optionText(row string) string {
	row = strings.TrimSpace(strings.TrimLeft(row, "›❯> "))
	return numberedOption.ReplaceAllString(row, "")
}
