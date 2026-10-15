package host

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// accountsHost is a Host whose droid CLI is signed in as a@x.dev, with a
// ~/.factory that has skills, MCP servers, settings and an AGENTS.md.
func accountsHost(t *testing.T) (h *Host, shared string) {
	t.Helper()
	home, data := t.TempDir(), t.TempDir()
	shared = filepath.Join(home, ".factory")
	os.MkdirAll(filepath.Join(shared, "skills", "tdd"), 0o700)
	os.WriteFile(filepath.Join(shared, "mcp.json"), []byte(`{"mcpServers":{}}`), 0o600)
	os.WriteFile(filepath.Join(shared, "settings.json"), []byte(`{}`), 0o600)
	os.WriteFile(filepath.Join(shared, "AGENTS.md"), []byte("v1"), 0o600)
	if err := WriteCliLogin(shared, jwt(map[string]any{"sub": "ua", "email": "a@x.dev", "org_id": "org_a"}), "ra"); err != nil {
		t.Fatal(err)
	}
	h = New(Config{UserData: data, Home: home, Env: func(string) string { return "" }})
	t.Cleanup(h.Stop)
	return h, shared
}

func emailOf(a AccountInfo) string {
	if a.Login == nil || a.Login.Email == nil {
		return ""
	}
	return *a.Login.Email
}

func TestAnAddedAccountJoinsTheListUnused(t *testing.T) {
	h, _ := accountsHost(t)
	if err := h.addAccount(jwt(map[string]any{"sub": "ub", "email": "b@y.dev", "org_id": "org_b"}), "rb"); err != nil {
		t.Fatal(err)
	}
	// The same account again only takes the new login.
	if err := h.addAccount(jwt(map[string]any{"sub": "ub", "email": "B@y.dev", "org_id": "org_b"}), "rb2"); err != nil {
		t.Fatal(err)
	}
	list := h.Accounts()
	if len(list) != 2 || list[0].ID != "" || !list[0].Active || emailOf(list[0]) != "a@x.dev" {
		t.Fatalf("%+v", list)
	}
	if list[1].Active || emailOf(list[1]) != "B@y.dev" {
		t.Fatalf("%+v", list[1])
	}
	if s := h.LoginState(); s.Account == nil || *s.Account.Email != "a@x.dev" {
		t.Fatalf("adding switched accounts: %+v", s)
	}
	// The same email in another organization is another account.
	if err := h.addAccount(jwt(map[string]any{"sub": "ub", "email": "b@y.dev", "org_id": "org_c"}), "rc"); err != nil {
		t.Fatal(err)
	}
	if n := len(h.Accounts()); n != 3 {
		t.Fatalf("%d accounts", n)
	}
}

func TestSwitchingMovesTheLoginTheHostUses(t *testing.T) {
	h, shared := accountsHost(t)
	bToken := jwt(map[string]any{"sub": "ub", "email": "b@y.dev", "org_id": "org_b"})
	h.addAccount(bToken, "rb")
	id := h.Accounts()[1].ID
	if err := h.SwitchAccount("nope"); !errors.Is(err, errNoAccount) {
		t.Fatalf("unknown account: %v", err)
	}
	if err := h.SwitchAccount(id); err != nil {
		t.Fatal(err)
	}
	if h.FactoryHome() == shared || !strings.HasPrefix(h.FactoryHome(), h.cfg.UserData) {
		t.Fatalf("FactoryHome %s", h.FactoryHome())
	}
	if cred, _ := h.Credential(t.Context()); cred == nil || cred.Token != bToken {
		t.Fatalf("credential %+v", cred)
	}
	// Signing out signs the account in use out, not the droid CLI.
	if err := h.SignOut(); err != nil {
		t.Fatal(err)
	}
	if h.LoginState().Status != SignedOut || h.cli(shared).Read() == nil {
		t.Fatalf("%+v", h.LoginState())
	}
	if err := h.RemoveAccount(id); !errors.Is(err, errAccountInUse) {
		t.Fatalf("removed the account in use: %v", err)
	}
}

func TestTheDaemonOfAnAddedAccountSharesTheCLIsHome(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs a shell")
	}
	h, shared := accountsHost(t)
	h.addAccount(jwt(map[string]any{"sub": "ub", "email": "b@y.dev", "org_id": "org_b"}), "rb")
	id := h.Accounts()[1].ID
	out := filepath.Join(t.TempDir(), "env")
	droidPath := filepath.Join(t.TempDir(), "droid")
	os.WriteFile(droidPath, []byte("#!/bin/sh\nprintf '%s|%s' \"$FACTORY_HOME_OVERRIDE\" \"$FACTORY_API_KEY\" > "+out+"\n"), 0o700)
	h.Settings.Update(func(s *Settings) {
		s.DroidPath = OptString(droidPath)
		s.APIKey = OptString("fk-stored")
		s.ActiveAccount = id
	})
	run := func() string {
		t.Helper()
		cmd, err := h.spawn(1)
		if err != nil {
			t.Fatal(err)
		}
		cmd.Wait()
		b, _ := os.ReadFile(out)
		return string(b)
	}
	root := h.accountRoot(id)
	// Signed in, so the stored key stays out of the Daemon's way.
	if got := run(); got != root+"|" {
		t.Fatalf("env %q", got)
	}
	home := filepath.Join(root, ".factory")
	for _, name := range []string{"skills", "mcp.json", "settings.json", "sessions"} {
		if to, err := os.Readlink(filepath.Join(home, name)); err != nil || to != filepath.Join(shared, name) {
			t.Errorf("%s: %q %v", name, to, err)
		}
	}
	// A folder the CLI lacks is made there, so the account never grows its own.
	if to, err := os.Readlink(filepath.Join(home, "hooks")); err != nil || to != filepath.Join(shared, "hooks") {
		t.Errorf("hooks: %q %v", to, err)
	}
	// An empty folder droid made for the account gives way to the link; one
	// with files in it stays the account's.
	os.Remove(filepath.Join(home, "droids"))
	os.Mkdir(filepath.Join(home, "droids"), 0o700)
	os.Remove(filepath.Join(home, "commands"))
	os.MkdirAll(filepath.Join(home, "commands", "mine"), 0o700)
	run()
	if _, err := os.Readlink(filepath.Join(home, "droids")); err != nil {
		t.Error("the empty droids folder was kept")
	}
	if _, err := os.Stat(filepath.Join(home, "commands", "mine")); err != nil {
		t.Error("the account's own commands were replaced")
	}
	agents := func() string {
		st, err := os.Lstat(filepath.Join(home, "AGENTS.md"))
		if err != nil {
			return ""
		}
		if !st.Mode().IsRegular() {
			t.Fatal("AGENTS.md is not a copy")
		}
		b, _ := os.ReadFile(filepath.Join(home, "AGENTS.md"))
		return string(b)
	}
	if agents() != "v1" {
		t.Fatalf("AGENTS.md %q", agents())
	}
	os.WriteFile(filepath.Join(shared, "AGENTS.md"), []byte("v2"), 0o600)
	run()
	if agents() != "v2" {
		t.Fatalf("AGENTS.md not copied again: %q", agents())
	}
	os.Remove(filepath.Join(shared, "AGENTS.md"))
	run()
	if agents() != "" {
		t.Fatal("AGENTS.md outlived the CLI's")
	}

	// Removing the account deletes its folder and nothing it linked to.
	h.Settings.Update(func(s *Settings) { s.ActiveAccount = "" })
	if err := h.RemoveAccount(id); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("the account's folder stayed")
	}
	for _, p := range []string{filepath.Join(shared, "skills", "tdd"), filepath.Join(shared, "mcp.json"), filepath.Join(shared, "sessions")} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("removing the account took %s", p)
		}
	}
	if len(h.Accounts()) != 1 {
		t.Fatal("the account is still listed")
	}
	// The droid CLI's own account runs without the override.
	if got := run(); got != "|" {
		t.Fatalf("env %q", got)
	}
}
