package app

import (
	"testing"

	"github.com/kkkk2323/droi/packages/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/internal/transcript"
)

func TestRowsCutAClusterIntoItsCalls(t *testing.T) {
	call := func(id string, nested ...*transcript.ToolCall) *transcript.ToolCall {
		return &transcript.ToolCall{Use: protocol.ToolUse{ID: id, Name: "Read"}, Nested: nested}
	}
	script := call("s", call("n1"), call("n2"))
	e := &transcript.Entry{ID: "e", Blocks: []transcript.Block{
		{Kind: transcript.Tools, ID: "c", Calls: []*transcript.ToolCall{call("a"), script}},
	}}
	v := &sessionView{flags: map[string]*bool{}}

	kinds := func() []rowKind {
		var out []rowKind
		for _, r := range v.rows([]*transcript.Entry{e}, "", nil) {
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
