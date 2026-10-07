package sessions

import "testing"

func TestAutomationRunsLeaveTheListAndTheRecentWorkspaces(t *testing.T) {
	run := []Tag{{Name: AutomationTag, Metadata: map[string]string{"automationId": "daily-review", "automationName": "Daily review", "type": "run"}}}
	list := []Summary{
		{SessionID: "a", Cwd: "/w/alpha", UpdatedAt: 10},
		{SessionID: "r", Cwd: "/Users/dev/.factory/automations/daily-review", UpdatedAt: 30, Tags: run},
		// Another kind of automation Session, such as one setting it up, stays.
		{SessionID: "s", Cwd: "/w/beta", UpdatedAt: 20, Tags: []Tag{{Name: AutomationTag, Metadata: map[string]string{"automationId": "x", "type": "create"}}}},
	}
	if id, name := AutomationRun(run); id != "daily-review" || name != "Daily review" {
		t.Fatalf("AutomationRun: %q %q", id, name)
	}
	eq(t, ids(WithoutAutomationRuns(list)), []string{"a", "s"})
	var paths []string
	for _, w := range RecentWorkspaces(list) {
		paths = append(paths, w.Path)
	}
	eq(t, paths, []string{"/w/beta", "/w/alpha"})
}
