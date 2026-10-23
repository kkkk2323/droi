package smartwarm

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)

// transcript builds a session file: each step is a role, the content block
// types, an offset from t0 and an optional text length.
type step struct {
	role  string
	kinds string
	at    time.Duration
	text  int
}

func transcript(start string, steps ...step) string {
	var b strings.Builder
	b.WriteString(start + "\n")
	for i, s := range steps {
		var blocks []string
		for _, k := range strings.Split(s.kinds, ",") {
			switch k {
			case "text":
				blocks = append(blocks, fmt.Sprintf(`{"type":"text","text":%q}`, strings.Repeat("x", max(s.text, 1))))
			case "tool_use":
				blocks = append(blocks, `{"type":"tool_use","name":"AskUser","input":{}}`)
			case "tool_result":
				blocks = append(blocks, `{"type":"tool_result","content":"ok"}`)
			}
		}
		fmt.Fprintf(&b, `{"type":"message","id":"m%d","timestamp":%q,"message":{"role":%q,"content":[%s]}}`+"\n",
			i, t0.Add(s.at).Format(time.RFC3339Nano), s.role, strings.Join(blocks, ","))
	}
	return b.String()
}

const mainStart = `{"type":"session_start","id":"s","title":"t"}`

func TestWaitsFromTranscript(t *testing.T) {
	tr := transcript(mainStart,
		step{"user", "text", 0, 10},
		step{"assistant", "text,tool_use", 10 * time.Second, 10},
		step{"user", "tool_result", 12 * time.Minute, 0},
		step{"assistant", "text", 13 * time.Minute, 2000},
		step{"user", "text", 40 * time.Minute, 10},
		step{"assistant", "text", 41 * time.Minute, 10},
	)
	msgs, err := parse(strings.NewReader(tr))
	if err != nil {
		t.Fatal(err)
	}
	ws := waits(msgs)
	want := []wait{
		{Features{State: Waiting}, 12*time.Minute - 10*time.Second},
		{Features{State: Idle, FirstTurn: true, LongReply: true}, 27 * time.Minute},
		{Features{State: Idle}, never},
	}
	if fmt.Sprint(ws) != fmt.Sprint(want) {
		t.Fatalf("waits = %v\nwant    %v", ws, want)
	}

	if _, err := parse(strings.NewReader(transcript(`{"type":"session_start","id":"c","callingSessionId":"p"}`, step{"user", "text", 0, 1}))); err != errSubagent {
		t.Fatalf("subagent transcript: err = %v", err)
	}
}

func gaps(minutes ...float64) []wait {
	var ws []wait
	for _, m := range minutes {
		g := never
		if m >= 0 {
			g = time.Duration(m * float64(time.Minute))
		}
		ws = append(ws, wait{Features{State: Idle}, g})
	}
	return ws
}

func repeat(ws []wait, n int) []wait {
	var out []wait
	for i := 0; i < n; i++ {
		out = append(out, ws...)
	}
	return out
}

func fiveMinute(elapsed time.Duration) Query {
	return Query{
		Features: Features{State: Idle}, Elapsed: elapsed, CoveredUntil: elapsed + 30*time.Second,
		Interval: 270 * time.Second, TTL: 5 * time.Minute, Gain: 24,
	}
}

func TestWorth(t *testing.T) {
	// Half the users come back after 8 minutes, the rest never.
	m := newModel(repeat(gaps(8, -1), 30), t0)
	if worth, ok := m.Worth(fiveMinute(270 * time.Second)); !ok || !worth {
		t.Fatalf("likely return: worth=%v ok=%v", worth, ok)
	}
	// Past 8 minutes only the never-returning waits are left.
	if worth, ok := m.Worth(fiveMinute(9 * time.Minute)); !ok || worth {
		t.Fatalf("after everyone came back: worth=%v ok=%v", worth, ok)
	}

	// One in thirty returns, after 8 minutes: below the 1 in 25 break-even.
	rare := newModel(repeat(append(gaps(8), repeat(gaps(-1), 29)...), 2), t0)
	if worth, _ := rare.Worth(fiveMinute(270 * time.Second)); worth {
		t.Fatal("refreshing for a 1 in 30 return")
	}

	// Returns late in a long wait still justify the refreshes before them.
	late := newModel(repeat(gaps(40, 40, -1), 20), t0)
	if worth, _ := late.Worth(fiveMinute(270 * time.Second)); !worth {
		t.Fatal("a 2 in 3 return at 40 minutes is worth 9 refreshes")
	}

	if _, ok := newModel(gaps(8, 8, 8), t0).Worth(fiveMinute(270 * time.Second)); ok {
		t.Fatal("three waits treated as enough history")
	}
	var nilModel *Model
	if _, ok := nilModel.Worth(fiveMinute(time.Minute)); ok {
		t.Fatal("nil model answered")
	}
}

func TestBucketFallsBackToState(t *testing.T) {
	ws := repeat(gaps(8, -1), 30)
	ws = append(ws, wait{Features{State: Idle, FirstTurn: true}, never})
	m := newModel(ws, t0)
	q := fiveMinute(270 * time.Second)
	q.Features.FirstTurn = true
	if worth, ok := m.Worth(q); !ok || !worth {
		t.Fatalf("thin first-turn bucket should use all idle waits: worth=%v ok=%v", worth, ok)
	}
}

func TestAdvisorBuildsAndReadsLiveFeatures(t *testing.T) {
	dir := t.TempDir()
	ws := filepath.Join(dir, "-Users-me-dev")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(ws, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("aaaaaaaa-1111.jsonl", transcript(mainStart,
		step{"user", "text", 0, 10},
		step{"assistant", "text", time.Minute, 3000},
		step{"user", "text", 20 * time.Minute, 10},
		step{"assistant", "text", 21 * time.Minute, 100},
	))
	write("bbbbbbbb-2222.jsonl", transcript(`{"type":"session_start","id":"b","callingSessionId":"p"}`,
		step{"assistant", "text", 0, 10},
	))
	a := NewAdvisor(dir)
	a.now = func() time.Time { return time.Now() }
	m := a.Build()
	// The 19 minute wait and the open last wait (never) count; the subagent
	// session is skipped.
	if m.Waits != 2 {
		t.Fatalf("waits = %d, want 2", m.Waits)
	}
	if f := a.Features("aaaaaaaa-1111", Idle); f != (Features{State: Idle}) {
		t.Fatalf("live features = %+v, want a later short reply", f)
	}
	if f := a.Features("../../etc/passwd", Idle); f != (Features{State: Idle}) {
		t.Fatalf("unsafe id features = %+v", f)
	}
	if f := a.Features("aaaaaaaa-1111", Waiting); f != (Features{State: Waiting}) {
		t.Fatalf("waiting features = %+v", f)
	}

	// A second build reuses the parsed file.
	a.files[filepath.Join(ws, "aaaaaaaa-1111.jsonl")] = fileWaits{
		mod: a.files[filepath.Join(ws, "aaaaaaaa-1111.jsonl")].mod, size: a.files[filepath.Join(ws, "aaaaaaaa-1111.jsonl")].size,
		waits: gaps(30, 30, 30),
	}
	if m := a.Build(); m.Waits != 3 {
		t.Fatalf("rebuild did not reuse the cached file: waits = %d", m.Waits)
	}
}
