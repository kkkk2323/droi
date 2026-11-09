//go:build !windows

package host

import (
	"os"
	"path/filepath"
	"testing"
)

// fakeShell is a shell that prints noise around what it runs, as rc files do, and has PATH set as the person's.
func fakeShell(t *testing.T, body string) string {
	t.Helper()
	shell := filepath.Join(t.TempDir(), "zsh")
	if err := os.WriteFile(shell, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return shell
}

func TestLoginPathIsTheShellsAheadOfLaunchds(t *testing.T) {
	// The fake shell takes -i -l -c script name marker, as the real one does.
	shell := fakeShell(t, `echo "welcome back"
PATH=/opt/homebrew/bin:/Users/me/.bun/bin:/usr/bin
shift 3
/bin/sh -c "$1" "$2" "$3"
echo "bye"
`)
	got := loginPath(shell, "/usr/bin:/bin:/usr/sbin:/sbin")
	if want := "/opt/homebrew/bin:/Users/me/.bun/bin:/usr/bin:/bin:/usr/sbin:/sbin"; got != want {
		t.Errorf("loginPath = %q, want %q", got, want)
	}
}

func TestLoginPathKeepsLaunchdsWhenTheShellFails(t *testing.T) {
	for name, body := range map[string]string{
		"exits":    "exit 1\n",
		"no path":  "echo nothing here\n",
		"no shell": "",
	} {
		shell := fakeShell(t, body)
		if name == "no shell" {
			shell = filepath.Join(t.TempDir(), "missing")
		}
		if got := loginPath(shell, "/usr/bin:/bin"); got != "/usr/bin:/bin" {
			t.Errorf("%s: loginPath = %q", name, got)
		}
	}
}

func TestLoginPathOfThisComputersShell(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the login shell")
	}
	got := loginPath(os.Getenv("SHELL"), "/usr/bin:/bin")
	if got == "" {
		t.Error("no PATH")
	}
	t.Logf("PATH: %s", got)
}
