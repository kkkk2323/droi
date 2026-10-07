// Package alerts says when a Session calls for an alert. Each Client decides
// what an alert does (a sound, a notification, a haptic); every Client marks
// the Session unread (a port of alerts.ts).
package alerts

import "sync"

type Event string

const (
	Completion    Event = "completion"
	AwaitingInput Event = "awaiting-input"
)

// Tracker follows each Session's working state and says when it calls for an
// alert, the way Factory App does: once when it starts waiting for an answer,
// and once when it goes idle after doing something.
type Tracker struct {
	mu       sync.Mutex
	active   map[string]bool
	awaiting map[string]bool
}

func NewTracker() *Tracker {
	return &Tracker{active: map[string]bool{}, awaiting: map[string]bool{}}
}

// Update takes a Session's new working state; "" is no alert.
func (t *Tracker) Update(sessionID, workingState string) Event {
	t.mu.Lock()
	defer t.mu.Unlock()
	if workingState == "idle" {
		delete(t.awaiting, sessionID)
		if t.active[sessionID] {
			delete(t.active, sessionID)
			return Completion
		}
		return ""
	}
	t.active[sessionID] = true
	if workingState != "waiting_for_tool_confirmation" || t.awaiting[sessionID] {
		return ""
	}
	t.awaiting[sessionID] = true
	return AwaitingInput
}

// Forget is for a Session that dropped out of this Client (a reconnect, a
// close); its next state is no news.
func (t *Tracker) Forget(sessionID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.active, sessionID)
	delete(t.awaiting, sessionID)
}
