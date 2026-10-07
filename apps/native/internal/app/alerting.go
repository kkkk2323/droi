package app

import (
	"github.com/kkkk2323/droi/packages/droid-sdk-go/session"

	"github.com/kkkk2323/droi/apps/native/internal/alerts"
	"github.com/kkkk2323/droi/apps/native/internal/compaction"
)

// Alert is what the window asks the desktop for when a Session finishes or
// starts waiting for an answer: a notification that opens the Session.
type Alert struct {
	SessionID string
	Title     string
	Body      string
}

// watchAlerts turns the Store's working states into alerts, as the web
// Client's useDesktopAlerts does: a Session other than the open one turns
// unread, the chosen sound plays, and a notification shows unless the
// user is looking at that Session. A `/compact` this window ran counts as
// a completion. It runs on the Store's goroutine; the window's state
// changes through Update.
func (a *App) watchAlerts() {
	tracker := alerts.NewTracker()
	store := a.ctl.Store()
	store.Subscribe(func(e session.Event) {
		switch e.Kind {
		case session.EventLoadStateChanged:
			if e.LoadState != session.Loaded {
				tracker.Forget(e.SessionID)
			}
		case session.EventWorkingStateChanged:
			s := store.Session(e.SessionID)
			if s == nil {
				return
			}
			// A subagent reports back to its calling Session, whose own alert is the one to see.
			if caller, _ := s.CallingSession(); caller != "" {
				return
			}
			if ev := tracker.Update(e.SessionID, string(s.WorkingState())); ev != "" {
				id, title := e.SessionID, s.Title()
				a.cfg.Update(func() { a.raise(id, title, ev) })
			}
		}
	})
	a.compactions.OnDone(func(f compaction.Finished) {
		id := f.SessionID
		a.cfg.Update(func() { a.raise(id, "", alerts.Completion) })
	})
}

// raise is one alert, on the main thread.
func (a *App) raise(id, title string, ev alerts.Event) {
	open := a.route.Name == "session" && a.route.SessionID == id
	if !open {
		if a.unread == nil {
			a.unread = map[string]bool{}
		}
		a.unread[id] = true
	}
	p := a.alertPrefs()
	if a.cfg.PlaySound != nil && soundGateAllows(p.FocusMode, a.focused()) {
		if sound := soundFor(p, ev); sound != "" {
			a.cfg.PlaySound(sound)
		}
	}
	if a.cfg.Notify == nil || open && a.focused() {
		return
	}
	if title == "" {
		a.mu.Lock()
		if s := findSummary(a.listed, id); s != nil {
			title = s.Title
		}
		a.mu.Unlock()
	}
	title = trimOr(title, "Untitled session")
	switch {
	case ev == alerts.Completion && p.NotifyOnComplete:
		a.cfg.Notify(Alert{SessionID: id, Title: "Droid finished", Body: title})
	case ev == alerts.AwaitingInput && p.NotifyOnWaitingForInput:
		a.cfg.Notify(Alert{SessionID: id, Title: "Droid needs input", Body: title + " — waiting for your answer"})
	}
}

// focused is whether the window has the focus; true without a way to tell.
func (a *App) focused() bool {
	if a.cfg.Focused == nil {
		return true
	}
	return a.cfg.Focused()
}

// soundGateAllows is whether a sound plays given when-to-play and focus.
func soundGateAllows(mode string, focused bool) bool {
	switch mode {
	case "focused":
		return focused
	case "unfocused":
		return !focused
	}
	return true
}

// soundFor is the sound chosen for an event: off is "", custom the file.
func soundFor(p alertPrefs, ev alerts.Event) string {
	choice, custom := p.CompletionSound, p.CustomCompletionSound
	if ev == alerts.AwaitingInput {
		choice, custom = p.AwaitingInputSound, p.CustomAwaitingInputSound
	}
	switch choice {
	case "off", "":
		return ""
	case "custom":
		if custom == nil || *custom == "" {
			return "bell"
		}
		return *custom
	}
	return choice
}

// OpenSession shows a Session, as a click on its notification does.
func (a *App) OpenSession(id string) { a.Go(Route{Name: "session", SessionID: id}) }
