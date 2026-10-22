package app

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kkkk2323/droid-sdk-go/fakedaemon"

	"github.com/kkkk2323/droi/apps/native/pkg/host"
)

func testJWT(claims map[string]any) string {
	b, _ := json.Marshal(claims)
	return "h." + base64.RawURLEncoding.EncodeToString(b) + ".s"
}

// accountsTestHost is a Host whose droid CLI is signed in as a@x.dev and
// that has b@y.dev added, laid out as Droi keeps them. Its Factory API is
// a stand-in that has a@x.dev near its 5-hour limit, on a plan that ends in
// three days, and b@y.dev on one that renews, drawing on Core too.
func accountsTestHost(t *testing.T) (*host.Host, string) {
	t.Helper()
	home, data := t.TempDir(), t.TempDir()
	aToken := testJWT(map[string]any{"sub": "ua", "email": "a@x.dev", "org_id": "org_acme", "external_org_id": "org_acme"})
	if err := host.WriteCliLogin(filepath.Join(home, ".factory"), aToken, "r"); err != nil {
		t.Fatal(err)
	}
	if err := host.WriteCliLogin(filepath.Join(data, "accounts", "b1", ".factory"), testJWT(map[string]any{"sub": "ub", "email": "b@y.dev", "org_id": "org_side", "external_org_id": "org_side"}), "r"); err != nil {
		t.Fatal(err)
	}
	ends := time.Now().Add(3 * time.Hour).UTC().Format(time.RFC3339)
	planEnds := time.Now().Add(71 * time.Hour).UTC().Format(time.RFC3339)
	factory := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/organization" {
			end := "null"
			if r.Header.Get("Authorization") == "Bearer "+aToken {
				end = strconv.Quote(planEnds)
			}
			fmt.Fprintf(w, `{"organization":{"subscription":{"orbSubscription":{"name":"Factory Pro Plan","status":"active","end_date":%s,"current_billing_period_end_date":"2099-01-30T08:00:00+00:00","trial_info":{"end_date":null}}}}}`, end)
			return
		}
		five, week := 22, 9
		core := fmt.Sprintf(`{"fiveHour":{"usedPercent":9,"windowEnd":%q}}`, ends)
		if r.Header.Get("Authorization") == "Bearer "+aToken {
			five, week, core = 92, 41, "{}"
		}
		fmt.Fprintf(w, `{"limits":{"standard":{"fiveHour":{"usedPercent":%d,"windowEnd":%q},"weekly":{"usedPercent":%d,"windowEnd":%q},"monthly":{"usedPercent":17,"windowEnd":null}},"core":%s}}`, five, ends, week, ends, core)
	}))
	t.Cleanup(factory.Close)
	os.WriteFile(filepath.Join(data, "settings.json"), []byte(`{"accounts":["b1"],"factoryApiBaseUrl":"`+factory.URL+`"}`), 0o600)
	h := host.New(host.Config{UserData: data, Home: home, Env: func(string) string { return "" }})
	t.Cleanup(h.Stop)
	return h, data
}

func TestTheAccountTabSwitchesAndRemovesAccounts(t *testing.T) {
	h0, data := accountsTestHost(t)
	h := newHarnessWith(t, oneSession(), "", func(cfg *Config) { cfg.Host = h0 })
	h.until("the Session list", func() bool { return h.hasText("One") })
	h.click("Settings")
	h.until("the accounts", func() bool { return h.hasText("b@y.dev") && h.hasText("In use") })
	if !h.hasText("Shared with droid in the terminal") || h.hasText("org_acme") || h.hasText("droid CLI ·") {
		t.Fatalf("the CLI's account is not marked as shared, or names its organization: %q", h.tt.Texts())
	}
	// Each account shows a ring of what is left per window, and the one in
	// use, near its limit, says so.
	h.until("the usage", func() bool {
		_, a := h.tt.Find("5h: 8% left")
		_, b := h.tt.Find("5h: 78% left")
		return a && b
	})
	if !h.hasText("This account is close to its limit.") {
		t.Fatalf("no hint to switch: %q", h.tt.Texts())
	}
	// Core shows, with its three windows, only for the account that draws on it.
	texts := strings.Join(h.tt.Texts(), "\n")
	if _, ok := h.tt.Find("Core 5h: 91% left"); !ok || strings.Count(texts, "Core 5h: ") != 1 || strings.Count(texts, "Core Month: 100% left") != 1 {
		t.Fatalf("Core is not shown for b@y.dev alone: %q", h.tt.Texts())
	}
	// The plan is a badge; only the one that ends soon says when.
	ending := "Ends " + time.Now().Add(71*time.Hour).Local().Format("Jan 2, 2006") + " · 3 days left"
	h.until("the plans", func() bool { _, ok := h.tt.Find(ending); return ok && h.hasText("Pro") })
	if h.hasText("Renews") || h.hasText("Factory Pro Plan") {
		t.Fatalf("a renewal date: %q", h.tt.Texts())
	}
	// When each window resets is in its details. Opened once the plans
	// moved the rows down, so the click lands on the button.
	if h.hasText("Resets in") {
		t.Fatalf("the details show before they are asked for: %q", h.tt.Texts())
	}
	h.click("Usage details for a@x.dev")
	h.until("a@x.dev's details", func() bool { return h.hasText("Resets in 2h ") && h.hasText("8% left") && h.hasText("Not started") })
	if slices.Contains(h.tt.Texts(), "78% left") {
		t.Fatalf("b@y.dev's details opened too: %q", h.tt.Texts())
	}
	h.shoot("settings-account")

	h.click("Switch to b@y.dev")
	h.until("the switch", func() bool { return h0.ActiveAccount() == "b1" })
	h.until("the CLI's account to switch back to", func() bool { _, ok := h.tt.Find("Switch to a@x.dev"); return ok })
	if s := h0.LoginState(); s.Account == nil || *s.Account.Email != "b@y.dev" {
		t.Fatalf("%+v", s)
	}
	h.shoot("settings-account-switched")

	h.click("Switch to a@x.dev")
	h.until("the switch back", func() bool { return h0.ActiveAccount() == "" })
	h.click("Remove b@y.dev")
	h.until("the question", func() bool { return h.hasText("Droi deletes this account's login") })
	h.shoot("settings-account-remove")
	h.click("Remove")
	h.until("the account gone", func() bool { return !h.hasText("b@y.dev") })
	if _, err := os.Stat(filepath.Join(data, "accounts", "b1")); !os.IsNotExist(err) {
		t.Fatal("the account's folder stayed")
	}
}

func TestSubscriptionText(t *testing.T) {
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.Local)
	renews := time.Date(2027, 1, 30, 8, 0, 0, 0, time.Local)
	for _, c := range []struct {
		sub   host.Subscription
		want  string
		level planLevel
	}{
		{host.Subscription{Plan: "Pro", Status: "active", Renews: renews}, "", planFine},
		{host.Subscription{Plan: "Pro", Status: "active", Ends: renews}, "", planFine},
		{host.Subscription{Plan: "Pro", Status: "active", Ends: now.Add(20 * time.Hour)}, "Ends Oct 21, 2026 · 1 day left", planEnding},
		{host.Subscription{Status: "active", TrialEnds: now.Add(5 * 24 * time.Hour), Renews: renews}, "Trial ends Oct 25, 2026 · 5 days left", planEnding},
		{host.Subscription{Status: "active", TrialEnds: now.Add(20 * 24 * time.Hour), Renews: renews}, "", planFine},
		{host.Subscription{Plan: "Pro", Status: "ended", Ends: now.Add(-time.Hour)}, "Ended Oct 20, 2026", planEnded},
		{host.Subscription{Plan: "Pro", Status: "ended"}, "Ended", planEnded},
	} {
		line, rest, level := subscriptionText(&c.sub, now)
		if got := strings.TrimSuffix(line+" · "+rest, " · "); got != c.want || level != c.level {
			t.Errorf("%+v: %q %d, want %q %d", c.sub, got, level, c.want, c.level)
		}
	}
}

func TestResetText(t *testing.T) {
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.Local)
	for d, want := range map[time.Duration]string{
		30 * time.Second:             "Resets soon",
		42 * time.Minute:             "Resets in 42m",
		2*time.Hour + 15*time.Minute: "Resets in 2h 15m",
		30 * time.Hour:               "Resets in 1 day (Oct 21)",
		4*24*time.Hour + 6*time.Hour: "Resets in 4 days (Oct 24)",
	} {
		if got := resetText(d, now); got != want {
			t.Errorf("%v: %q, want %q", d, got, want)
		}
	}
}

func TestPlanName(t *testing.T) {
	for plan, want := range map[string]string{"Factory Pro Plan": "Pro", "Max": "Max", "Factory Plan": "Factory Plan", "": ""} {
		if got := planName(&host.Subscription{Plan: plan}); got != want {
			t.Errorf("%q: %q, want %q", plan, got, want)
		}
	}
}

// A switch while a Session is waiting asks first: the restart would stop it.
func TestSwitchingAccountsAsksWhileASessionWorks(t *testing.T) {
	h0, _ := accountsTestHost(t)
	h := newHarnessWith(t, deployScenario(&fakedaemon.Turn{Kind: "askUser", Question: "Which environment?", Options: []string{"staging", "production"}}), "", func(cfg *Config) { cfg.Host = h0 })
	h.openSession("Deploy")
	h.send("Ship it")
	h.until("the question", func() bool { return h.hasText("Which environment?") })
	h.click("Settings")
	h.until("the accounts", func() bool { return h.hasText("b@y.dev") })
	h.click("Switch to b@y.dev")
	h.until("the warning", func() bool { return h.hasText("A session is working.") })
	if h0.ActiveAccount() != "" {
		t.Fatal("switched without asking")
	}
	h.shoot("settings-account-switch-busy")
	h.click("Cancel")
	h.until("the warning gone", func() bool { return !h.hasText("A session is working.") })
	h.click("Switch to b@y.dev")
	h.click("Switch anyway")
	h.until("the switch", func() bool { return h0.ActiveAccount() == "b1" })
}
