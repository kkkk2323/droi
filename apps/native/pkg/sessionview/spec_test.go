package sessionview

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/kkkk2323/droid-sdk-go/fakedaemon"
	"github.com/kkkk2323/droid-sdk-go/protocol"
)

func specScenario() fakedaemon.Scenario {
	return fakedaemon.Scenario{
		Sessions: []fakedaemon.SessionSpec{{
			Title: "Greeting", Cwd: "/Users/dev/acme-web",
			Messages: []fakedaemon.Message{{Role: "user", Text: "hi"}},
			Extra:    map[string]any{"settings": map[string]any{"interactionMode": "spec"}},
		}},
		Turn: &fakedaemon.Turn{Kind: "spec", Plan: "1. Create `hello.txt`\n2. Read it back", Title: "Add hello.txt", Reply: "Created hello.txt."},
	}
}

// comment types a comment into the spec review.
func (h *harness) comment(text string) {
	h.t.Helper()
	if err := h.tt.Click("Comment for Droid"); err != nil {
		h.t.Fatal(err)
	}
	h.frame()
	h.tt.Type(text)
	h.frame()
	h.frame()
}

// Spec mode's plan shows in the transcript, and its review stands in for the
// composer; Proceed approves it with the comment typed, and the work goes on
// in this Session, out of Spec mode.
func TestASpecIsApprovedWithAComment(t *testing.T) {
	h := newHarness(t, specScenario())
	h.send("Plan hello.txt")
	h.until("the spec review", func() bool { return h.hasText("Review the spec") || h.hasText("Approve the plan above") })
	for _, want := range []string{"Add hello.txt", "Read it back", "Waiting for your review"} {
		if !h.hasText(want) {
			t.Errorf("no %q in %q", want, h.tt.Texts())
		}
	}
	if _, ok := h.tt.Find("Message"); ok {
		t.Error("the composer shows beside the spec review")
	}
	h.comment("Keep it short")
	if err := h.tt.Click("Proceed"); err != nil {
		t.Fatal(err)
	}
	h.until("the work after the approval", func() bool { return h.hasText("Created hello.txt.") })
	h.until("the plan approved", func() bool { return h.hasText("Approved") && h.hasText("Keep it short") })
	if mode := h.ctl.Store().Session(h.v.ID()).Settings().InteractionMode; mode == protocol.DroidInteractionModeSpec {
		t.Errorf("still in Spec mode after the approval")
	}
	h.until("the composer back", func() bool { _, ok := h.tt.Find("Message"); return ok })
}

// The Daemon drops the comment of a plan sent back, so Keep planning sends
// it as the next message.
func TestKeepPlanningSendsTheCommentAsTheNextMessage(t *testing.T) {
	h := newHarness(t, specScenario())
	h.send("Plan hello.txt")
	h.until("the spec review", func() bool { return h.hasText("Approve the plan above") })
	h.comment("Also add a test")
	if err := h.tt.Click("Keep planning"); err != nil {
		t.Fatal(err)
	}
	h.until("the comment sent", func() bool {
		reqs, _ := h.d.Requests()
		for _, r := range reqs {
			var p struct {
				Text string `json:"text"`
			}
			if r.Method == "daemon.add_user_message" && json.Unmarshal(r.Params, &p) == nil && p.Text == "Also add a test" {
				return true
			}
		}
		return false
	})
	h.until("the plan sent back", func() bool { return h.hasText("Sent back to plan further") })
	if mode := h.ctl.Store().Session(h.v.ID()).Settings().InteractionMode; mode != protocol.DroidInteractionModeSpec {
		t.Errorf("left Spec mode: %q", mode)
	}
}

// With the new-session switch on, Proceed hands the plan to a new Session,
// which the Controller starts with the plan.
func TestProceedingInANewSessionHandsThePlanOver(t *testing.T) {
	h := newHarness(t, specScenario())
	h.send("Plan hello.txt")
	h.until("the spec review", func() bool { return h.hasText("Approve the plan above") })
	if err := h.tt.Click("Start in a new session"); err != nil {
		t.Fatal(err)
	}
	h.frame()
	if err := h.tt.Click("Proceed in a new session"); err != nil {
		t.Fatal(err)
	}
	req, err := h.d.WaitForRequest("daemon.add_user_message", 2)
	if err != nil {
		t.Fatal(err)
	}
	var p struct {
		SessionID string `json:"sessionId"`
		Text      string `json:"text"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil || p.SessionID == h.v.ID() || !strings.Contains(p.Text, "Read it back") {
		t.Fatalf("sent %s (%v)", req.Params, err)
	}
	h.until("the plan approved for a new Session", func() bool { return h.hasText("Approved, carried out in a new session") })
}
