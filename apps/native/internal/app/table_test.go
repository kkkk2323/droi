package app

import (
	"testing"

	"github.com/kkkk2323/droi/packages/droid-sdk-go/fakedaemon"
)

const wideTable = "## 研究证据\n\n| 来源 | 结果 | 对我们的意义 |\n|---|---|---|\n" +
	"| **Cognition 自己**（2025 年，Sonnet 4.5 那篇博客） | 模型在上下文快满时会草草收尾，写笔记不能代替真正的记忆 | 他们自己也承认笔记不够 |\n" +
	"| **VibeMemBench**（arXiv 2609.23570） | 主要收益是步数更少 | 省钱，不是更对 |\n\nAfter the table.\n"

// A table wider than the reading column wraps its cells inside it: every
// column shows, and no room is left above or below the rows.
func TestAWideTableFitsTheColumn(t *testing.T) {
	h := newHarness(t, fakedaemon.Scenario{Sessions: []fakedaemon.SessionSpec{{Title: "Table", Cwd: "/Users/dev/acme-web",
		Messages: []fakedaemon.Message{{Role: "user", Text: "hi"}, {Role: "assistant", Text: wideTable}}}}}, "")
	h.openSession("Table")
	h.until("the table", func() bool { return h.hasText("After the table.") })
	h.settle()
	list, _ := h.tt.Find("Transcript")
	right := list.X + list.W
	for _, cell := range []string{"来源", "结果", "对我们的意义", "他们自己也承认笔记不够", "省钱，不是更对"} {
		r, ok := h.tt.Find(cell)
		if !ok || r.W == 0 || r.X+r.W > right {
			t.Errorf("cell %q not inside the transcript (%v): %+v", cell, right, r)
		}
	}
	head, _ := h.tt.Find("研究证据")
	first, _ := h.tt.Find("来源")
	after, _ := h.tt.Find("After the table.")
	last, _ := h.tt.Find("省钱，不是更对")
	// The heading's margin, the table's border and a cell's padding: no more.
	if gap := first.Y - (head.Y + head.H); gap > 50 {
		t.Errorf("%v DIPs between the heading and the first row", gap)
	}
	if gap := after.Y - (last.Y + last.H); gap > 50 {
		t.Errorf("%v DIPs between the last row and the text after", gap)
	}
}
