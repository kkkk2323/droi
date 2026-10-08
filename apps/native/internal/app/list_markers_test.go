package app

import (
	"testing"

	"github.com/kkkk2323/droi/packages/droid-sdk-go/fakedaemon"
)

const numberedLists = "Tight:\n\n1. First tight item\n2. Second tight item\n\nLoose:\n\n5. First loose item\n\n6. Second loose item\n\n   More of the second.\n"

// An item's number sits on its first line, in a tight list and in a loose
// one, where the item's first paragraph keeps its top margin.
func TestListNumbersSitOnTheirItemsFirstLine(t *testing.T) {
	h := newHarness(t, fakedaemon.Scenario{Sessions: []fakedaemon.SessionSpec{{Title: "Lists", Cwd: "/Users/dev/acme-web",
		Messages: []fakedaemon.Message{{Role: "user", Text: "hi"}, {Role: "assistant", Text: numberedLists}}}}}, "")
	h.openSession("Lists")
	h.until("the lists", func() bool { return h.hasText("More of the second.") })
	h.settle()
	for _, item := range []struct{ number, text string }{
		{"1.", "First tight item"}, {"2.", "Second tight item"}, {"5.", "First loose item"}, {"6.", "Second loose item"},
	} {
		text, ok := h.tt.Find(item.text)
		number, found := h.tt.Find(item.number)
		if !ok || !found {
			t.Fatalf("no %q or %q: %q", item.number, item.text, h.tt.Texts())
		}
		if number.Y != text.Y {
			t.Errorf("%s %q: the number is at y=%v, its first line at y=%v", item.number, item.text, number.Y, text.Y)
		}
	}
}
