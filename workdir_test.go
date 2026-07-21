package agentruntime

import (
	"strings"
	"testing"
)

func TestNormalizeAdditionalWorkdirs_HappyPath(t *testing.T) {
	out, err := NormalizeAdditionalWorkdirs("/repo-a", []string{"/repo-b", "/repo-c"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/repo-b", "/repo-c"}
	if len(out) != len(want) {
		t.Fatalf("got %v, want %v", out, want)
	}
	for i := range want {
		if out[i] != want[i] {
			t.Errorf("index %d: got %q want %q", i, out[i], want[i])
		}
	}
}

func TestNormalizeAdditionalWorkdirs_TrimsWhitespace(t *testing.T) {
	out, err := NormalizeAdditionalWorkdirs("/repo-a", []string{"  /repo-b  "})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0] != "/repo-b" {
		t.Fatalf("got %v", out)
	}
}

func TestNormalizeAdditionalWorkdirs_RejectsRelative(t *testing.T) {
	for _, dir := range []string{"repo-b", "./repo-b"} {
		_, err := NormalizeAdditionalWorkdirs("/repo-a", []string{dir})
		if err == nil {
			t.Fatalf("expected error for relative path %q", dir)
		}
		if !strings.Contains(err.Error(), "absolute") {
			t.Errorf("error should mention absolute: %v", err)
		}
		if !strings.Contains(err.Error(), "index 0") {
			t.Errorf("error should mention index: %v", err)
		}
	}
}

func TestNormalizeAdditionalWorkdirs_RejectsEmptyOrBlank(t *testing.T) {
	for _, dir := range []string{"", "   "} {
		_, err := NormalizeAdditionalWorkdirs("/repo-a", []string{"/repo-b", dir})
		if err == nil {
			t.Fatalf("expected error for blank entry %q", dir)
		}
		if !strings.Contains(err.Error(), "index 1") {
			t.Errorf("error should mention index 1: %v", err)
		}
	}
}

func TestNormalizeAdditionalWorkdirs_RejectsDuplicateAmongAdditional(t *testing.T) {
	_, err := NormalizeAdditionalWorkdirs("/repo-a", []string{"/repo-b", "/repo-b"})
	if err == nil {
		t.Fatal("expected error for duplicate additional workdir")
	}
	if !strings.Contains(err.Error(), "index 1") || !strings.Contains(err.Error(), "index 0") {
		t.Errorf("error should name both indices: %v", err)
	}
}

func TestNormalizeAdditionalWorkdirs_RejectsDuplicateOfWorkdir(t *testing.T) {
	_, err := NormalizeAdditionalWorkdirs("/repo-a", []string{"/repo-a"})
	if err == nil {
		t.Fatal("expected error for duplicate of primary workdir")
	}
	if !strings.Contains(err.Error(), "primary workdir") {
		t.Errorf("error should mention primary workdir: %v", err)
	}
}

func TestNormalizeAdditionalWorkdirs_CleansPaths(t *testing.T) {
	out, err := NormalizeAdditionalWorkdirs("/repo-a", []string{"/repo-b/../repo-b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0] != "/repo-b" {
		t.Fatalf("got %v", out)
	}

	// A cleaned duplicate of an already-listed dir must still be rejected.
	_, err = NormalizeAdditionalWorkdirs("/repo-a", []string{"/repo-b", "/repo-b/../repo-b"})
	if err == nil {
		t.Fatal("expected error for path that cleans to an existing duplicate")
	}
}

func TestNormalizeAdditionalWorkdirs_EmptyInput(t *testing.T) {
	out, err := NormalizeAdditionalWorkdirs("/repo-a", nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != nil {
		t.Fatalf("expected nil, got %v", out)
	}

	out, err = NormalizeAdditionalWorkdirs("/repo-a", []string{})
	if err != nil {
		t.Fatal(err)
	}
	if out != nil {
		t.Fatalf("expected nil, got %v", out)
	}
}
