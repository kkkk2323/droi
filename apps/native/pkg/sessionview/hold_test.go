package sessionview

import (
	"fmt"
	"strings"
	"testing"

	"github.com/kkkk2323/droid-sdk-go/fakedaemon"
	"github.com/kkkk2323/droid-sdk-go/protocol"
)

// A fold opened in the last reply grows below the line clicked, which stays
// under the pointer; once the user scrolls back to the end, the transcript
// follows what comes again.
func TestAFoldOpenedAtTheEndKeepsItsPlace(t *testing.T) {
	var msgs []fakedaemon.Message
	for i := range 20 {
		msgs = append(msgs, fakedaemon.Message{Role: "user", Text: fmt.Sprintf("Question %d", i)}, fakedaemon.Message{Role: "assistant", Text: fmt.Sprintf("Answer %d", i)})
	}
	reasoning := strings.Repeat("A line of reasoning about the next step.\n\n", 40)
	msgs = append(msgs, fakedaemon.Message{Role: "user", Text: "Last question"}, fakedaemon.Message{Role: "assistant", Content: []map[string]any{
		{"type": "thinking", "thinking": reasoning, "signature": "sig", "durationMs": 3000},
		{"type": "text", "text": "Final answer"},
	}})
	h := newHarness(t, fakedaemon.Scenario{Sessions: []fakedaemon.SessionSpec{{Title: "Folds", Cwd: "/Users/dev/acme-web", Messages: msgs}}})
	h.until("the last reply", func() bool { return h.hasText("Final answer") })
	for range 5 {
		h.frame()
	}
	before, ok := h.tt.Find("Reasoned for 3s")
	if !ok {
		t.Fatalf("no fold in %q", h.tt.Texts())
	}
	if err := h.tt.Click("Reasoned for 3s"); err != nil {
		t.Fatal(err)
	}
	h.until("the reasoning", func() bool { return h.hasText("A line of reasoning") })
	for range 10 {
		h.frame()
	}
	after, _ := h.tt.Find("Reasoned for 3s")
	if d := after.Y - before.Y; d > 1 || d < -1 {
		t.Fatalf("the fold moved from %v to %v as it opened", before.Y, after.Y)
	}

	h.tt.Scroll(450, 320, 0, 100000)
	h.until("the end", func() bool { h.tt.Scroll(450, 320, 0, 100000); return h.v.list.AtEnd() })
	id := h.v.ID()
	var parent string
	h.ctl.Store().Session(id).ReadMessages(func(ms []protocol.FactoryDroidMessage) { parent = ms[len(ms)-1].ID })
	if err := h.d.Notify(id, map[string]any{"type": "create_message", "message": map[string]any{
		"id": "later", "parentId": parent, "role": "assistant", "createdAt": 1, "updatedAt": 1, "visibility": "both",
		"content": []any{map[string]any{"type": "text", "text": "A later reply"}},
	}}); err != nil {
		t.Fatal(err)
	}
	h.until("the later reply in view", func() bool {
		r, ok := h.tt.Find("A later reply")
		return ok && r.Y >= 0 && r.Y+r.H <= 640
	})
}

// A reply's GitHub alert shows as a box under its title, not as its marker.
func TestAReplysAlertShowsAsABox(t *testing.T) {
	h := newHarness(t, wideTableScenario("Before.\n\n> [!WARNING]\n> The build deletes the cache.\n\nAfter."))
	h.until("the reply", func() bool { return h.hasText("After.") })
	if h.hasText("[!WARNING]") || !h.hasText("Warning") || !h.hasText("The build deletes the cache.") {
		t.Fatalf("the alert shows as %q", h.tt.Texts())
	}
}

// A table whose cells name people and issues, as GitHub writes them, is
// measured with them: one wider than the reading column grows past it.
func TestATableOfMentionsIsMeasuredWithThem(t *testing.T) {
	who := strings.Repeat("@a-rather-long-github-username ", 4)
	issue := strings.Repeat("some-organization/some-repository#12345 ", 3)
	reply := "Prose above the table.\n\n| Who | Issue | Also |\n| --- | --- | --- |\n| " + who + "| " + issue + "| " + issue + "|\n\nProse under the table."
	h := newHarness(t, wideTableScenario(reply))
	h.tt.SetSize(1400, 900)
	h.until("the reply", func() bool { return h.hasText("Prose under the table.") })
	h.until("the table past the column", func() bool {
		prose, _ := h.box("Prose above the table.")
		col, _ := h.box("Who")
		return col < prose-40
	})
}
