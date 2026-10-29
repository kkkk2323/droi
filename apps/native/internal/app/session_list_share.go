package app

import (
	"slices"
	"sync"

	"github.com/kkkk2323/droi/apps/native/pkg/gateway"
	"github.com/kkkk2323/droi/apps/native/pkg/prefs"
	"github.com/kkkk2323/droi/apps/native/pkg/sessions"
)

// SessionListShare is this computer's Session list order, which the Gateway
// gives a paired phone: the pins, the Session sort and the view kept here,
// and the Workspace order the window last showed for this computer. Its methods are
// safe from any goroutine.
type SessionListShare struct {
	Prefs *prefs.Store
	// Changed, optional, redraws the window after a phone pinned.
	Changed func()

	mu    sync.Mutex
	shown []string
}

func (s *SessionListShare) Order() gateway.SessionListOrder {
	s.mu.Lock()
	shown := slices.Clone(s.shown)
	s.mu.Unlock()
	return gateway.SessionListOrder{
		PinnedSessions:   prefs.PinnedSessions.Get(s.Prefs),
		PinnedWorkspaces: prefs.PinnedWorkspaces.On("").Get(s.Prefs),
		WorkspaceOrder:   shown,
		SessionSort:      prefs.SessionSort.Get(s.Prefs),
		SessionView:      prefs.SessionView.Get(s.Prefs),
	}
}

func (s *SessionListShare) SetPinned(kind, key string, pinned bool) error {
	list := prefs.PinnedSessions
	if kind == gateway.PinWorkspace {
		list = prefs.PinnedWorkspaces.On("")
	}
	if list.Has(s.Prefs, key) != pinned {
		list.Toggle(s.Prefs, key)
	}
	if s.Changed != nil {
		s.Changed()
	}
	return nil
}

// show keeps the order of the Workspace groups the window shows for this
// computer; Recents and the pinned Sessions' group are no Workspaces.
func (s *SessionListShare) show(groups []sessions.Group) {
	keys := make([]string, 0, len(groups))
	for _, g := range groups {
		if !g.Scratch && g.Key != sessions.PinnedSessionsGroupKey {
			keys = append(keys, g.Key)
		}
	}
	s.mu.Lock()
	s.shown = keys
	s.mu.Unlock()
}
