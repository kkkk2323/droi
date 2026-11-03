package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kkkk2323/droid-sdk-go/fakedaemon"
)

// Folding the card hides its answers and keeps what was picked.
func TestAskUserCardFolds(t *testing.T) {
	h := newHarness(t, deployScenario(&fakedaemon.Turn{Kind: "askUser", Question: "Which environment?", Options: []string{"staging", "production"}}), "")
	h.openSession("Deploy")
	h.send("Ship it")
	h.until("the question", func() bool { return h.hasText("Which environment?") })
	h.click("staging")
	h.click("Hide options")
	if h.hasText("production") || h.hasButton("Answer") || h.hasButton("Other answer for: Which environment?") {
		t.Fatalf("the folded card shows its answers: %q", h.tt.Texts())
	}
	if !h.hasText("Which environment?") {
		t.Fatalf("the folded card hides its question: %q", h.tt.Texts())
	}
	h.click("Show options")
	h.click("Answer")
	h.until("the answer sent", func() bool {
		for _, v := range h.a.views {
			if len(h.a.ctl.PendingAskUsers(v.id)) > 0 {
				return false
			}
		}
		return true
	})
}

// droid 1.2 says what each option means; the card shows it under the option.
func TestAskUserCardShowsOptionDescriptions(t *testing.T) {
	h := newHarness(t, deployScenario(&fakedaemon.Turn{Kind: "askUser", Question: "Which environment?", Options: []string{"staging", "production"},
		OptionDescriptions: []string{"Only the team sees it.", "Every customer gets it."}}), "")
	h.openSession("Deploy")
	h.send("Ship it")
	h.until("the question", func() bool { return h.hasText("Which environment?") })
	if !h.hasText("Only the team sees it.") || !h.hasText("Every customer gets it.") {
		t.Fatalf("no option descriptions in %q", h.tt.Texts())
	}
	if dir := os.Getenv("DROI_NATIVE_SHOTS"); dir != "" {
		h.settle()
		_ = os.MkdirAll(dir, 0o755)
		writePNG(filepath.Join(dir, "ask-user-descriptions.png"), h.tt.Image())
	}
	h.click("production")
	h.click("Hide options")
	if h.hasText("Every customer gets it.") {
		t.Fatal("the folded card shows the descriptions")
	}
}

// A typed answer outlives the build that took it, so Answer sends it.
func TestAskUserSendsATypedAnswer(t *testing.T) {
	h := newHarness(t, deployScenario(&fakedaemon.Turn{Kind: "askUser", Question: "Which environment?", Options: []string{"staging", "production"}}), "")
	h.openSession("Deploy")
	h.send("Ship it")
	h.until("the question", func() bool { return h.hasText("Which environment?") })
	h.click("Other answer for: Which environment?")
	h.tt.Type("canary")
	h.frame()
	h.frame()
	h.click("Answer")
	h.until("the answer sent", func() bool {
		for _, v := range h.a.views {
			if len(h.a.ctl.PendingAskUsers(v.id)) > 0 {
				return false
			}
		}
		return true
	})
}
