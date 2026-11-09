package sessionview

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/kkkk2323/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/pkg/transcript"
)

func TestRowsCutAClusterIntoItsCalls(t *testing.T) {
	call := func(id string, nested ...*transcript.ToolCall) *transcript.ToolCall {
		return &transcript.ToolCall{Use: protocol.ToolUse{ID: id, Name: "Read"}, Nested: nested}
	}
	script := call("s", call("n1"), call("n2"))
	e := &transcript.Entry{ID: "e", Blocks: []transcript.Block{
		{Kind: transcript.Tools, ID: "c", Calls: []*transcript.ToolCall{call("a"), script}},
	}}
	v := &View{flags: map[string]*bool{}}

	kinds := func() []rowKind {
		var out []rowKind
		for _, r := range v.cutRows(&rowCache{}, []*transcript.Entry{e}, "", false) {
			out = append(out, r.kind)
		}
		return out
	}
	got := kinds()
	want := []rowKind{rowCluster, rowCall, rowCall, rowNested, rowNested}
	if len(got) != len(want) {
		t.Fatalf("rows %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("rows %v, want %v", got, want)
		}
	}

	*v.flag("cluster:c", true) = false
	if got := kinds(); len(got) != 1 || got[0] != rowCluster {
		t.Fatalf("a folded cluster shows %v", got)
	}
}

// Cut again only from where the entries changed, the rows are those a fresh
// cut makes, whatever changed: a turn appended, the last entry streaming or
// replaced, an older page put in front, the Session starting or stopping, a
// fold closed.
func TestCachedRowsAreAFreshCut(t *testing.T) {
	for seed := range uint64(20) {
		cachedRowsAgainstFreshCuts(t, seed)
	}
}

func cachedRowsAgainstFreshCuts(t *testing.T, seed uint64) {
	rnd := rand.New(rand.NewPCG(seed, 2))
	n := 0
	entry := func(user bool) *transcript.Entry {
		n++
		id := fmt.Sprintf("e%d", n)
		if user {
			return &transcript.Entry{ID: id, User: true, CreatedAt: float64(n * 1000), Blocks: []transcript.Block{{Kind: transcript.Text, ID: id + ":0", Text: "do it"}}}
		}
		var blocks []transcript.Block
		for b := range 1 + rnd.IntN(3) {
			bid := fmt.Sprintf("%s:%d", id, b)
			if rnd.IntN(2) == 0 {
				blocks = append(blocks, transcript.Block{Kind: transcript.Text, ID: bid, Text: "done"})
				continue
			}
			var calls []*transcript.ToolCall
			for c := range 1 + rnd.IntN(3) {
				calls = append(calls, &transcript.ToolCall{Use: protocol.ToolUse{ID: fmt.Sprintf("%s-%d", bid, c), Name: "Read"}})
			}
			blocks = append(blocks, transcript.Block{Kind: transcript.Tools, ID: bid, Calls: calls})
		}
		return &transcript.Entry{ID: id, CreatedAt: float64(n * 1000), Blocks: blocks}
	}
	v := &View{flags: map[string]*bool{}, calls: map[*transcript.ToolCall]*callView{}}
	var entries []*transcript.Entry
	streaming, running := "", false
	for step := range 400 {
		switch op := rnd.IntN(7); {
		case op <= 1 || len(entries) == 0:
			entries = append(slices.Clone(entries), entry(rnd.IntN(3) == 0))
		case op == 2:
			entries = append(slices.Clone(entries[:len(entries)-1]), entry(entries[len(entries)-1].User))
		case op == 3:
			entries = append([]*transcript.Entry{entry(false), entry(true)}, entries...)
		case op == 4:
			running = !running
			streaming = ""
			if running && !entries[len(entries)-1].User {
				streaming = entries[len(entries)-1].ID
			}
		case op == 5:
			for _, e := range entries {
				for _, b := range e.Blocks {
					if b.Kind == transcript.Tools && rnd.IntN(4) == 0 {
						open := v.flag("cluster:"+b.ID, true)
						*open = !*open
						v.rowsGen++
					}
				}
			}
		default:
			if len(entries) > 3 {
				entries = slices.Clone(entries[:len(entries)-1-rnd.IntN(2)])
			}
		}
		got := v.cachedRows(entries, streaming, running)
		want := v.cutRows(&rowCache{}, entries, streaming, running)
		if len(got) != len(want) {
			t.Fatalf("seed %d, step %d: %d rows, a fresh cut has %d", seed, step, len(got), len(want))
		}
		for i := range want {
			g, w := got[i], want[i]
			if g.kind != w.kind || g.key != w.key || g.e != w.e || g.block != w.block || g.call != w.call || g.top != w.top || g.bottom != w.bottom ||
				g.first != w.first || g.streaming != w.streaming || g.end != w.end || len(g.turn) != len(w.turn) || len(g.turn) > 0 && g.turn[0] != w.turn[0] {
				t.Fatalf("seed %d, step %d, row %d: %+v, a fresh cut has %+v", seed, step, i, g, w)
			}
		}
	}
}
