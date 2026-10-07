// Package activity is what the Sessions are doing right now, for the sidebar's
// activity marks: working, or stopped until the human answers (a permission
// request or a question). A Session this Client has loaded reports through its
// own state. For the rest, the Daemon's list of open Sessions gives the state
// at connect and its working-state notifications keep it current, so a Session
// driven from the phone shows as busy on the computer too. A `/compact` this
// Client started shows as compacting from its own log (package compaction),
// since the Daemon reports no state for it (a port of the pure parts of
// use-session-activity.ts).
package activity

import "github.com/kkkk2323/droi/apps/native/internal/sessions"

type Activity string

const (
	Working    Activity = "working"
	NeedsInput Activity = "needs-input"
	Compacting Activity = "compacting"
)

// Of is the activity a working state stands for; "" when the Session is idle.
func Of(workingState string) Activity {
	switch workingState {
	case "", "idle":
		return ""
	case "waiting_for_tool_confirmation":
		return NeedsInput
	case "compacting_conversation":
		return Compacting
	}
	return Working
}

// Snapshot is every Session's activity. reported holds the working states the
// Daemon reported for Sessions, loaded here or not; loaded those of the
// Sessions this Client has loaded, which know best since their state includes
// any local step; compacting the Sessions whose `/compact` this Client is
// running.
func Snapshot(reported, loaded map[string]string, compacting map[string]bool) map[string]Activity {
	busy := map[string]Activity{}
	for id, state := range reported {
		if a := Of(state); a != "" {
			busy[id] = a
		}
	}
	for id, state := range loaded {
		if a := Of(state); a != "" {
			busy[id] = a
		} else {
			delete(busy, id)
		}
	}
	for id := range compacting {
		busy[id] = Compacting
	}
	return busy
}

// Busy is how many of some Sessions are busy, for a folded group's header.
type Busy struct {
	Working    int
	NeedsInput int
}

// CountBusy counts the Sessions working (subagents running count as working)
// and the ones waiting for an answer; false when none is doing anything.
func CountBusy(list []sessions.Summary, activity map[string]Activity, subagentsRunning map[string]int) (Busy, bool) {
	var busy Busy
	for _, s := range list {
		doing := activity[s.SessionID]
		if doing == NeedsInput {
			busy.NeedsInput++
		} else if doing != "" || subagentsRunning[s.SessionID] > 0 {
			busy.Working++
		}
	}
	return busy, busy.Working+busy.NeedsInput > 0
}
