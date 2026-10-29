package app

import (
	"slices"
	"testing"

	"github.com/kkkk2323/droi/apps/native/pkg/gateway"
	"github.com/kkkk2323/droi/apps/native/pkg/prefs"
)

// The Gateway gives a phone the Workspace order the sidebar shows, and a pin
// made on the phone shows in the sidebar.
func TestTheSessionListShareFollowsTheSidebar(t *testing.T) {
	var share *SessionListShare
	h := newHarnessWith(t, sessionsScenario(), "", func(cfg *Config) {
		share = &SessionListShare{Prefs: cfg.Prefs}
		cfg.SessionList = share
	})
	h.until("the Session list", func() bool { return h.hasText("Fix the login race") })
	if got := share.Order().WorkspaceOrder; !slices.Equal(got, []string{"/Users/dev/acme-web", "/Users/dev/billing-service"}) {
		t.Fatalf("shown order %v", got)
	}
	if err := share.SetPinned(gateway.PinWorkspace, "/Users/dev/billing-service", true); err != nil {
		t.Fatal(err)
	}
	h.until("the pinned Workspace first", func() bool {
		got := share.Order().WorkspaceOrder
		return len(got) == 2 && got[0] == "/Users/dev/billing-service"
	})
	if !h.hasText("Pinned") {
		t.Fatalf("no Pinned section: %q", h.tt.Texts())
	}
	if got := share.Order().PinnedWorkspaces; !slices.Equal(got, prefs.PinnedWorkspaces.Get(h.a.prefs)) {
		t.Fatalf("pinned %v", got)
	}
	// The phone lists by time when the sidebar does.
	prefs.SessionView.Set(h.a.prefs, "time")
	if got := share.Order().SessionView; got != "time" {
		t.Fatalf("view %q", got)
	}
}
