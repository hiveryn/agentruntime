package codex

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hiveryn/agentruntime"
)

const (
	threadA = "019eb65a-a50a-7353-b9ec-88fd2de2d323"
	threadB = "0a0a0a0a-1111-7222-8333-444444444444"
	segA1   = "019eb6ff-0001-7353-b9ec-000000000001"
	segA2   = "019eb6ff-0002-7353-b9ec-000000000002"
)

// writeSegment writes sessions/<bucket>/rollout-<ts>-<thread>[_<segment>].jsonl
// opening with a session_meta that identifies metaID (the thread id, or
// another thread's to fake a misnamed file), followed by lines.
func writeSegment(t *testing.T, root, bucket, ts, thread, segment, metaID string, lines ...string) string {
	t.Helper()
	if metaID == "" {
		metaID = thread
	}
	dir := filepath.Join(root, "sessions", bucket)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	name := "rollout-" + ts + "-" + thread
	if segment != "" {
		name += "_" + segment
	}
	path := filepath.Join(dir, name+".jsonl")
	stamp, err := time.ParseInLocation("2006-01-02T15-04-05", ts, time.Local)
	if err != nil {
		t.Fatal(err)
	}
	meta := `{"timestamp":"` + stamp.UTC().Format(time.RFC3339Nano) + `","type":"session_meta","payload":{"id":"` + metaID + `","timestamp":"` + stamp.UTC().Format(time.RFC3339Nano) + `","cli_version":"0.163.0"`
	if segment != "" {
		meta += `,"history_mode":"paginated","history_base":{"thread_id":"` + thread + `","end_ordinal_exclusive":1,"end_byte_offset":1}`
	}
	meta += "}}"
	content := strings.Join(append([]string{meta}, lines...), "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLocateRolloutSegmentsVerifiesIdentityAndOrdersSegments(t *testing.T) {
	root := t.TempDir()
	adapter := New(DefaultOptions())
	// The thread's first file, two resumed segments in later date buckets,
	// and distractors: another thread whose segment id is thread A's id, a
	// file named for thread A whose session_meta belongs to thread B, and
	// thread B's own first file.
	first := writeSegment(t, root, "2026/06/11", "2026-06-11T11-04-14", threadA, "", "")
	second := writeSegment(t, root, "2026/06/12", "2026-06-12T09-00-00", threadA, segA1, "")
	third := writeSegment(t, root, "2026/06/13", "2026-06-13T08-30-00", threadA, segA2, "")
	writeSegment(t, root, "2026/06/13", "2026-06-13T09-00-00", threadB, threadA, "")
	writeSegment(t, root, "2026/06/14", "2026-06-14T09-00-00", threadA, "0000-misnamed", threadB)
	writeSegment(t, root, "2026/06/14", "2026-06-14T10-00-00", threadB, "", "")

	segments, err := adapter.LocateRolloutSegments(context.Background(), agentruntime.LocateRequest{NativeSessionID: threadA, ConfigRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	if len(segments) != 3 || segments[0].Path != first || segments[1].Path != second || segments[2].Path != third {
		t.Fatalf("segments = %+v", segments)
	}
	if segments[0].SegmentID != "" || segments[0].Continues != "" || segments[1].SegmentID != segA1 || segments[1].Continues != threadA || segments[2].SegmentID != segA2 {
		t.Fatalf("segment identity = %+v", segments)
	}
	if !segments[0].StartedAt.Before(segments[1].StartedAt) || !segments[1].StartedAt.Before(segments[2].StartedAt) {
		t.Fatalf("segment order = %+v", segments)
	}
	// LocateTranscript is the newest segment: where new entries land.
	path, err := adapter.LocateTranscript(context.Background(), agentruntime.LocateRequest{NativeSessionID: threadA, ConfigRoot: root})
	if err != nil || path != third {
		t.Fatalf("transcript = %q %v", path, err)
	}
	// Thread B has exactly its own file; thread A's id in B's segment name
	// never made that file A's.
	path, err = adapter.LocateTranscript(context.Background(), agentruntime.LocateRequest{NativeSessionID: threadB, ConfigRoot: root})
	if err != nil || !strings.HasSuffix(path, "-"+threadB+".jsonl") {
		t.Fatalf("thread B transcript = %q %v", path, err)
	}
}

func TestLocateRolloutSegmentsAcceptsLegacyRolloutsAndFilenameStamps(t *testing.T) {
	root := t.TempDir()
	adapter := New(DefaultOptions())
	// 2025 CLIs opened the file with the session object itself and no
	// session_meta; the filename stamp orders it.
	dir := filepath.Join(root, "sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(dir, "rollout-2025-07-09T07-46-23-"+threadA+".jsonl")
	if err := os.WriteFile(legacy, []byte(`{"id":"`+threadA+`","instructions":"...","git":{}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	segments, err := adapter.LocateRolloutSegments(context.Background(), agentruntime.LocateRequest{NativeSessionID: threadA, ConfigRoot: root})
	if err != nil || len(segments) != 1 || segments[0].Path != legacy {
		t.Fatalf("legacy = %+v %v", segments, err)
	}
	want := time.Date(2025, 7, 9, 7, 46, 23, 0, time.Local)
	if !segments[0].StartedAt.Equal(want) {
		t.Fatalf("legacy started at %v, want %v", segments[0].StartedAt, want)
	}
	// A legacy file of another thread is excluded, not an error.
	other := filepath.Join(dir, "rollout-2025-07-09T07-50-00-"+threadA+"_x.jsonl")
	if err := os.WriteFile(other, []byte(`{"id":"`+threadB+`"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if segments, err = adapter.LocateRolloutSegments(context.Background(), agentruntime.LocateRequest{NativeSessionID: threadA, ConfigRoot: root}); err != nil || len(segments) != 1 {
		t.Fatalf("with other legacy = %+v %v", segments, err)
	}
}

func TestLocateRolloutSegmentsRefusesUnverifiableEvidence(t *testing.T) {
	root := t.TempDir()
	adapter := New(DefaultOptions())
	// No sessions tree at all: nothing yet, not an error.
	segments, err := adapter.LocateRolloutSegments(context.Background(), agentruntime.LocateRequest{NativeSessionID: threadA, ConfigRoot: root})
	if err != nil || len(segments) != 0 {
		t.Fatalf("absent tree = %+v %v", segments, err)
	}
	if _, err := adapter.LocateTranscript(context.Background(), agentruntime.LocateRequest{NativeSessionID: threadA, ConfigRoot: root}); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("absent transcript err = %v", err)
	}
	writeSegment(t, root, "2026/06/11", "2026-06-11T11-04-14", threadA, "", "")
	// A segment that is being created (empty) or truncated cannot be told
	// apart from a record without the message: an error, not an absence.
	empty := filepath.Join(root, "sessions", "2026", "06", "12", "rollout-2026-06-12T09-00-00-"+threadA+"_"+segA1+".jsonl")
	if err := os.MkdirAll(filepath.Dir(empty), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = adapter.LocateRolloutSegments(context.Background(), agentruntime.LocateRequest{NativeSessionID: threadA, ConfigRoot: root})
	if err == nil || !strings.Contains(err.Error(), "no session_meta line") {
		t.Fatalf("empty segment err = %v", err)
	}
	if err := os.WriteFile(empty, []byte("not json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = adapter.LocateRolloutSegments(context.Background(), agentruntime.LocateRequest{NativeSessionID: threadA, ConfigRoot: root}); err == nil || !strings.Contains(err.Error(), "not JSON") {
		t.Fatalf("garbled segment err = %v", err)
	}
	if err := os.WriteFile(empty, []byte(`{"type":"turn_context","payload":{}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = adapter.LocateRolloutSegments(context.Background(), agentruntime.LocateRequest{NativeSessionID: threadA, ConfigRoot: root}); err == nil || !strings.Contains(err.Error(), "no thread identity") {
		t.Fatalf("identity-less segment err = %v", err)
	}
	if _, err = adapter.LocateTranscript(context.Background(), agentruntime.LocateRequest{NativeSessionID: threadA, ConfigRoot: root}); err == nil {
		t.Fatal("LocateTranscript accepted unverifiable evidence")
	}
	// Two files claiming the same segment are ambiguous.
	if err := os.Remove(empty); err != nil {
		t.Fatal(err)
	}
	writeSegment(t, root, "2026/06/12", "2026-06-12T09-00-00", threadA, segA1, "")
	writeSegment(t, root, "2026/06/13", "2026-06-13T09-00-00", threadA, segA1, "")
	if _, err = adapter.LocateRolloutSegments(context.Background(), agentruntime.LocateRequest{NativeSessionID: threadA, ConfigRoot: root}); err == nil || !strings.Contains(err.Error(), "ambiguous rollout segment") {
		t.Fatalf("duplicate segment err = %v", err)
	}
}

func itemLine(ts, kind, clientID string) string {
	item := `{"type":"` + kind + `","id":"i"`
	if clientID != "" {
		item += `,"client_id":"` + clientID + `"`
	}
	return `{"timestamp":"` + ts + `","type":"event_msg","payload":{"type":"item_completed","item":` + item + `}}}`
}

func TestConfirmMessageReadsReceiptsAcrossSegments(t *testing.T) {
	adapter := New(DefaultOptions())
	command, queueFile := fakeCodex(t)
	home := t.TempDir()
	env := map[string]string{"CODEX_HOME": home, "FAKE_QUEUE_FILE": queueFile, "FAKE_EXPECT_HOME": home}
	const msg = "msg-uuid-1"
	// The first file has no receipt; the message ran in the first resumed
	// segment; a later resume opened a third segment that holds the reply
	// and nothing about the message. The receipt check must find it.
	writeSegment(t, home, "2026/10/20", "2026-10-20T06-27-35", threadA, "", "",
		itemLine("2026-10-20T05:28:00Z", "UserMessage", ""),
		itemLine("2026-10-20T05:54:16Z", "AgentMessage", ""))
	writeSegment(t, home, "2026/10/20", "2026-10-20T06-54-17", threadA, segA1, "",
		itemLine("2026-10-20T08:50:46Z", "UserMessage", msg))
	// The reply is dated past every segment's session_meta time whatever the
	// test machine's zone makes of the filename stamps.
	writeSegment(t, home, "2026/10/20", "2026-10-20T09-53-22", threadA, segA2, "",
		itemLine("2026-10-20T23:07:26Z", "AgentMessage", ""))

	stale, err := adapter.LocateTranscript(context.Background(), agentruntime.LocateRequest{NativeSessionID: threadA, ConfigRoot: home})
	if err != nil || !strings.Contains(stale, segA2) {
		t.Fatalf("transcript = %q %v", stale, err)
	}
	// With the thread known, the handle is not trusted alone.
	receipt, err := adapter.ConfirmMessage(context.Background(), agentruntime.ConfirmRequest{TranscriptHandle: stale, Command: command, Env: env, NativeSessionID: threadA, MessageID: msg})
	if err != nil || !receipt.Appeared || !receipt.Acknowledged || receipt.Queued {
		t.Fatalf("across segments = %+v %v", receipt, err)
	}
	if want := time.Date(2026, 10, 20, 23, 7, 26, 0, time.UTC); !receipt.RecordedThrough.Equal(want) {
		t.Fatalf("recorded through %v, want %v", receipt.RecordedThrough, want)
	}
	// Absent from every segment and not queued: an authoritative absence
	// that still says how far the record reaches.
	receipt, err = adapter.ConfirmMessage(context.Background(), agentruntime.ConfirmRequest{TranscriptHandle: stale, Command: command, Env: env, NativeSessionID: threadA, MessageID: "msg-uuid-9"})
	if err != nil || receipt.Appeared || receipt.Acknowledged || receipt.Queued || receipt.RecordedThrough.IsZero() {
		t.Fatalf("absent = %+v %v", receipt, err)
	}
	// An unreadable segment makes the whole check an error.
	broken := filepath.Join(home, "sessions", "2026", "10", "21", "rollout-2026-10-21T01-00-00-"+threadA+"_broken.jsonl")
	if err := os.MkdirAll(filepath.Dir(broken), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(broken, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = adapter.ConfirmMessage(context.Background(), agentruntime.ConfirmRequest{Command: command, Env: env, NativeSessionID: threadA, MessageID: msg}); err == nil || !strings.Contains(err.Error(), "no session_meta line") {
		t.Fatalf("unreadable segment err = %v", err)
	}
	if err := os.Remove(broken); err != nil {
		t.Fatal(err)
	}
	// Without the thread, the handle alone is scanned (one segment).
	receipt, err = adapter.ConfirmMessage(context.Background(), agentruntime.ConfirmRequest{TranscriptHandle: stale, MessageID: msg})
	if err != nil || receipt.Appeared {
		t.Fatalf("handle only = %+v %v", receipt, err)
	}
	// No rollout at all: only the queue is consulted.
	fresh := t.TempDir()
	freshEnv := map[string]string{"CODEX_HOME": fresh, "FAKE_QUEUE_FILE": queueFile, "FAKE_EXPECT_HOME": fresh}
	if _, err := adapter.DeliverMessage(context.Background(), agentruntime.DeliverRequest{Command: command, Env: freshEnv, NativeSessionID: threadB, MessageID: "msg-uuid-2", Text: "x"}); err != nil {
		t.Fatal(err)
	}
	receipt, err = adapter.ConfirmMessage(context.Background(), agentruntime.ConfirmRequest{Command: command, Env: freshEnv, NativeSessionID: threadB, MessageID: "msg-uuid-2"})
	if err != nil || receipt.Appeared || !receipt.Queued || !receipt.RecordedThrough.IsZero() {
		t.Fatalf("queue only = %+v %v", receipt, err)
	}
}
