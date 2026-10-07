package md

import "testing"

func TestParseTreeNestsLists(t *testing.T) {
	nodes := ParseTree("Intro\n\n- one `x`\n- two\n  - deep\n\n1. first\n2. second\n\n```ts\nlet a = 1\n```\n\n- [x] done\n")
	kinds := []NodeKind{}
	for _, n := range nodes {
		kinds = append(kinds, n.Kind)
	}
	want := []NodeKind{NodeParagraph, NodeList, NodeList, NodeCode, NodeList}
	if len(kinds) != len(want) {
		t.Fatalf("kinds %v", kinds)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("kinds %v", kinds)
		}
	}
	ul := nodes[1]
	if ul.Ordered || !ul.Tight || len(ul.Children) != 2 {
		t.Fatalf("ul %+v", ul)
	}
	if got := PlainText(ul.Children[0].Children[0].Inlines); got != "one x" {
		t.Fatalf("first item %q", got)
	}
	if second := ul.Children[1]; len(second.Children) != 2 || second.Children[1].Kind != NodeList {
		t.Fatalf("nested list %+v", second)
	}
	if ol := nodes[2]; !ol.Ordered || ol.Start != 1 {
		t.Fatalf("ol %+v", ol)
	}
	if code := nodes[3]; code.Lang != "ts" || code.Code != "let a = 1" {
		t.Fatalf("code %+v", code)
	}
	task := nodes[4].Children[0]
	if task.Checked != 2 || PlainText(task.Children[0].Inlines) != "done" {
		t.Fatalf("task %+v %q", task, PlainText(task.Children[0].Inlines))
	}
}
