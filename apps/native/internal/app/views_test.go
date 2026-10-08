package app

import (
	"fmt"
	"testing"

	"github.com/kkkk2323/droi/packages/droid-sdk-go/fakedaemon"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/protocol"
)

// Only the Sessions opened last keep their view, with its transcript and
// decoded pictures; one opened again builds a new view with its draft.
func TestOnlyTheLastSessionsKeepTheirView(t *testing.T) {
	var specs []fakedaemon.SessionSpec
	for i := 0; i < 5; i++ {
		specs = append(specs, fakedaemon.SessionSpec{Title: fmt.Sprintf("Task %d", i), Cwd: "/Users/dev/acme-web",
			Messages: []fakedaemon.Message{{Role: "user", Text: fmt.Sprintf("question %d", i)}}})
	}
	h := newHarness(t, fakedaemon.Scenario{Sessions: specs}, "")
	h.openSession("Task 0")
	h.until("the transcript", func() bool { return h.hasText("question 0") })
	first := h.a.Route().SessionID
	h.a.view(first).composer.text = "half a thought"
	h.a.view(first).saveDraft()
	for i := 1; i < 5; i++ {
		h.click(fmt.Sprintf("Task %d", i))
		h.until("the transcript", func() bool { return h.hasText(fmt.Sprintf("question %d", i)) })
	}
	if n := len(h.a.views); n != keptViews {
		t.Fatalf("%d views kept", n)
	}
	if h.a.views[first] != nil {
		t.Fatal("the first Session kept its view")
	}
	if h.a.ctl.Store().Session(first) != nil {
		t.Fatal("the Store kept the first Session's messages")
	}
	h.click("Task 0")
	h.until("the transcript again", func() bool { return h.hasText("question 0") })
	if got := h.a.views[first].composer.text; got != "half a thought" {
		t.Fatalf("draft %q", got)
	}
}

// The Daemon sends each skill whole; the window keeps only what it shows.
func TestSkillsShownDropTheirBodies(t *testing.T) {
	list := []protocol.SkillInfo{{Name: "review", Description: "Review a diff", Content: "a long body", Resources: []protocol.SkillResource{{}}}}
	got := skillsShown(list)
	if got[0].Name != "review" || got[0].Description != "Review a diff" || got[0].Content != "" || got[0].Resources != nil {
		t.Fatalf("%+v", got[0])
	}
	if list[0].Content == "" {
		t.Fatal("the Daemon's list changed")
	}
}
