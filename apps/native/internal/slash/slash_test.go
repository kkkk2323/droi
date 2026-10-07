package slash

import (
	"reflect"
	"testing"

	"github.com/kkkk2323/droi/packages/droid-sdk-go/protocol"
)

func item(name, description string, kind Kind) Item {
	return Item{Name: name, Description: description, Kind: kind}
}

func names(items []Item) []string {
	out := []string{}
	for _, i := range items {
		out = append(out, i.Name)
	}
	return out
}

func TestQuery(t *testing.T) {
	for _, c := range []struct {
		text  string
		caret int
		want  string
		ok    bool
	}{
		{"/", 1, "", true},
		{"/han", 4, "han", true},
		{"/han", 2, "h", true},
		{"/handoff ", 9, "", false},
		{"/handoff foo", 12, "", false},
		{"hello /x", 8, "", false},
		{"", 0, "", false},
	} {
		got, ok := Query(c.text, c.caret)
		if got != c.want || ok != c.ok {
			t.Errorf("Query(%q, %d) = %q, %v", c.text, c.caret, got, ok)
		}
	}
}

func TestFilter(t *testing.T) {
	items := []Item{
		item("opsx-propose", "Propose a change", Command),
		item("handoff", "Compact the conversation", Skill),
		item("review", "Review the diff for a proposal", Skill),
	}
	for query, want := range map[string][]string{
		"pro": {"opsx-propose"},
		"rev": {"review"},
		"op":  {"opsx-propose"},
		"":    {"opsx-propose", "handoff", "review"},
	} {
		if got := names(Filter(items, query, DefaultLimit)); !reflect.DeepEqual(got, want) {
			t.Errorf("Filter(%q) = %v, want %v", query, got, want)
		}
	}
	// A prefix match comes before an earlier item that only contains the query.
	if got := names(Filter([]Item{item("xpro", "", Command), item("pro", "", Command)}, "pro", 8)); !reflect.DeepEqual(got, []string{"pro", "xpro"}) {
		t.Errorf("order = %v", got)
	}
	var many []Item
	for i := range 20 {
		many = append(many, item("cmd"+string(rune('a'+i)), "", Command))
	}
	if got := len(Filter(many, "cmd", DefaultLimit)); got != 8 {
		t.Errorf("limit: %d", got)
	}
}

func TestMerge(t *testing.T) {
	merged := Merge(
		[]Item{item("compact", "builtin", Command)},
		[]Item{item("compact", "workspace", Command), item("handoff", "", Skill)},
	)
	got := [][2]string{}
	for _, i := range merged {
		got = append(got, [2]string{i.Name, i.Description})
	}
	if !reflect.DeepEqual(got, [][2]string{{"compact", "builtin"}, {"handoff", ""}}) {
		t.Errorf("merged = %v", got)
	}
	if got := names(Merge(nil, []Item{item("handoff", "", Skill)})); !reflect.DeepEqual(got, []string{"handoff"}) {
		t.Errorf("without builtins = %v", got)
	}
	if got := Merge(nil, nil); len(got) != 0 {
		t.Errorf("empty = %v", got)
	}
}

func TestPicked(t *testing.T) {
	items := []Item{item("handoff", "", Skill), item("compact", "", Command)}
	if got, rest, ok := Picked("/handoff carry on", items); !ok || got.Name != "handoff" || rest != "carry on" {
		t.Errorf("handoff: %v %q %v", got, rest, ok)
	}
	if got, rest, ok := Picked("/compact ", items); !ok || got.Name != "compact" || rest != "" {
		t.Errorf("compact: %v %q %v", got, rest, ok)
	}
	for _, text := range []string{"/handoff", "/nope carry on", "say /handoff "} {
		if _, _, ok := Picked(text, items); ok {
			t.Errorf("Picked(%q) should be nothing", text)
		}
	}
}

func TestFromDaemon(t *testing.T) {
	no, yes := false, true
	got := FromDaemon(
		[]protocol.CustomCommandInfo{{Name: "ship", Description: "Ship it", ArgumentHint: "[pr]"}, {Name: "ship"}},
		[]protocol.SkillInfo{
			{Name: "review", Description: "Review", Enabled: &yes},
			{Name: "ship", Description: "shadowed"},
			{Name: "hidden", UserInvocable: &no},
			{Name: "off", Enabled: &no},
		},
	)
	want := []Item{
		{Name: "ship", Description: "Ship it", ArgumentHint: "[pr]", Kind: Command},
		{Name: "review", Description: "Review", Kind: Skill},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v", got)
	}
}
