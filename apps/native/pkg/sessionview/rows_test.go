package sessionview

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
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
				g.first != w.first || g.streaming != w.streaming || g.end != w.end || g.inSegment != w.inSegment ||
				g.segEvents != w.segEvents || g.segFailed != w.segFailed || g.segWorking != w.segWorking || g.groupKey != w.groupKey ||
				len(g.turn) != len(w.turn) || len(g.turn) > 0 && g.turn[0] != w.turn[0] {
				t.Fatalf("seed %d, step %d, row %d: %+v, a fresh cut has %+v", seed, step, i, g, w)
			}
		}
	}
}

func TestWorkstreamGroupRowsFoldAndUnfold(t *testing.T) {
	spawn := func(id, title string) *transcript.ToolCall {
		failed := false
		raw, _ := json.Marshal(`Started agent "` + title + `"` + " (" + id + ").")
		return &transcript.ToolCall{
			Use:    protocol.ToolUse{ID: id, Name: transcript.WorkstreamServer + "___spawn_agent"},
			Input:  map[string]any{"title": title, "prompt": "go"},
			Result: &protocol.ToolResult{Content: raw, IsError: &failed},
		}
	}
	e := &transcript.Entry{ID: "e1", Blocks: []transcript.Block{
		{Kind: transcript.Tools, ID: "b1", Calls: []*transcript.ToolCall{spawn("a1", "Alpha"), spawn("a2", "Beta")}},
	}}
	v := &View{flags: map[string]*bool{}}
	rows := v.cachedRows([]*transcript.Entry{e}, "", false)
	if len(rows) != 3 || rows[0].kind != rowGroup || rows[1].kind != rowCall || rows[2].kind != rowCall {
		t.Fatalf("expanded group rows = %+v", rows)
	}
	*v.flag("wsgroup:"+rows[0].groupKey, true) = false
	v.rowsGen++
	rows = v.cachedRows([]*transcript.Entry{e}, "", false)
	if len(rows) != 1 || rows[0].kind != rowGroup {
		t.Fatalf("folded group rows = %+v", rows)
	}
}

func TestFoldActivityCollapsesConsecutiveActivityAndLeavesBoundariesOutside(t *testing.T) {
	fail := true
	ok := false
	readOK := &transcript.ToolCall{Use: protocol.ToolUse{ID: "t1", Name: "Read"}, Result: &protocol.ToolResult{IsError: &ok}}
	grepFail := &transcript.ToolCall{Use: protocol.ToolUse{ID: "t2", Name: "Grep"}, Result: &protocol.ToolResult{IsError: &fail}}
	spawn := &transcript.ToolCall{Use: protocol.ToolUse{ID: "t3", Name: transcript.WorkstreamServer + "___spawn_agent"}, Input: map[string]any{"title": "Docs"}, Result: &protocol.ToolResult{IsError: &ok}}
	entries := []*transcript.Entry{
		{ID: "u1", User: true, Blocks: []transcript.Block{{Kind: transcript.Text, ID: "u1:0", Text: "go"}}},
		{ID: "a1", CreatedAt: 1000, Blocks: []transcript.Block{
			{Kind: transcript.Thinking, ID: "a1:0", Text: "hmm"},
			{Kind: transcript.Tools, ID: "a1:1", Calls: []*transcript.ToolCall{readOK, grepFail}},
			{Kind: transcript.Tools, ID: "a1:2", Calls: []*transcript.ToolCall{spawn}},
			{Kind: transcript.Thinking, ID: "a1:3", Text: "single"},
			{Kind: transcript.Text, ID: "a1:4", Text: "Final answer"},
		}},
	}
	v := &View{env: Env{FoldActivity: true}, flags: map[string]*bool{}}
	rows := v.cachedRows(entries, "", false)
	// Expected: rowUser, rowActivitySegment (3 events, 1 failed), rowCall (spawn lifted), rowBlock (single Thinking, not folded), rowBlock (Text), rowTurnEnd.
	if len(rows) != 6 {
		t.Fatalf("got %d rows, want 6: %+v", len(rows), rows)
	}
	seg := rows[1]
	if seg.kind != rowActivitySegment || seg.segEvents != 3 || seg.segFailed != 1 || seg.segWorking {
		t.Fatalf("segment = %+v", seg)
	}
	if rows[2].kind != rowCall || !rows[2].lifted {
		t.Fatalf("lifted spawn = %+v", rows[2])
	}
	if rows[3].kind != rowBlock || entries[1].Blocks[rows[3].block].Kind != transcript.Thinking {
		t.Fatalf("single thinking should not fold: %+v", rows[3])
	}
	*v.flag(seg.key, false) = true
	v.rowsGen++
	expanded := v.cachedRows(entries, "", false)
	if len(expanded) <= len(rows) || expanded[1].kind != rowActivitySegment || !expanded[2].inSegment {
		t.Fatalf("expanded rows = %+v", expanded)
	}
}

func TestCachedRowsAreAFreshCutWhenFoldingActivity(t *testing.T) {
	for seed := range uint64(20) {
		rnd := rand.New(rand.NewPCG(seed+100, 7))
		n := 0
		entry := func(user bool) *transcript.Entry {
			n++
			id := fmt.Sprintf("e%d", n)
			if user {
				return &transcript.Entry{ID: id, User: true, CreatedAt: float64(n * 1000), Blocks: []transcript.Block{{Kind: transcript.Text, ID: id + ":0", Text: "do it"}}}
			}
			var blocks []transcript.Block
			for b := range 1 + rnd.IntN(4) {
				bid := fmt.Sprintf("%s:%d", id, b)
				switch rnd.IntN(4) {
				case 0:
					blocks = append(blocks, transcript.Block{Kind: transcript.Text, ID: bid, Text: "done"})
				case 1:
					blocks = append(blocks, transcript.Block{Kind: transcript.Thinking, ID: bid, Text: "think"})
				case 2:
					var calls []*transcript.ToolCall
					for c := range 1 + rnd.IntN(3) {
						ok := rnd.IntN(3) != 0
						isErr := !ok
						calls = append(calls, &transcript.ToolCall{
							Use:    protocol.ToolUse{ID: fmt.Sprintf("%s-%d", bid, c), Name: "Read"},
							Result: &protocol.ToolResult{IsError: &isErr},
						})
					}
					if rnd.IntN(3) == 0 {
						ok := false
						calls = append(calls, &transcript.ToolCall{Use: protocol.ToolUse{ID: bid + "-mix", Name: transcript.WorkstreamServer + "___spawn_agent"}, Input: map[string]any{"title": "Mix"}, Result: &protocol.ToolResult{IsError: &ok}})
					}
					blocks = append(blocks, transcript.Block{Kind: transcript.Tools, ID: bid, Calls: calls})
				default:
					ok := false
					blocks = append(blocks, transcript.Block{Kind: transcript.Tools, ID: bid, Calls: []*transcript.ToolCall{
						{Use: protocol.ToolUse{ID: bid + "-ws1", Name: transcript.WorkstreamServer + "___spawn_agent"}, Input: map[string]any{"title": "A"}, Result: &protocol.ToolResult{IsError: &ok}},
						{Use: protocol.ToolUse{ID: bid + "-ws2", Name: transcript.WorkstreamServer + "___spawn_agent"}, Input: map[string]any{"title": "B"}, Result: &protocol.ToolResult{IsError: &ok}},
					}})
				}
			}
			return &transcript.Entry{ID: id, CreatedAt: float64(n * 1000), Blocks: blocks}
		}
		v := &View{env: Env{FoldActivity: true}, flags: map[string]*bool{}, calls: map[*transcript.ToolCall]*callView{}}
		var entries []*transcript.Entry
		streaming, running := "", false
		for step := range 300 {
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
				for k, fl := range v.flags {
					if rnd.IntN(3) == 0 && (strings.HasPrefix(k, "actseg:") || strings.HasPrefix(k, "wsgroup:") || strings.HasPrefix(k, "cluster:")) {
						*fl = !*fl
						v.rowsGen++
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
				t.Fatalf("seed %d, step %d: %d rows, fresh cut has %d", seed, step, len(got), len(want))
			}
			for i := range want {
				g, w := got[i], want[i]
				if g.kind != w.kind || g.key != w.key || g.e != w.e || g.block != w.block || g.call != w.call || g.top != w.top || g.bottom != w.bottom ||
					g.first != w.first || g.streaming != w.streaming || g.end != w.end || g.inSegment != w.inSegment ||
					g.segEvents != w.segEvents || g.segFailed != w.segFailed || g.segWorking != w.segWorking || g.groupKey != w.groupKey ||
					len(g.turn) != len(w.turn) || len(g.turn) > 0 && g.turn[0] != w.turn[0] {
					t.Fatalf("seed %d, step %d, row %d: %+v, fresh cut has %+v", seed, step, i, g, w)
				}
			}
		}
	}
}
