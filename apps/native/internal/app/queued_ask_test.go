package app

import (
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/kkkk2323/droid-sdk-go/fakedaemon"
)

func queuedAskScenario() fakedaemon.Scenario {
	return deployScenario(&fakedaemon.Turn{Kind: "queuedAskUser", Question: "Which region?", Options: []string{"eu-west", "us-east"},
		Context: "The region decides the CDN; the build goes on meanwhile.", Reply: "Building meanwhile."})
}

// alertsOf records the window's sounds.
func alertsOf(h *harness) func() []string {
	var mu sync.Mutex
	var sounds []string
	h.a.cfg.PlaySound = func(s string) { mu.Lock(); sounds = append(sounds, s); mu.Unlock() }
	h.a.cfg.Focused = func() bool { return true }
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(sounds)
	}
}

// A queued question sits above the composer, which still takes text; the
// turn ends without waiting for it, and the Session then needs input. Answering it hides the receipt and shows the record as one
// card, and the agent's next turn finishes as usual.
func TestAQueuedQuestionLeavesTheComposer(t *testing.T) {
	h := newHarness(t, queuedAskScenario(), "")
	sounds := alertsOf(h)
	h.openSession("Deploy")
	h.send("Ship it")
	h.until("the queued question and the turn's end", func() bool {
		return h.hasText("Which region?") && h.hasText("Building meanwhile.") && !h.hasButton("Cancel")
	})
	for _, want := range []string{"The region decides the CDN; the build goes on meanwhile.", "Not blocking", "waiting for your answer"} {
		if !h.hasText(want) {
			t.Errorf("no %q: %q", want, h.tt.Texts())
		}
	}
	if !h.hasButton("Dismiss") || h.hasButton("Cancel") {
		t.Error("the queued card's button is not Dismiss")
	}
	h.click("Message")
	h.typeInto("and tag it")
	if got := h.sessionText(); got != "and tag it" {
		t.Fatalf("the composer does not take text beside the card: %q", got)
	}
	h.until("the Session to need input", func() bool { _, ok := h.tt.Find("Needs input"); return ok })
	if got := sounds(); !slices.Equal(got, []string{"fx-ack01"}) {
		t.Errorf("sounds at the turn's end %q, want the needs-input sound alone", got)
	}

	h.click("eu-west")
	h.click("Answer")
	h.until("the record and the next turn", func() bool {
		return h.hasText("Answered queued questions") && h.hasText("Noted: eu-west.")
	})
	v := h.a.views[h.a.Route().SessionID]
	if n := len(h.a.ctl.PendingAskUsers(v.id)); n != 0 {
		t.Fatalf("%d questions still pending", n)
	}
	h.until("the turn's end", func() bool { return slices.Contains(sounds(), "fx-ok01") })
	for _, s := range h.tt.Texts() {
		if strings.Contains(s, "(AskUser call") {
			t.Errorf("the record's own text shows: %q", s)
		}
	}
	if h.hasText("waiting for your answer") || !h.hasText("answered") {
		t.Errorf("the AskUser row does not say it was answered: %q", h.tt.Texts())
	}
	if !h.hasText("eu-west") || h.hasText("Not blocking") {
		t.Errorf("the record or the card: %q", h.tt.Texts())
	}
	if got := h.sessionText(); got != "and tag it" {
		t.Errorf("the composer's text after answering: %q", got)
	}
}

// Dismissing a queued question tells the agent, which the record shows.
func TestAQueuedQuestionCanBeDismissed(t *testing.T) {
	h := newHarness(t, queuedAskScenario(), "")
	h.openSession("Deploy")
	h.send("Ship it")
	h.until("the queued question", func() bool { return h.hasText("Which region?") && h.hasButton("Dismiss") })
	h.click("Dismiss")
	h.until("the record of the dismissal", func() bool {
		return h.hasText("Dismissed queued questions") && h.hasText("Understood, I will decide myself.")
	})
	if !h.hasText("dismissed") || h.hasButton("Dismiss") {
		t.Errorf("after the dismissal: %q", h.tt.Texts())
	}
}
