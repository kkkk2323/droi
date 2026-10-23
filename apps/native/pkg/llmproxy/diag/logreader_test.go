package diag

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLogReaderReadsArchivesThenOnlyWhatIsNew(t *testing.T) {
	path := filepath.Join(t.TempDir(), "requests.jsonl")
	// Small enough that every few records rotate the log.
	sink, err := OpenFileSink(path, 300, -1)
	if err != nil {
		t.Fatal(err)
	}
	defer sink.Close()
	write := func(seq int) {
		rec := RequestRecord{Seq: seq, Time: time.Date(2026, 10, 1, 0, 0, seq, 0, time.UTC), Model: "claude-opus-5-5", Status: 200}
		b := fmt.Appendf(nil, `{"seq":%d,"time":%q,"model":%q,"status":200}`, rec.Seq, rec.Time.Format(time.RFC3339Nano), rec.Model)
		if err := sink.Write(b); err != nil {
			t.Fatal(err)
		}
	}
	r := NewLogReader(path)
	var got []int
	read := func() {
		t.Helper()
		if err := r.Read(func(rec RequestRecord) { got = append(got, rec.Seq) }); err != nil {
			t.Fatal(err)
		}
	}
	for i := 1; i <= 7; i++ {
		write(i)
	}
	read()
	if archives, _ := filepath.Glob(path + ".*"); len(archives) == 0 {
		t.Fatal("the test log did not rotate")
	}
	// Records written after the first read, across another rotation, come
	// once each and in order.
	for i := 8; i <= 15; i++ {
		write(i)
	}
	read()
	read()
	if len(got) != 15 {
		t.Fatalf("read %v", got)
	}
	for i, seq := range got {
		if seq != i+1 {
			t.Fatalf("read %v", got)
		}
	}
}

func TestLogReaderLeavesAnUnfinishedLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "requests.jsonl")
	if err := os.WriteFile(path, []byte(`{"seq":1,"time":"2026-10-01T00:00:00Z"}`+"\n"+`{"seq":2,"time":"2026-10-01T00:00:01Z"`), 0o600); err != nil {
		t.Fatal(err)
	}
	r := NewLogReader(path)
	var got []int
	_ = r.Read(func(rec RequestRecord) { got = append(got, rec.Seq) })
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	f.WriteString("}\n")
	f.Close()
	_ = r.Read(func(rec RequestRecord) { got = append(got, rec.Seq) })
	if len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("read %v", got)
	}
	if err := NewLogReader(filepath.Join(t.TempDir(), "none.jsonl")).Read(func(RequestRecord) {}); err != nil {
		t.Fatalf("a missing log: %v", err)
	}
}
