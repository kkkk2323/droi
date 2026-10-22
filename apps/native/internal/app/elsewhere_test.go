package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

// Opening another account's Session moves it to the account in use, with no
// switch: its transcript is stamped with that account's organization.
func TestOpeningAnotherAccountsSessionMovesItHere(t *testing.T) {
	h0, data := accountsTestHost(t)
	idx, _ := json.Marshal(map[string]any{"orgs": map[string][]sessions.Summary{
		"org_side": {{SessionID: "side-1", Title: "Side project plan", Cwd: "/Users/dev/side", UpdatedAt: time.Now().Unix(), OrganizationID: "org_side"}},
	}})
	if err := os.WriteFile(filepath.Join(data, "session-index.json"), idx, 0o600); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(h0.SharedFactoryHome(), "sessions", "-Users-dev-side")
	os.MkdirAll(dir, 0o700)
	transcript := filepath.Join(dir, "side-1.jsonl")
	os.WriteFile(transcript, []byte(`{"type":"session_start","id":"side-1","organizationId":"org_side"}`+"\n"), 0o600)
	h := newHarnessWith(t, oneSession(), "", func(cfg *Config) { cfg.Host = h0 })
	h.until("both accounts' Sessions", func() bool { return h.hasText("One") && h.hasText("Side project plan") })
	h.click("Side project plan")
	h.until("the move", func() bool {
		b, _ := os.ReadFile(transcript)
		return strings.Contains(string(b), `"organizationId":"org_acme"`)
	})
	h.until("the wait for the Daemon to list it", func() bool { return h.hasText("Opening it under a@x.dev…") })
	if h0.ActiveAccount() != "" {
		t.Fatalf("switched to %q", h0.ActiveAccount())
	}
}

// A Session that cannot be moved offers the switch to its account.
func TestASessionThatCannotMoveOffersTheSwitch(t *testing.T) {
	h0, data := accountsTestHost(t)
	idx, _ := json.Marshal(map[string]any{"orgs": map[string][]sessions.Summary{
		"org_side": {{SessionID: "side-1", Title: "Side project plan", UpdatedAt: time.Now().Unix(), OrganizationID: "org_side"}},
	}})
	os.WriteFile(filepath.Join(data, "session-index.json"), idx, 0o600)
	h := newHarnessWith(t, oneSession(), "", func(cfg *Config) { cfg.Host = h0 })
	h.until("both accounts' Sessions", func() bool { return h.hasText("Side project plan") })
	h.click("Side project plan")
	h.until("the switch offer", func() bool {
		return h.hasText("Could not move it to a@x.dev: the session's transcript is not on this computer") && h.hasText("Switch to b@y.dev")
	})
}
