package app

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kkkk2323/droi/packages/droid-sdk-go/fakedaemon"

	"github.com/kkkk2323/droi/apps/native/internal/host"
)

func testJWT(claims map[string]any) string {
	b, _ := json.Marshal(claims)
	return "h." + base64.RawURLEncoding.EncodeToString(b) + ".s"
}

// accountsTestHost is a Host whose droid CLI is signed in as a@x.dev and
// that has b@y.dev added, laid out as Droi keeps them. Its Factory API is
// a stand-in that has a@x.dev near its 5-hour limit.
func accountsTestHost(t *testing.T) (*host.Host, string) {
	t.Helper()
	home, data := t.TempDir(), t.TempDir()
	aToken := testJWT(map[string]any{"sub": "ua", "email": "a@x.dev", "org_id": "org_acme"})
	if err := host.WriteCliLogin(filepath.Join(home, ".factory"), aToken, "r"); err != nil {
		t.Fatal(err)
	}
	if err := host.WriteCliLogin(filepath.Join(data, "accounts", "b1", ".factory"), testJWT(map[string]any{"sub": "ub", "email": "b@y.dev", "org_id": "org_side"}), "r"); err != nil {
		t.Fatal(err)
	}
	ends := time.Now().Add(3 * time.Hour).UTC().Format(time.RFC3339)
	factory := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		five, week := 22, 9
		if r.Header.Get("Authorization") == "Bearer "+aToken {
			five, week = 92, 41
		}
		fmt.Fprintf(w, `{"limits":{"standard":{"fiveHour":{"usedPercent":%d,"windowEnd":%q},"weekly":{"usedPercent":%d,"windowEnd":%q},"monthly":{"usedPercent":17,"windowEnd":%q}},"core":{}}}`, five, ends, week, ends, ends)
	}))
	t.Cleanup(factory.Close)
	os.WriteFile(filepath.Join(data, "settings.json"), []byte(`{"accounts":["b1"],"factoryApiBaseUrl":"`+factory.URL+`"}`), 0o600)
	h := host.New(host.Config{UserData: data, Home: home, Env: func(string) string { return "" }, MoveToTrash: os.Remove})
	t.Cleanup(h.Stop)
	return h, data
}

func TestTheAccountTabSwitchesAndRemovesAccounts(t *testing.T) {
	h0, data := accountsTestHost(t)
	h := newHarnessWith(t, oneSession(), "", func(cfg *Config) { cfg.Host = h0 })
	h.until("the Session list", func() bool { return h.hasText("One") })
	h.click("Settings")
	h.until("the accounts", func() bool { return h.hasText("b@y.dev") && h.hasText("In use") })
	if !h.hasText("droid CLI · org_acme") {
		t.Fatalf("the CLI's account is not marked: %q", h.tt.Texts())
	}
	// Each account shows its usage, and the one in use, near its limit, says so.
	h.until("the usage", func() bool { return h.hasText("92%") && h.hasText("22%") })
	if !h.hasText("This account is close to its limit.") {
		t.Fatalf("no hint to switch: %q", h.tt.Texts())
	}
	if _, ok := h.tt.Find("5h: 92% used"); !ok {
		t.Fatal("the 5-hour meter has no name")
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
