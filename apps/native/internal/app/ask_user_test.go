package app

import (
	"testing"

	"github.com/kkkk2323/droi/packages/droid-sdk-go/fakedaemon"
)

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
