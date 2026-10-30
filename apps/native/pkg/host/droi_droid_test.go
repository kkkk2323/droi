package host

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/droidhook"
	"github.com/kkkk2323/droi/apps/native/pkg/keypool"
)

// fakeDroid is a made-up droid that writes its environment to out and, like
// the hook, checks in at the ready file.
func fakeDroid(out string) []byte {
	return []byte("#!/bin/sh\n[ \"$1\" = --version ] && { echo 0.247.0; exit 0; }\nenv > " + out + "\n[ -n \"$" + droidhook.ReadyEnv + "\" ] && printf '{\"pid\":%d}' $$ > \"$" + droidhook.ReadyEnv + "\"\nexit 0\n")
}

// droiHost is a Host whose Settings choose Droi's droid 0.247.0, a made-up
// one that writes its environment to the file it returns.
func droiHost(t *testing.T, withKey bool) (*Host, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("needs a shell")
	}
	data := t.TempDir()
	h := New(Config{UserData: data, Home: t.TempDir(), Env: func(string) string { return "" }, DroidDownloads: "http://127.0.0.1:1/releases"})
	t.Cleanup(h.Stop)
	if withKey {
		os.MkdirAll(filepath.Join(data, "llm"), 0o700)
		os.WriteFile(filepath.Join(data, "llm", "keys.json"), []byte(`{"mode":"auto","keys":[{"key":"fk-pool-0000000000"}]}`), 0o600)
	}
	h.droi()
	h.droiDroid.pool = keypool.Open(keypool.Options{Dir: filepath.Join(data, "llm"), UsageURL: "http://127.0.0.1:1/billing"})
	t.Cleanup(h.droiDroid.pool.Stop)
	out := filepath.Join(t.TempDir(), "env")
	exe := h.droiDroid.store.Path("0.247.0")
	os.MkdirAll(filepath.Dir(exe), 0o700)
	os.WriteFile(exe, fakeDroid(out), 0o700)
	os.WriteFile(exe+".verified", []byte(droidhook.HookVersion()), 0o600)
	h.Settings.Update(func(s *Settings) {
		s.DroidKind = DroidDroi
		s.DroiDroidVersion = "0.247.0"
	})
	return h, out
}

func readEnv(t *testing.T, path string) map[string]string {
	t.Helper()
	b, _ := os.ReadFile(path)
	env := map[string]string{}
	for _, line := range strings.Split(string(b), "\n") {
		if k, v, ok := strings.Cut(line, "="); ok {
			env[k] = v
		}
	}
	return env
}

// A Factory API base URL, such as droid-proxy's, would change the keys the
// Key Pool chose: the Key Pool refuses to start with one.
func TestTheKeyPoolRefusesAFactoryAPIBaseURL(t *testing.T) {
	h, _ := droiHost(t, true)
	h.Settings.Update(func(s *Settings) { s.FactoryAPIBaseURL = OptString("http://127.0.0.1:37650") })
	if _, err := h.spawn(1); err == nil || !strings.Contains(err.Error(), "127.0.0.1:37650") {
		t.Fatalf("spawn with a base URL: %v", err)
	}
	h.Settings.Update(func(s *Settings) { s.DroidKind = "" })
	h.prepareDroiDroid(false)
	if st := h.DroiDroidState(); st.Enabled || !strings.Contains(st.Err, "127.0.0.1:37650") {
		t.Fatalf("turned on with a base URL: %+v", st)
	}
	h.Settings.Update(func(s *Settings) { s.FactoryAPIBaseURL = OptString("https://api.factory.ai/") })
	if msg := h.baseURLConflict(); msg != "" {
		t.Fatalf("Factory's own URL is a conflict: %s", msg)
	}
}

// Droi's droid runs with the hook preloaded through BUN_OPTIONS, ahead of the
// options droid would have had, the key file and the ready file, against the
// Key Pool's proxy.
func TestDroisDroidRunsWithTheHook(t *testing.T) {
	h, out := droiHost(t, true)
	t.Setenv("BUN_OPTIONS", "--smol")
	if got := h.DroidPath(); got != h.droiDroid.store.Path("0.247.0") {
		t.Fatalf("DroidPath %q", got)
	}
	cmd, err := h.spawn(1)
	if err != nil {
		t.Fatal(err)
	}
	cmd.Wait()
	env := readEnv(t, out)
	pool := h.KeyPool()
	hook := filepath.Join(h.droiDroid.store.Dir, "llm-hook.js")
	if want := `"--preload=` + hook + `" --smol`; env["BUN_OPTIONS"] != want {
		t.Fatalf("BUN_OPTIONS %q, want %q", env["BUN_OPTIONS"], want)
	}
	if b, err := os.ReadFile(hook); err != nil || !strings.Contains(string(b), droidhook.KeyFileEnv) {
		t.Fatalf("no hook at %s: %v", hook, err)
	}
	if env[droidhook.KeyFileEnv] != pool.KeyFile() || env["FACTORY_DROID_AUTO_UPDATE_ENABLED"] != "false" || env[droidhook.ReadyEnv] == "" {
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
	if err := h.checkDaemon(cmd.Process.Pid); err != nil {
		t.Fatalf("the hook's check-in was refused: %v", err)
	}
	if err := h.checkDaemon(cmd.Process.Pid + 1); err == nil {
		t.Fatal("another process passed for the Daemon")
	}
}

// Bun ignores a --preload behind another option without a word: a Daemon
// whose hook did not check in is stopped, and the window says why.
func TestADaemonWithoutTheHookIsStopped(t *testing.T) {
	h, _ := droiHost(t, true)
	exe := h.droiDroid.store.Path("0.247.0")
	daemon := "#!/bin/sh\n[ \"$1\" = --version ] && { echo 0.247.0; exit 0; }\nport=$5\nexec python3 -c \"import http.server,sys\nclass H(http.server.BaseHTTPRequestHandler):\n  def do_GET(s): s.send_response(200); s.end_headers()\n  def log_message(s,*a): pass\nhttp.server.HTTPServer(('127.0.0.1',int(sys.argv[1])),H).serve_forever()\" $port\n"
	os.WriteFile(exe, []byte(daemon), 0o700)
	h.Daemon.Backoff = []time.Duration{time.Hour}
	h.Daemon.Start()
	defer h.Daemon.Stop()
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if st := h.Daemon.State(); st.Status == DaemonRestarting {
			if !strings.Contains(st.Reason, "BUN_OPTIONS") {
				t.Fatalf("reason %q", st.Reason)
			}
			return
		}
		if st := h.Daemon.State(); st.Status == DaemonRunning {
			t.Fatal("a Daemon without the hook runs")
		}
	}
	t.Fatalf("the Daemon was not stopped: %+v", h.Daemon.State())
}

// The official droid gets neither the hook's files nor the Key Pool.
func TestTheOfficialDroidGetsNoHook(t *testing.T) {
	h, out := droiHost(t, true)
	official := filepath.Join(t.TempDir(), "droid")
	os.WriteFile(official, fakeDroid(out), 0o700)
	t.Setenv(droidhook.KeyFileEnv, "/elsewhere/llm-key")
	t.Setenv(droidhook.ReadyEnv, "/elsewhere/ready")
	h.Settings.Update(func(s *Settings) {
		s.DroidKind = ""
		s.DroidPath = OptString(official)
	})
	cmd, err := h.spawn(1)
	if err != nil {
		t.Fatal(err)
	}
	cmd.Wait()
	env := readEnv(t, out)
	if _, ok := env[droidhook.KeyFileEnv]; ok {
		t.Fatalf("the official droid got the key file: %v", env)
	}
	if _, ok := env[droidhook.ReadyEnv]; ok {
		t.Fatalf("the official droid got the ready file: %v", env)
	}
	if err := h.checkDaemon(cmd.Process.Pid); err != nil {
		t.Fatalf("the official droid was checked for the hook: %v", err)
	}
}

func TestDroisDroidNeedsAKey(t *testing.T) {
	h, _ := droiHost(t, false)
	if _, err := h.spawn(1); err == nil || !strings.Contains(err.Error(), "Settings → Usage") {
		t.Fatalf("spawn without keys: %v", err)
	}
}

func TestAMissingDroisDroidIsNotTheOfficialOne(t *testing.T) {
	h, _ := droiHost(t, true)
	h.Settings.Update(func(s *Settings) { s.DroiDroidVersion = "0.1.0" })
	if h.DroidPath() != "" {
		t.Fatal("fell back to another droid")
	}
	if _, err := h.spawn(1); err == nil || !strings.Contains(err.Error(), "Droi's droid is missing") {
		t.Fatalf("spawn: %v", err)
	}
}

// factoryReleases serves LATEST and one droid build the way Factory does.
func factoryReleases(t *testing.T, latest string, build []byte, published *atomic.Bool) string {
	t.Helper()
	sum := sha256.Sum256(build)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case published != nil && !published.Load():
			http.NotFound(w, r)
		case r.URL.Path == "/LATEST":
			w.Write([]byte(latest + "\n"))
		case strings.HasSuffix(r.URL.Path, "/"+latest+"/"+platformPath()+".sha256"):
			w.Write([]byte(hex.EncodeToString(sum[:]) + "  droid\n"))
		case strings.HasSuffix(r.URL.Path, "/"+latest+"/"+platformPath()):
			w.Write(build)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/releases"
}

func platformPath() string {
	arch := runtime.GOARCH
	if arch == "amd64" {
		arch = "x64"
	}
	return runtime.GOOS + "/" + arch + "/" + droidBinary()
}

// Checking for updates asks Factory for its newest droid at once, even
// within the hour of the last check, and says why when it cannot.
func TestCheckingForANewerDroisDroid(t *testing.T) {
	h, _ := droiHost(t, true)
	var published atomic.Bool
	base := factoryReleases(t, "9.9.9", []byte("droid"), &published)
	h.droiDroid.store.DownloadBase, h.droiDroid.store.LatestURL = base, droidhook.LatestURLFor(base)
	h.droiDroid.refreshed = time.Now()
	check := func() DroiDroid {
		t.Helper()
		h.CheckDroiDroid()
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
			if st := h.DroiDroidState(); !st.Checking {
				return st
			}
		}
		t.Fatal("the check did not end")
		return DroiDroid{}
	}
	if st := check(); st.Checked || !strings.Contains(st.CheckErr, "404") {
		t.Fatalf("no answer: %+v", st)
	}
	published.Store(true)
	if st := check(); !st.Checked || st.CheckErr != "" || st.Latest != "9.9.9" || !st.UpdateAvailable() {
		t.Fatalf("a newer droid: %+v", st)
	}
}

// A Host that ran the Patched Droid (ADR 0027) runs Droi's droid: it
// prepares Factory's build of the newest version, verified with the hook,
// and drops the patched builds.
func TestThePatchedDroidMovesToDroisDroid(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs a shell")
	}
	data := t.TempDir()
	os.WriteFile(filepath.Join(data, "settings.json"), []byte(`{"droidKind":"patched","patchedDroidVersion":"0.251.0"}`), 0o600)
	patched := filepath.Join(data, "droid", "0.251.0", droidBinary())
	os.MkdirAll(filepath.Dir(patched), 0o700)
	os.WriteFile(patched, []byte("patched"), 0o700)
	base := factoryReleases(t, "9.9.9", []byte("official droid"), nil)
	h := New(Config{UserData: data, Home: t.TempDir(), Env: func(string) string { return "" }, DroidDownloads: base})
	t.Cleanup(h.Stop)
	st := h.DroiDroidState()
	if !st.Enabled || st.Version != "" || h.DroidPath() != "" {
		t.Fatalf("before: %+v, path %q", st, h.DroidPath())
	}
	verified := 0
	restore := droidhook.SetVerifyForTest(func(context.Context, string, string) error { verified++; return nil })
	defer restore()
	h.prepareDroiDroid(false)
	if st := h.DroiDroidState(); st.Err != "" || st.Version != "9.9.9" || st.Latest != "9.9.9" || verified != 1 {
		t.Fatalf("after: %+v, verified %d", st, verified)
	}
	if got := h.Settings.Get().DroidKind; got != DroidDroi {
		t.Fatalf("DroidKind %q", got)
	}
	if b, _ := os.ReadFile(h.DroidPath()); string(b) != "official droid" {
		t.Fatalf("Droi's droid is %q", b)
	}
	if _, err := os.Stat(patched); !os.IsNotExist(err) {
		t.Fatal("the Patched Droid was kept")
	}
}
