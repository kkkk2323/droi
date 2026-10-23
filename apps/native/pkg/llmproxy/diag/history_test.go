package diag

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func historySink(t *testing.T, maxBytes int64) (*Recorder, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "requests.jsonl")
	sink, err := OpenFileSink(path, maxBytes, -1)
	if err != nil {
		t.Fatal(err)
	}
	recorder := NewRecorder(500)
	recorder.PersistRequests(sink)
	t.Cleanup(func() { _ = recorder.Close() })
	return recorder, path
}

func historyRecord(id int) RequestRecord {
	return RequestRecord{Time: time.Unix(int64(id), 0), Model: fmt.Sprintf("model-%d", id), Status: 200}
}

func TestRequestHistoryBeyondRingAcrossRotationAndRestart(t *testing.T) {
	recorder, path := historySink(t, 4096)
	for i := 1; i <= 625; i++ {
		recorder.RecordRequest(historyRecord(i))
	}
	if len(recorder.Requests()) != 500 {
		t.Fatal("test must exceed the in-memory ring")
	}
	first, err := recorder.RequestHistory(100, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Requests) != 100 || first.Requests[0].Model != "model-625" || first.NextCursor == "" {
		t.Fatalf("unexpected first page: count=%d cursor=%q", len(first.Requests), first.NextCursor)
	}
	// Writes and a daemon restart must not shift the cursor or duplicate rows.
	for i := 626; i <= 650; i++ {
		recorder.RecordRequest(historyRecord(i))
	}
	if err := recorder.Close(); err != nil {
		t.Fatal(err)
	}
	sink, err := OpenFileSink(path, 4096, -1)
	if err != nil {
		t.Fatal(err)
	}
	restarted := NewRecorder(500)
	restarted.PersistRequests(sink)
	defer restarted.Close()
	if len(restarted.Requests()) != 0 {
		t.Fatal("restarted ring should be empty")
	}
	all := append([]RequestRecord{}, first.Requests...)
	for cursor := first.NextCursor; cursor != ""; {
		page, err := restarted.RequestHistory(100, cursor)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Requests) == 0 || len(page.Requests) > 100 {
			t.Fatalf("invalid page size: %d", len(page.Requests))
		}
		all = append(all, page.Requests...)
		cursor = page.NextCursor
	}
	if len(all) != 625 {
		t.Fatalf("got %d records, want 625", len(all))
	}
	for i, rec := range all {
		if want := fmt.Sprintf("model-%d", 625-i); rec.Model != want {
			t.Fatalf("record %d = %s, want %s", i, rec.Model, want)
		}
	}
	latest, err := restarted.RequestHistory(100, "")
	if err != nil || latest.Requests[0].Model != "model-650" {
		t.Fatalf("latest page did not include new writes: %v", err)
	}
}

func TestRequestHistoryCursorFollowsRenamedCurrentFile(t *testing.T) {
	recorder, _ := historySink(t, 1<<20)
	for i := 1; i <= 4; i++ {
		recorder.RecordRequest(historyRecord(i))
	}
	first, err := recorder.RequestHistory(2, "")
	if err != nil {
		t.Fatal(err)
	}
	recorder.requestSink.mu.Lock()
	err = recorder.requestSink.rotateLocked()
	recorder.requestSink.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	recorder.RecordRequest(historyRecord(5))
	page, err := recorder.RequestHistory(2, first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Requests) != 2 || page.Requests[0].Model != "model-2" || page.Requests[1].Model != "model-1" || page.NextCursor != "" {
		t.Fatalf("cursor shifted after rotation: %+v", page)
	}
}

func TestRequestHistorySkipsInvalidAndUnterminatedLines(t *testing.T) {
	recorder, path := historySink(t, 1<<20)
	var content []byte
	for i := 1; i <= 3; i++ {
		rec := historyRecord(i)
		if i == 2 {
			rec.Model = strings.Repeat("x", 70<<10) // spans multiple read blocks
		}
		data, _ := json.Marshal(rec)
		content = append(content, data...)
		content = append(content, '\n')
		content = append(content, []byte("not-json\nnull\n{}\n\n")...)
	}
	tail, _ := json.Marshal(historyRecord(4))
	content = append(content, tail...) // valid JSON, but not a completed line
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	page, err := recorder.RequestHistory(10, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Requests) != 3 || page.Requests[0].Model != "model-3" || len(page.Requests[1].Model) != 70<<10 || page.Requests[2].Model != "model-1" || page.NextCursor != "" {
		t.Fatalf("unexpected records: count=%d cursor=%q", len(page.Requests), page.NextCursor)
	}
}

func TestRequestHistoryEmptyAndValidation(t *testing.T) {
	recorder, _ := historySink(t, 1024)
	page, err := recorder.RequestHistory(100, "")
	if err != nil || page.Requests == nil || len(page.Requests) != 0 || page.NextCursor != "" {
		t.Fatalf("unexpected empty page: %+v, %v", page, err)
	}
	for _, size := range []int{-1, 0, 501} {
		if _, err := recorder.RequestHistory(size, ""); !errors.Is(err, ErrInvalidPageSize) {
			t.Fatalf("size %d: %v", size, err)
		}
	}
	for _, cursor := range []string{"bad!", "e30", base64.RawURLEncoding.EncodeToString([]byte(`{"d":1,"i":123,"o":0}`))} {
		if _, err := recorder.RequestHistory(10, cursor); !errors.Is(err, ErrInvalidCursor) {
			t.Fatalf("cursor %q: %v", cursor, err)
		}
	}
	if _, err := NewRecorder(1).RequestHistory(10, ""); !errors.Is(err, ErrHistoryUnavailable) {
		t.Fatalf("unpersisted history: %v", err)
	}
}

func TestRequestHistoryLegacyArchivesAndExactPageBoundary(t *testing.T) {
	recorder, path := historySink(t, 1024)
	for i := 1; i <= 2; i++ {
		data, _ := json.Marshal(historyRecord(i))
		if err := os.WriteFile(fmt.Sprintf("%s.2000010%d-000000", path, i), append(data, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	page, err := recorder.RequestHistory(2, "")
	if err != nil {
		t.Fatal(err)
	}
	var models []string
	for _, rec := range page.Requests {
		models = append(models, rec.Model)
	}
	if !slices.Equal(models, []string{"model-2", "model-1"}) || page.NextCursor != "" {
		t.Fatalf("legacy records: %v, cursor %q", models, page.NextCursor)
	}
}

func TestRequestHistoryConcurrentWrites(t *testing.T) {
	recorder, _ := historySink(t, 4096)
	var wg sync.WaitGroup
	wg.Go(func() {
		for i := 1; i <= 150; i++ {
			recorder.RecordRequest(historyRecord(i))
		}
	})
	for i := 0; i < 20; i++ {
		if _, err := recorder.RequestHistory(10, ""); err != nil {
			t.Fatal(err)
		}
	}
	wg.Wait()
	page, err := recorder.RequestHistory(1, "")
	if err != nil || len(page.Requests) != 1 || page.Requests[0].Model != "model-150" {
		t.Fatalf("latest record missing: %+v, %v", page, err)
	}
}
