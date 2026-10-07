package app

import (
	"testing"

	"github.com/kkkk2323/droi/packages/droid-sdk-go/fakedaemon"
)

// Until the Daemon has loaded a Session it has no commands or skills for
// it. The slash suggestions wait for the load, so the skills are there
// when "/" is typed, not a minute later.
func TestSlashSuggestsTheSkillsOfASessionOpenedFromDisk(t *testing.T) {
	h := newHarness(t, fakedaemon.Scenario{
		Sessions: []fakedaemon.SessionSpec{{Title: "One", Cwd: "/Users/dev/acme-web",
			Messages: []fakedaemon.Message{{Role: "user", Text: "hi"}}, Extra: map[string]any{"inactive": true}}},
		Input: map[string]any{"loadDelayMs": 300, "skills": []map[string]any{{"name": "wait-what", "description": "Re-pitch the last message"}}},
	}, "")
	h.openSession("One")
	h.until("the Session loaded", func() bool { return h.hasText("Context used") })
	h.click("Message")
	h.tt.Type("/")
	h.until("the skill in the suggestions", func() bool { return h.hasText("wait-what") })
}
