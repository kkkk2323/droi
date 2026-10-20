package host

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/kkkk2323/droi/apps/native/pkg/sessions"
)

func sessionIDs(list []ElsewhereSession) []string {
	var out []string
	for _, s := range list {
		out = append(out, s.SessionID+"@"+emailOf(s.Account))
	}
	return out
}

// What an account's Daemon listed shows under the other accounts, kept on
// disk, and goes with the account.
func TestTheSessionsOfAnotherAccountsOrganizationAreKept(t *testing.T) {
	h, _ := accountsHost(t)
	h.addAccount(jwt(map[string]any{"sub": "ub", "email": "b@y.dev", "org_id": "org_b"}), "rb")
	b := h.Accounts()[1].ID
	list := []sessions.Summary{
		{SessionID: "a1", Title: "On a", OrganizationID: "org_a"},
		{SessionID: "old", Title: "Before organizations"},
		{SessionID: "b0", Title: "Shown across", OrganizationID: "org_b"},
	}
	if err := h.RecordSessions("org_a", list); err != nil {
		t.Fatal(err)
	}
	if got := sessionIDs(h.SessionsElsewhere()); got != nil {
		t.Fatalf("the active account's own Sessions are elsewhere: %v", got)
	}
	rev := h.SessionIndexRev()
	h.RecordSessions("org_a", list)
	if h.SessionIndexRev() != rev {
		t.Fatal("the same list counted as a change")
	}

	h.SwitchAccount(b)
	if got := sessionIDs(h.SessionsElsewhere()); !slices.Equal(got, []string{"a1@a@x.dev"}) {
		t.Fatalf("under b@y.dev: %v", got)
	}
	// Kept across a restart of Droi.
	h2 := New(Config{UserData: h.cfg.UserData, Home: h.cfg.Home, Env: func(string) string { return "" }})
	t.Cleanup(h2.Stop)
	if got := sessionIDs(h2.SessionsElsewhere()); !slices.Equal(got, []string{"a1@a@x.dev"}) {
		t.Fatalf("after a restart: %v", got)
	}

	h.RecordSessions("org_b", []sessions.Summary{{SessionID: "b1", OrganizationID: "org_b"}})
	h.SwitchAccount("")
	if got := sessionIDs(h.SessionsElsewhere()); !slices.Equal(got, []string{"b1@b@y.dev"}) {
		t.Fatalf("back under a@x.dev: %v", got)
	}
	if err := h.RemoveAccount(b); err != nil {
		t.Fatal(err)
	}
	h.indexMu.Lock()
	_, kept := h.loadIndex().Orgs["org_b"]
	h.indexMu.Unlock()
	if kept {
		t.Fatal("the removed account's organization stayed in the index")
	}
}

func visibleOrgs(t *testing.T, shared string) (ids []string, raw map[string]json.RawMessage) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(shared, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("%v: %s", err, b)
	}
	if v, ok := raw[visibleOrgsKey]; ok {
		json.Unmarshal(v, &ids)
	}
	return ids, raw
}

// droid's setting names every account's organization, keeps the user's
// own ids and the rest of the file, and loses Droi's ids with the account.
func TestDroidIsToldEveryAccountsOrganization(t *testing.T) {
	h, shared := accountsHost(t)
	os.WriteFile(filepath.Join(shared, "settings.json"), []byte(`{"zeta": 1, "sessionVisibleOrganizationIds": ["org_mine"], "alpha": {"x": "\u2019"}}`), 0o644)
	os.Chmod(filepath.Join(shared, "settings.json"), 0o644)
	if err := h.shareSessionVisibility(); err != nil {
		t.Fatal(err)
	}
	if ids, _ := visibleOrgs(t, shared); !slices.Equal(ids, []string{"org_mine"}) {
		t.Fatalf("one account changed the setting: %v", ids)
	}
	h.addAccount(jwt(map[string]any{"sub": "ub", "email": "b@y.dev", "org_id": "org_b"}), "rb")
	if err := h.shareSessionVisibility(); err != nil {
		t.Fatal(err)
	}
	ids, raw := visibleOrgs(t, shared)
	var alpha bytes.Buffer
	json.Compact(&alpha, raw["alpha"])
	if !slices.Equal(ids, []string{"org_mine", "org_a", "org_b"}) || string(raw["zeta"]) != "1" || alpha.String() != `{"x":"\u2019"}` {
		t.Fatalf("%v %s", ids, raw)
	}
	b, _ := os.ReadFile(filepath.Join(shared, "settings.json"))
	if i, j := bytes.Index(b, []byte(`"zeta"`)), bytes.Index(b, []byte(`"alpha"`)); i > j {
		t.Fatalf("the keys moved: %s", b)
	}
	if st, _ := os.Stat(filepath.Join(shared, "settings.json")); st.Mode().Perm() != 0o644 {
		t.Fatalf("mode %v", st.Mode())
	}
	h.RemoveAccount(h.Accounts()[1].ID)
	if err := h.shareSessionVisibility(); err != nil {
		t.Fatal(err)
	}
	if ids, _ := visibleOrgs(t, shared); !slices.Equal(ids, []string{"org_mine"}) {
		t.Fatalf("after removing b@y.dev: %v", ids)
	}
	if got := h.Settings.Get().VisibleOrgsAdded; got != nil {
		t.Fatalf("still noted as added: %v", got)
	}
}

func TestSetJSONKeyKeepsTheOrderAndTakesAKeyOut(t *testing.T) {
	out, err := setJSONKey([]byte(`{"b": [1, 2], "a": {"c": true}}`), "b", nil)
	if err != nil || string(out) != "{\n  \"a\": {\n    \"c\": true\n  }\n}\n" {
		t.Fatalf("%q %v", out, err)
	}
	out, _ = setJSONKey(out, "k", []string{"x"})
	if string(out) != "{\n  \"a\": {\n    \"c\": true\n  },\n  \"k\": [\n    \"x\"\n  ]\n}\n" {
		t.Fatalf("%q", out)
	}
}
