package diag

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRecorder_RingBufferAndNewestFirst(t *testing.T) {
	r := NewRecorder(2)
	for i := 0; i < 3; i++ {
		r.RecordEvent(Event{Time: time.Now(), Kind: "e", Detail: string(rune('a' + i))})
	}
	events := r.Events()
	if len(events) != 2 {
		t.Fatalf("expected capacity 2, got %d", len(events))
	}
	if events[0].Detail != "c" || events[1].Detail != "b" {
		t.Fatalf("expected newest first [c b], got %+v", events)
	}
}

func TestFileSink_PersistsJSONL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "requests.jsonl")
	sink, err := OpenFileSink(path, 1<<20, 3)
	if err != nil {
		t.Fatal(err)
	}
	r := NewRecorder(10)
	r.PersistRequests(sink)
	r.PersistEvents(sink)

	r.RecordRequest(RequestRecord{Time: time.Now(), Model: "claude-fable-5.1", Status: 200})
	r.RecordEvent(Event{Time: time.Now(), Kind: "key_exhausted", Status: 402})
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}

	lines := readLines(t, path)
	if len(lines) != 2 {
		t.Fatalf("expected 2 persisted lines, got %d", len(lines))
	}
	var rec RequestRecord
	if err := json.Unmarshal([]byte(lines[0]), &rec); err != nil {
		t.Fatal(err)
	}
	if rec.Model != "claude-fable-5.1" || rec.Status != 200 {
		t.Fatalf("unexpected persisted request: %+v", rec)
	}
}

func TestFileSink_RotatesWhenLarge(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	sink, err := OpenFileSink(path, 64, 2) // tiny cap to force rotation
	if err != nil {
		t.Fatal(err)
	}
	r := NewRecorder(100)
	r.PersistEvents(sink)
	for i := 0; i < 10; i++ {
		r.RecordEvent(Event{Time: time.Now(), Kind: "e", Detail: "0123456789012345678901234567890123456789"})
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}

	matches, err := filepath.Glob(path + ".*")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) == 0 {
		t.Fatal("expected at least one rotated file")
	}
	if len(matches) > 2 {
		t.Fatalf("expected at most 2 rotated files, got %d", len(matches))
	}
}

func readLines(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if sc.Text() != "" {
			lines = append(lines, sc.Text())
		}
	}
	return lines
}
