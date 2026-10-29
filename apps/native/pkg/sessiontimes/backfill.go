package sessiontimes

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Backfill dates, once per Store, the Sessions whose transcripts are under
// dir (the Daemon's sessions folder, a folder of transcripts per
// Workspace) by the last message the user typed in each, for the messages
// sent before the Store kept them. A Session's later message wins.
func (s *Store) Backfill(dir string) error {
	s.mu.Lock()
	done := s.backfilled
	s.mu.Unlock()
	if done {
		return nil
	}
	found := map[string]int64{}
	files, err := filepath.Glob(filepath.Join(dir, "*", "*.jsonl"))
	if err != nil {
		return err
	}
	for _, path := range files {
		if at, err := lastTyped(path); err == nil && at > 0 {
			found[strings.TrimSuffix(filepath.Base(path), ".jsonl")] = at
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, at := range found {
		t := s.times[id]
		t.SentAt = max(t.SentAt, at)
		s.times[id] = t
	}
	s.backfilled = true
	s.changedLocked()
	for id := range found {
		s.changedAt[id] = s.rev
	}
	return nil
}

// readChunk is how much of a line is read before it is judged: a line
// that is no typed message is skipped, however long, without being kept.
const readChunk = 64 << 10

// lastTyped is when the user last typed a message in the transcript at
// path, in Unix ms; 0 for never. A typed message is a user message with
// text, no tool result, no hook's record and no visibility of its own
// (droid marks with one what it adds for the model or for the screen).
func lastTyped(path string) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, readChunk)
	var last int64
	var line []byte
	skip, first := false, true
	for {
		chunk, err := r.ReadSlice('\n')
		if first {
			skip = !maybeTyped(chunk)
			first = false
		}
		if !skip {
			line = append(line, chunk...)
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if !skip {
			if at := typedAt(line); at > last {
				last = at
			}
		}
		line, first = line[:0], true
		if errors.Is(err, io.EOF) {
			return last, nil
		}
		if err != nil {
			return last, err
		}
	}
}

// maybeTyped judges a line by its start, where droid writes the type and
// the message's role ahead of its content.
func maybeTyped(start []byte) bool {
	return bytes.Contains(start, []byte(`"type":"message"`)) && bytes.Contains(start, []byte(`"role":"user"`)) &&
		!bytes.Contains(start, []byte(`"tool_result"`))
}

func typedAt(line []byte) int64 {
	if bytes.Contains(line, []byte(`"hookEventName"`)) {
		return 0
	}
	var m struct {
		Type      string `json:"type"`
		Timestamp string `json:"timestamp"`
		Message   struct {
			Role          string `json:"role"`
			Visibility    string `json:"visibility"`
			HookEventName string `json:"hookEventName"`
			Content       []struct {
				Type string `json:"type"`
			} `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &m) != nil || m.Type != "message" || m.Message.Role != "user" || m.Message.Visibility != "" || m.Message.HookEventName != "" {
		return 0
	}
	text := false
	for _, c := range m.Message.Content {
		switch c.Type {
		case "text":
			text = true
		case "tool_result":
			return 0
		}
	}
	if !text {
		return 0
	}
	at, err := time.Parse(time.RFC3339Nano, m.Timestamp)
	if err != nil {
		return 0
	}
	return at.UnixMilli()
}
