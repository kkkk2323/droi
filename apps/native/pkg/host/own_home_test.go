package host

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// ownHomeHost is a Host with a Factory home of its own, on a computer whose
// droid CLI is signed in as a@x.dev.
func ownHomeHost(t *testing.T) (h *Host, root, cliHome string) {
	t.Helper()
	home, data := t.TempDir(), t.TempDir()
	cliHome = filepath.Join(home, ".factory")
	if err := WriteCliLogin(cliHome, jwt(map[string]any{"sub": "ua", "email": "a@x.dev", "org_id": "org_a"}), "ra"); err != nil {
		t.Fatal(err)
	}
	root = filepath.Join(data, "factory")
	h = New(Config{UserData: data, Home: home, FactoryRoot: root, Env: func(string) string { return "" }})
	t.Cleanup(h.Stop)
	return h, root, cliHome
}

func TestAHostWithAnOwnFactoryHomeIgnoresTheCLIsLogin(t *testing.T) {
	h, root, cliHome := ownHomeHost(t)
	if got, want := h.FactoryHome(), filepath.Join(root, ".factory"); got != want {
		t.Fatalf("FactoryHome %q, want %q", got, want)
	}
	if h.LoginState().Status != SignedOut {
		t.Fatalf("signed in with the CLI's login: %+v", h.LoginState())
	}
	// Its sign-in goes to its own home, and signing out leaves the CLI's alone.
	if err := h.Auth.Handoff(jwt(map[string]any{"sub": "ub", "email": "b@y.dev", "org_id": "org_b"}), "rb"); err != nil {
		t.Fatal(err)
	}
	if s := h.LoginState(); s.Status != SignedIn || s.Account == nil || s.Account.Email == nil || *s.Account.Email != "b@y.dev" {
		t.Fatalf("%+v", s)
	}
	if err := h.SignOut(); err != nil {
		t.Fatal(err)
	}
	if h.LoginState().Status != SignedOut || h.cli(cliHome).Read() == nil {
		t.Fatal("signing out touched the CLI's login")
	}
}

func TestTheDaemonOfAnOwnFactoryHomeRunsThere(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs a shell")
	}
	h, root, _ := ownHomeHost(t)
	out := filepath.Join(t.TempDir(), "env")
	droidPath := filepath.Join(t.TempDir(), "droid")
	os.WriteFile(droidPath, []byte("#!/bin/sh\nprintf '%s' \"$FACTORY_HOME_OVERRIDE\" > "+out+"\n"), 0o700)
	h.Settings.Update(func(s *Settings) { s.DroidPath = OptString(droidPath) })
	cmd, err := h.spawn(1)
	if err != nil {
		t.Fatal(err)
	}
	cmd.Wait()
	if b, _ := os.ReadFile(out); string(b) != root {
		t.Fatalf("FACTORY_HOME_OVERRIDE %q, want %q", b, root)
	}
	// Nothing links back to the CLI's ~/.factory: its Sessions stay its own.
	if _, err := os.Lstat(filepath.Join(root, ".factory", "sessions")); err == nil {
		if to, err := os.Readlink(filepath.Join(root, ".factory", "sessions")); err == nil {
			t.Fatalf("sessions links to %q", to)
		}
	}
}

func TestScratchDefaultIsTheAppsOwn(t *testing.T) {
	home := t.TempDir()
	h := New(Config{UserData: t.TempDir(), Home: home, Env: func(string) string { return "" }})
	if got, want := h.ScratchFolder(), filepath.Join(home, ".droi", "chats"); got != want {
		t.Fatalf("%q, want %q", got, want)
	}
	mine := filepath.Join(home, "bot", "chats")
	h = New(Config{UserData: t.TempDir(), Home: home, ScratchFolder: mine, Env: func(string) string { return "" }})
	if got := h.ScratchFolder(); got != mine {
		t.Fatalf("%q, want %q", got, mine)
	}
}
