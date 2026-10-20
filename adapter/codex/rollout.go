package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/hiveryn/agentruntime"
)

// Rollout segments. Codex stores a thread's record under
// <configRoot>/sessions/YYYY/MM/DD/ (older layouts: flat under sessions/).
// The first file of a thread is rollout-<ts>-<thread id>.jsonl. Since the
// paginated history mode (0.16x), `codex resume` does not append to that file:
// it starts a new segment rollout-<ts>-<thread id>_<segment id>.jsonl whose
// session_meta still carries the thread id as payload.id and whose
// history_base names the segment it continues. Every segment is part of the
// one conversation, and a receipt (a user message carrying a client_id) can
// live in any of them — including an older segment after a later resume, when
// the message ran before the latest one started.
//
// Identity is verified from the file, never inferred from a filename
// substring: a candidate is accepted only when its first line is a
// session_meta whose payload.id is the thread id (or, for pre-session_meta
// rollouts, a top-level id). A verified other thread is excluded; a candidate
// that cannot be verified (empty, truncated, unreadable) is an error, so a
// caller never mistakes incomplete evidence for an absent receipt.

// RolloutSegment is one file of a thread's record.
type RolloutSegment struct {
	Path string
	// SegmentID is the id after the underscore of a resumed segment; empty
	// for the thread's first file.
	SegmentID string
	// Continues is history_base.thread_id: the segment this one resumes
	// (the thread id for the first resume, the previous segment id after
	// that); empty for the first file.
	Continues string
	// StartedAt is the session_meta time, or the filename's local stamp when
	// the file carries none.
	StartedAt time.Time
}

// LocateRolloutSegments returns every verified rollout segment of the thread
// req.NativeSessionID under its CODEX_HOME, oldest first. An empty result with
// a nil error means no segment exists yet; an error means the record could
// not be established (unreadable sessions tree, a candidate that cannot be
// verified, two files claiming the same segment).
func (a *Adapter) LocateRolloutSegments(_ context.Context, req agentruntime.LocateRequest) ([]RolloutSegment, error) {
	if req.NativeSessionID == "" {
		return nil, fmt.Errorf("codex: locate rollout segments: empty native session id")
	}
	home, err := codexHome(req.ConfigRoot)
	if err != nil {
		return nil, fmt.Errorf("codex: locate rollout segments: resolve codex home: %w", err)
	}
	sessions := filepath.Join(home, "sessions")

	var (
		segments   []RolloutSegment
		verifyErrs []error
		seen       = map[string]string{}
	)
	walkErr := filepath.WalkDir(sessions, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		stamp, segmentID, ok := parseRolloutName(d.Name(), req.NativeSessionID)
		if !ok {
			return nil
		}
		segment, mine, err := verifyRolloutSegment(path, req.NativeSessionID)
		if err != nil {
			verifyErrs = append(verifyErrs, err)
			return nil
		}
		if !mine {
			return nil
		}
		if previous, dup := seen[segmentID]; dup {
			verifyErrs = append(verifyErrs, fmt.Errorf("codex: ambiguous rollout segment %q for thread %q: %s and %s", segmentID, req.NativeSessionID, previous, path))
			return nil
		}
		seen[segmentID] = path
		segment.SegmentID = segmentID
		if segment.StartedAt.IsZero() {
			segment.StartedAt = stamp
		}
		segments = append(segments, segment)
		return nil
	})
	if walkErr != nil {
		if errors.Is(walkErr, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("codex: locate rollout segments: walk %q: %w", sessions, walkErr)
	}
	if len(verifyErrs) > 0 {
		return nil, errors.Join(verifyErrs...)
	}
	sort.SliceStable(segments, func(i, j int) bool {
		if !segments[i].StartedAt.Equal(segments[j].StartedAt) {
			return segments[i].StartedAt.Before(segments[j].StartedAt)
		}
		// The first file precedes any resumed segment started in the same second.
		return segments[i].SegmentID == "" && segments[j].SegmentID != ""
	})
	return segments, nil
}

// parseRolloutName recognizes rollout-<ts>-<thread>.jsonl and
// rollout-<ts>-<thread>_<segment>.jsonl for the given thread and returns the
// filename stamp (local time, second precision; zero when unparsable) and the
// segment id.
func parseRolloutName(name, threadID string) (stamp time.Time, segmentID string, ok bool) {
	stem, found := strings.CutSuffix(name, ".jsonl")
	if !found {
		return time.Time{}, "", false
	}
	stem, found = strings.CutPrefix(stem, "rollout-")
	if !found {
		return time.Time{}, "", false
	}
	base, segmentID, _ := strings.Cut(stem, "_")
	if segmentID != "" && strings.Contains(segmentID, "_") {
		return time.Time{}, "", false
	}
	prefix, found := strings.CutSuffix(base, "-"+threadID)
	if !found || prefix == "" {
		return time.Time{}, "", false
	}
	stamp, _ = time.ParseInLocation("2006-01-02T15-04-05", prefix, time.Local)
	return stamp, segmentID, true
}

// verifyRolloutSegment reads the first line of a candidate and says whether
// it is a record of threadID. A line that identifies another thread is not
// an error; a line that identifies nothing is.
func verifyRolloutSegment(path, threadID string) (RolloutSegment, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return RolloutSegment{}, false, fmt.Errorf("codex: verify rollout %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()
	reader := bufio.NewReaderSize(file, 1024*1024)
	line, err := reader.ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return RolloutSegment{}, false, fmt.Errorf("codex: verify rollout %s: no session_meta line (%v)", path, err)
	}
	var head struct {
		Type      string `json:"type"`
		Timestamp string `json:"timestamp"`
		ID        string `json:"id"`
		Payload   struct {
			ID          string `json:"id"`
			Timestamp   string `json:"timestamp"`
			HistoryBase struct {
				ThreadID string `json:"thread_id"`
			} `json:"history_base"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(line, &head); err != nil {
		return RolloutSegment{}, false, fmt.Errorf("codex: verify rollout %s: first line is not JSON: %w", path, err)
	}
	segment := RolloutSegment{Path: path}
	switch {
	case head.Type == "session_meta" && head.Payload.ID != "":
		if head.Payload.ID != threadID {
			return RolloutSegment{}, false, nil
		}
		segment.Continues = head.Payload.HistoryBase.ThreadID
		segment.StartedAt = parseRolloutTime(head.Payload.Timestamp, head.Timestamp)
	case head.Type == "" && head.ID != "":
		// Pre-session_meta rollouts (2025 CLIs) open with the session
		// object itself.
		if head.ID != threadID {
			return RolloutSegment{}, false, nil
		}
		segment.StartedAt = parseRolloutTime(head.Timestamp)
	default:
		return RolloutSegment{}, false, fmt.Errorf("codex: verify rollout %s: first line carries no thread identity", path)
	}
	return segment, true, nil
}

// parseRolloutTime returns the first parsable RFC 3339 value.
func parseRolloutTime(values ...string) time.Time {
	for _, v := range values {
		if v == "" {
			continue
		}
		if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
			return t
		}
	}
	return time.Time{}
}
