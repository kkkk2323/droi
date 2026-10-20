package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/sessions"
)

// After a switch, the Sessions of the account before stay in the sidebar,
// marked as its; opening one offers to switch back, which droid needs to
// open it.
func TestAnotherAccountsSessionsStayListedAndOfferASwitch(t *testing.T) {
	h0, data := accountsTestHost(t)
	idx, _ := json.Marshal(map[string]any{"orgs": map[string][]sessions.Summary{
		"org_side": {{SessionID: "side-1", Title: "Side project plan", Cwd: "/Users/dev/side", UpdatedAt: time.Now().Unix(), OrganizationID: "org_side"}},
	}})
	if err := os.WriteFile(filepath.Join(data, "session-index.json"), idx, 0o600); err != nil {
		t.Fatal(err)
	}
	h := newHarnessWith(t, oneSession(), "", func(cfg *Config) { cfg.Host = h0 })
	h.until("both accounts' Sessions", func() bool { return h.hasText("One") && h.hasText("Side project plan") })
	if _, ok := h.tt.Find("Belongs to b@y.dev"); !ok {
		t.Fatalf("b@y.dev's Session is not marked: %q", h.tt.Texts())
	}
	h.click("Side project plan")
	h.until("the switch offer", func() bool { return h.hasText("This session belongs to b@y.dev.") })
	if h.a.Route().SessionID != "side-1" {
		t.Fatalf("route %+v", h.a.Route())
	}
	h.shoot("elsewhere-session")
	h.click("Switch to b@y.dev")
	h.until("the switch", func() bool { return h0.ActiveAccount() == "b1" })
	h.until("the wait for b@y.dev's Daemon", func() bool { return h.hasText("Switching to b@y.dev") && h.hasText("Side project plan") })
}
