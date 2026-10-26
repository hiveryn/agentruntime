package chatwire

import "testing"

func TestTextContentOnlyIncludesVisibleBlocks(t *testing.T) {
	text := TextContent([]any{Object{"type": "text", "text": "first"}, Object{"type": "thinking", "thinking": "PRIVATE"}, Object{"type": "image", "data": "BINARY"}, Object{"type": "text", "text": "tool failure"}})
	if text != "first\ntool failure" {
		t.Fatalf("text=%q", text)
	}
	if TextContent(Object{"instructions": "PRIVATE"}) != "" {
		t.Fatal("stringified arbitrary object")
	}
}
