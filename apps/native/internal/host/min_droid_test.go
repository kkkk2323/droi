package host

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestOlderThanMinDroid(t *testing.T) {
	for v, want := range map[string]bool{
		"0.232.9": true, "0.199.0": true, MinDroid: false, "0.233.1": false, "0.240.0\n": false,
		"1.0.0": false, "v0.100.0": true, "0.100.0-beta.1": true, "": false, "garbage": false,
	} {
		if got := OlderThanMinDroid(v); got != want {
			t.Errorf("%q: %v, want %v", v, got, want)
		}
	}
}

// oldDroidHost is a Host whose droid is 0.200.0 and runs update as the
// shell script given.
func oldDroidHost(t *testing.T, update string) *Host {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("needs a shell")
	}
	path := filepath.Join(t.TempDir(), "droid")
	script := "#!/bin/sh\nif [ \"$1\" = update ]; then\n" + update + "\nexit 0\nfi\necho 0.200.0\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	h := New(Config{UserData: t.TempDir(), Home: t.TempDir(), Env: func(string) string { return "" }})
	h.Settings.Update(func(s *Settings) { s.DroidPath = OptString(path) })
	t.Cleanup(h.Stop)
	return h
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatal("no " + what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestTheDaemonDoesNotStartFromAnOldDroid(t *testing.T) {
	h := oldDroidHost(t, "")
	h.Start()
	waitFor(t, "refusal", func() bool { return h.Daemon.State().DroidTooOld() })
	if v := h.DroidVersion(); v != "0.200.0" {
		t.Fatalf("version %q", v)
	}
}

func TestUpdateDroidStartsTheDaemonFromTheNewDroid(t *testing.T) {
	// What a Session's command inherits from its Daemon; droid update must
	// not see it.
	t.Setenv("FACTORY_DROID_AUTO_UPDATE_ENABLED", "false")
	h := oldDroidHost(t, `if [ -n "$FACTORY_DROID_AUTO_UPDATE_ENABLED" ]; then echo "Auto-update is not available for npm installations."; exit 0; fi
printf '#!/bin/sh\necho 0.240.0\n' > "$0.new" && chmod +x "$0.new" && mv "$0.new" "$0"
echo "Successfully updated to 0.240.0"`)
	h.Start()
	waitFor(t, "refusal", func() bool { return h.Daemon.State().DroidTooOld() })
	h.UpdateDroid()
	if u := h.DroidUpdateState(); u.Running || u.Err != "" {
		t.Fatalf("update %+v", u)
	}
	if v := h.DroidVersion(); v != "0.240.0" {
		t.Fatalf("version %q", v)
	}
	waitFor(t, "start from the new droid", func() bool {
		st := h.Daemon.State()
		return st.Status != DaemonStopped && !st.DroidTooOld()
	})
}

func TestUpdateDroidSaysWhyDroidIsStillOld(t *testing.T) {
	h := oldDroidHost(t, `echo "Checking for updates"
echo "Update to 0.240.0 skipped"`)
	h.UpdateDroid()
	if u := h.DroidUpdateState(); u.Running || u.Err != "Update to 0.240.0 skipped" {
		t.Fatalf("update %+v", u)
	}
	if st := h.Daemon.State(); st.Status != "" && st.Status != DaemonStopped {
		t.Fatal("started a Daemon from a droid still too old")
	}
	if !strings.Contains(h.DroidVersion(), "0.200.0") {
		t.Fatalf("version %q", h.DroidVersion())
	}
}
