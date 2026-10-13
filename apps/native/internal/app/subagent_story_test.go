package app

import (
	"strings"
	"testing"

	"github.com/kkkk2323/droi/packages/droid-sdk-go/fakedaemon"
)

const (
	bgCaller = "11111111-1111-4111-8111-111111111111"
	bgChild  = "66666666-6666-4666-8666-666666666666"
)

// backgroundScenario is a Session that launched a background Task, as the
// real Daemon (droid 0.233) reports it: the Task answers at once with the
// subagent's id, and child is how the list has the subagent meanwhile.
func backgroundScenario(child map[string]any) fakedaemon.Scenario {
	return fakedaemon.Scenario{Sessions: []fakedaemon.SessionSpec{
		{Title: "Fix the login race", Cwd: "/Users/dev/acme-web", Extra: map[string]any{"sessionId": bgCaller},
			Messages: []fakedaemon.Message{
				{Role: "user", Text: "Translate the strings"},
				{Role: "assistant", Content: []map[string]any{{"type": "tool_use", "id": "toolu_bg", "name": "Task",
					"input": map[string]any{"subagent_type": "worker", "description": "Translate", "prompt": "Translate the strings"}}}},
				{Role: "tool", Content: []map[string]any{{"type": "tool_result", "toolUseId": "toolu_bg",
					"content": "Task launched in background\nsession_id: " + bgChild}}},
			}},
		{Title: "Worker: Translate", Cwd: "/Users/dev/acme-web", Messages: []fakedaemon.Message{{Role: "user", Text: "Translate the strings"}},
			Extra: child},
	}}
}

// sameStory checks that the Task card, the header's subagent menu and the
// sidebar tell one story about the subagent, and that it is no Session of
// its own in the sidebar.
func (h *harness) sameStory(running bool) {
	h.t.Helper()
	menu := "1 subagent"
	card, other := "Completed", "Running"
	if running {
		menu = "1 subagent, 1 running"
		card, other = "Running", "Completed"
	}
	h.until("the card, the menu and the sidebar agree (running "+map[bool]string{true: "yes", false: "no"}[running]+")", func() bool {
		_, opened := h.tt.Find("Open subagent session")
		_, counted := h.tt.Find(menu)
		sidebar := h.hasText("1 subagent running")
		return opened && counted && h.hasText(card) && !h.hasText(other) && sidebar == running
	})
	// The sidebar's texts come first, up to its Settings row.
	for _, s := range h.tt.Texts() {
		if s == "Settings" {
			break
		}
		if strings.Contains(s, "Worker: Translate") {
			h.t.Fatalf("the subagent shows as a Session of its own: %q", h.tt.Texts())
		}
	}
}

func (h *harness) announceChild() {
	h.t.Helper()
	if err := h.d.Notify(bgCaller, map[string]any{"type": "child_session_available", "childSessionId": bgChild, "toolUseId": "toolu_bg",
		"subagentType": "worker", "description": "Translate", "timestamp": 1}); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) finishChild() {
	h.t.Helper()
	if err := h.d.Notify(bgChild, map[string]any{"type": "agent_turn_completed", "reason": "completed", "tokenUsage": map[string]any{
		"inputTokens": 0, "outputTokens": 0, "cacheCreationTokens": 0, "cacheReadTokens": 0, "thinkingTokens": 0, "factoryCredits": 0,
	}}); err != nil {
		h.t.Fatal(err)
	}
}

// The Daemon announces a background subagent before its Session is on
// disk, so the list has no row for it until the run ends; every view tells
// it running, and done once it ends.
func TestABackgroundSubagentNotListedYetTellsOneStory(t *testing.T) {
	h := newHarness(t, backgroundScenario(map[string]any{"sessionId": bgChild, "unwritten": true}), "")
	h.openSession("Fix the login race")
	h.until("the Task card", func() bool { return h.hasText("Translate the strings") })
	h.announceChild()
	h.sameStory(true)
	h.finishChild()
	h.sameStory(false)
}

// Once on disk the Daemon may list the subagent before it says whose it is;
// it is still the caller's subagent, not a Session in the sidebar.
func TestASubagentListedWithoutItsCallerTellsOneStory(t *testing.T) {
	h := newHarness(t, backgroundScenario(map[string]any{"sessionId": bgChild}), "")
	h.openSession("Fix the login race")
	h.until("the Task card", func() bool { return h.hasText("Translate the strings") })
	h.announceChild()
	h.sameStory(true)
	h.finishChild()
	h.sameStory(false)
	// A resumed run is running again everywhere, and ends again.
	h.announceChild()
	h.sameStory(true)
	h.finishChild()
	h.sameStory(false)
}
