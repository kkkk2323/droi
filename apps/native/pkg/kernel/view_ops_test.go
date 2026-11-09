package kernel

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"
)

// The cases are chord's own: apps/kernel/scripts/view-ops-fixture.ts edits views the way a running turn does and
// writes the ops its tracker made with the value they lead to.
func TestApplyOpsFollowsChordsTracker(t *testing.T) {
	raw, err := os.ReadFile("testdata/view_ops.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name string          `json:"name"`
		Base json.RawMessage `json:"base"`
		Ops  json.RawMessage `json:"ops"`
		Want json.RawMessage `json:"want"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			var base, want View
			if err := json.Unmarshal(c.Base, &base); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(c.Want, &want); err != nil {
				t.Fatal(err)
			}
			before := sameJSON(t, base)
			got, err := ApplyOps(base, c.Ops)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(sameJSON(t, got), sameJSON(t, want)) {
				t.Fatalf("got  %s\nwant %s", viewJSON(t, got), viewJSON(t, want))
			}
			if !reflect.DeepEqual(sameJSON(t, base), before) {
				t.Fatalf("the view the ops were applied to changed: %s", viewJSON(t, base))
			}
		})
	}
}

func TestApplyOpsTrimsUTF16CodeUnits(t *testing.T) {
	base := View{Docs: map[string]json.RawMessage{"pi.live": json.RawMessage(`{"text":"👋a世b"}`)}}
	got, err := ApplyOps(base, json.RawMessage(`[["t",["docs","pi.live","text"],3]]`))
	if err != nil {
		t.Fatal(err)
	}
	if string(got.Docs["pi.live"]) != `{"text":"世b"}` {
		t.Fatalf("got %s", got.Docs["pi.live"])
	}
}

func TestApplyOpsRefusesOpsThatDoNotFollowOn(t *testing.T) {
	base := View{
		Entries: []Entry{{ID: 1, Kind: "pi.user"}},
		Docs:    map[string]json.RawMessage{"pi.live": json.RawMessage(`{"text":"a","n":1}`)},
	}
	for name, ops := range map[string]string{
		"a splice past the entries":  `[["p",["entries"],2,0,[]]]`,
		"an append to a number":      `[["a",["docs","pi.live","n"],"x"]]`,
		"a path through nothing":     `[["s",["docs","pi.live","gone","x"],1]]`,
		"an edit of a missing doc":   `[["a",["docs","pi.usage","text"],"x"]]`,
		"an unknown op":              `[["z",["docs"]]]`,
		"an unsafe key":              `[["s",["docs","__proto__"],1]]`,
		"an index past the entries":  `[["s",["entries",5,"kind"],"x"]]`,
		"a reorder of another array": `[["m",["entries"],[1,0]]]`,
		"not ops at all":             `{"r":1}`,
	} {
		if _, err := ApplyOps(base, json.RawMessage(ops)); !errors.Is(err, ErrOps) {
			t.Errorf("%s: err = %v, want ErrOps", name, err)
		}
	}
}

func TestApplyOpsReplacesTheWholeView(t *testing.T) {
	base := View{Entries: []Entry{{ID: 1}}}
	got, err := ApplyOps(base, json.RawMessage(`[["r",{"conversation":{"id":9},"entries":[{"id":5,"kind":"pi.user"}]}],["a",["entries",0,"kind"],"!"]]`))
	if err != nil {
		t.Fatal(err)
	}
	if got.Conversation.ID != 9 || len(got.Entries) != 1 || got.Entries[0].Kind != "pi.user!" || base.Entries[0].ID != 1 {
		t.Fatalf("got %s", viewJSON(t, got))
	}
}

func sameJSON(t *testing.T, v View) any {
	t.Helper()
	tree, err := decodeTree(viewJSON(t, v))
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

func viewJSON(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
