package app

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/kkkk2323/droid-sdk-go/fakedaemon"
	"github.com/kkkk2323/droid-sdk-go/protocol"
)

const loopSession = "s-deploy"

func loopScenario(crons ...map[string]any) fakedaemon.Scenario {
	sc := deployScenario(&fakedaemon.Turn{Kind: "reply", Deltas: []string{"ok"}})
	sc.Sessions[0].Extra = map[string]any{"sessionId": loopSession}
	sc.Input = map[string]any{"crons": crons}
	return sc
}

// sent waits for a request of method whose params ok accepts.
func (h *harness) sent(method string, ok func(p map[string]any) bool) map[string]any {
	h.t.Helper()
	var found map[string]any
	h.until("a "+method+" request", func() bool {
		reqs, err := h.d.Requests()
		if err != nil {
			return false
		}
		for _, r := range reqs {
			var p map[string]any
			if r.Method == method && json.Unmarshal(r.Params, &p) == nil && ok(p) {
				found = p
				return true
			}
		}
		return false
	})
	return found
}

func keptAlive(want bool) func(map[string]any) bool {
	return func(p map[string]any) bool {
		v, ok := p["disableInactivityTimeout"].(bool)
		return p["sessionId"] == loopSession && ok && v == want
	}
}

func TestLoopsShowAboveTheComposerAndPause(t *testing.T) {
	next := time.Now().Add(7 * time.Minute).UTC().Format(time.RFC3339)
	h := newHarness(t, loopScenario(
		map[string]any{"id": "l1", "sessionId": loopSession, "prompt": "Check the build", "expression": "*/10 * * * *", "nextRunAt": next},
		map[string]any{"id": "l2", "sessionId": loopSession, "prompt": "Tidy the changelog\nand more", "expression": "0 */2 * * *", "status": "paused"},
	), "")
	h.openSession("Deploy")
	h.until("the loops", func() bool {
		return h.hasText("Check the build") && h.hasText("Every 10 minutes · Next at ") && h.hasText("Tidy the changelog…") && h.hasText("Every 2 hours · Paused")
	})
	h.shoot("loops")

	h.click("Resume loop")
	h.sent(protocol.MethodUpdateCron, func(p map[string]any) bool { return p["cronId"] == "l2" && p["status"] == "active" })
	h.until("the loop running again", func() bool { return !h.hasText("Paused") })
	h.click("Pause loop")
	h.sent(protocol.MethodUpdateCron, func(p map[string]any) bool { return p["status"] == "paused" })
	h.until("a loop paused", func() bool { return h.hasText("· Paused") })
}

// A Session with a loop to run is loaded at start, without the Daemon's
// inactivity timeout, which also resumes its held loops; once no loop is
// left to run it is loaded with it again.
func TestASessionWithLoopsKeepsItsWorker(t *testing.T) {
	h := newHarness(t, loopScenario(
		map[string]any{"id": "l1", "sessionId": loopSession, "prompt": "Check the build", "expression": "*/10 * * * *", "status": "held"},
	), "")
	h.sent(protocol.MethodLoadSession, keptAlive(true))
	h.openSession("Deploy")
	h.until("the held loop", func() bool { return h.hasText("Waiting for the session") })
	h.click("Delete loop")
	h.sent(protocol.MethodDeleteCron, func(p map[string]any) bool { return p["cronId"] == "l1" && p["sessionId"] == loopSession })
	h.sent(protocol.MethodLoadSession, keptAlive(false))
	h.until("no loop", func() bool { return !h.hasText("Check the build") })
}

func TestLoopCommandStartsALoop(t *testing.T) {
	h := newHarness(t, loopScenario(), "")
	h.openSession("Deploy")
	h.send("/loop 10m check the build")
	p := h.sent(protocol.MethodCreateCron, func(map[string]any) bool { return true })
	schedule, _ := p["schedule"].(map[string]any)
	scope, _ := p["scope"].(map[string]any)
	payload, _ := p["payload"].(map[string]any)
	expr, _ := schedule["expression"].(string)
	if p["kind"] != "session_prompt" || p["source"] != "loop_command" || p["runImmediately"] != true ||
		scope["sessionId"] != loopSession || scope["sessionCwd"] != "/Users/dev/acme-web" ||
		payload["prompt"] != "check the build" || !strings.HasSuffix(expr, " * * * *") || !strings.Contains(expr, ",") && !strings.HasPrefix(expr, "*/10") {
		t.Fatalf("create_cron %v", p)
	}
	h.until("the new loop", func() bool { return h.hasText("check the build") && h.hasText("Every 10 minutes") })
	h.sent(protocol.MethodLoadSession, keptAlive(true))
	if h.count(protocol.MethodAddUserMessage) != 0 {
		t.Error("the command went to the agent")
	}

	h.send("/loop stop")
	h.sent(protocol.MethodDeleteCron, func(map[string]any) bool { return true })
	h.until("the loop gone", func() bool { return !h.hasText("Every 10 minutes") })
}

func TestALoopCommandWithoutAnIntervalStaysInTheComposer(t *testing.T) {
	h := newHarness(t, loopScenario(), "")
	h.openSession("Deploy")
	h.send("/loop 7m tidy")
	h.until("the interval error", func() bool { return h.hasText("A loop runs every 1, 2, 3") })
	for _, v := range h.a.views {
		if v.composer.text != "/loop 7m tidy" {
			t.Errorf("composer %q", v.composer.text)
		}
	}
	if h.count(protocol.MethodCreateCron)+h.count(protocol.MethodAddUserMessage) != 0 {
		t.Error("the command was sent")
	}
}
