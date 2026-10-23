package host

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/kkkk2323/droi/apps/native/pkg/droidpatch"
	"github.com/kkkk2323/droi/apps/native/pkg/keypool"
)

// patchedHost is a Host whose Settings choose a (made-up) Patched Droid that
// writes its environment to the file it returns.
func patchedHost(t *testing.T, withKey bool) (*Host, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("needs a shell")
	}
	data := t.TempDir()
	h := New(Config{UserData: data, Home: t.TempDir(), Env: func(string) string { return "" }})
	t.Cleanup(h.Stop)
	h.patch.once.Do(func() {
		h.patch.store = droidpatch.NewStore(filepath.Join(data, "droid"))
		h.patch.pool = keypool.Open(keypool.Options{Dir: filepath.Join(data, "llm"), UsageURL: "http://127.0.0.1:1/billing"})
	})
	t.Cleanup(h.patch.pool.Stop)
	if withKey {
		os.MkdirAll(filepath.Join(data, "llm"), 0o700)
		os.WriteFile(filepath.Join(data, "llm", "keys.json"), []byte(`{"mode":"auto","keys":[{"key":"fk-pool-0000000000"}]}`), 0o600)
		h.patch.pool = keypool.Open(keypool.Options{Dir: filepath.Join(data, "llm"), UsageURL: "http://127.0.0.1:1/billing"})
	}
	out := filepath.Join(t.TempDir(), "env")
	exe := h.patch.store.Path("0.247.0")
	os.MkdirAll(filepath.Dir(exe), 0o700)
	os.WriteFile(exe, []byte("#!/bin/sh\n[ \"$1\" = --version ] && { echo 0.247.0; exit 0; }\nenv > "+out+"\n"), 0o700)
	os.WriteFile(exe+".json", []byte(`{}`), 0o600)
	h.Settings.Update(func(s *Settings) {
		s.DroidKind = DroidPatched
		s.PatchedDroidVersion = "0.247.0"
		s.FactoryAPIBaseURL = OptString("http://upstream.example")
	})
	return h, out
}

func TestThePatchedDroidRunsWithTheKeyPool(t *testing.T) {
	h, out := patchedHost(t, true)
	if got := h.DroidPath(); got != h.patch.store.Path("0.247.0") {
		t.Fatalf("DroidPath %q", got)
	}
	cmd, err := h.spawn(1)
	if err != nil {
		t.Fatal(err)
	}
	cmd.Wait()
	b, _ := os.ReadFile(out)
	env := map[string]string{}
	for _, line := range strings.Split(string(b), "\n") {
		if k, v, ok := strings.Cut(line, "="); ok {
			env[k] = v
		}
	}
	pool := h.KeyPool()
	if env[droidpatch.KeyFileEnv] != pool.KeyFile() || env["FACTORY_DROID_AUTO_UPDATE_ENABLED"] != "false" {
		t.Fatalf("env %v", env)
	}
	if base := env["FACTORY_API_BASE_URL"]; !strings.HasPrefix(base, "http://127.0.0.1:") {
		t.Fatalf("FACTORY_API_BASE_URL %q is not the Key Pool's proxy", base)
	}
	if key, _ := os.ReadFile(pool.KeyFile()); string(key) != "fk-pool-0000000000" {
		t.Fatalf("key file %q", key)
	}
	if _, ok := env["FACTORY_API_KEY"]; ok {
		t.Fatal("the Daemon got FACTORY_API_KEY")
	}
}

func TestThePatchedDroidNeedsAKey(t *testing.T) {
	h, _ := patchedHost(t, false)
	if _, err := h.spawn(1); err == nil || !strings.Contains(err.Error(), "no keys") {
		t.Fatalf("spawn without keys: %v", err)
	}
}

func TestAMissingPatchedDroidIsNotTheOfficialOne(t *testing.T) {
	h, _ := patchedHost(t, true)
	h.Settings.Update(func(s *Settings) { s.PatchedDroidVersion = "0.1.0" })
	if h.DroidPath() != "" {
		t.Fatal("fell back to another droid")
	}
	if _, err := h.spawn(1); err != errNoPatchedDroid {
		t.Fatalf("spawn: %v", err)
	}
}

func TestPatchedDroidStateNamesTheNewestRecipe(t *testing.T) {
	h, _ := patchedHost(t, true)
	h.patch.store.Platform = "darwin/arm64"
	st := h.PatchedDroidState()
	if !st.Enabled || !st.Supported || st.Latest == "" || st.Version != "0.247.0" {
		t.Fatalf("%+v", st)
	}
	h.patch.store.Platform = "plan9/mips"
	if st := h.PatchedDroidState(); st.Supported {
		t.Fatalf("supported on plan9: %+v", st)
	}
}
