package droidhook

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/netproxy"
)

// Bun reads BUN_OPTIONS like a shell: the hook's path stays one argument
// with spaces in it, comes before every other option (Bun ignores a
// --preload after one) and, on Windows, has forward slashes.
func TestBunOptions(t *testing.T) {
	mac := "/Users/a/Library/Application Support/Droi/droid/llm-hook.js"
	cases := []struct{ goos, hook, existing, want string }{
		{"darwin", mac, "", `"--preload=` + mac + `"`},
		{"darwin", mac, "--smol", `"--preload=` + mac + `" --smol`},
		{"darwin", mac, `"--preload=` + mac + `" --smol`, `"--preload=` + mac + `" --smol`},
		{"windows", `C:\Users\安蓝\AppData\Roaming\Droi\droid\llm-hook.js`, "", `"--preload=C:/Users/安蓝/AppData/Roaming/Droi/droid/llm-hook.js"`},
	}
	for _, c := range cases {
		if got := bunOptions(c.goos, c.hook, c.existing); got != c.want {
			t.Errorf("bunOptions(%s, %q, %q) = %q, want %q", c.goos, c.hook, c.existing, got, c.want)
		}
	}
}

func TestCheckReady(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ready")
	if CheckReady(path, 42) == nil {
		t.Fatal("no file passed")
	}
	os.WriteFile(path, []byte(`{"pid":41}`), 0o600)
	if CheckReady(path, 42) == nil {
		t.Fatal("another process's check-in passed")
	}
	os.WriteFile(path, []byte(`{"pid":42}`), 0o600)
	if err := CheckReady(path, 42); err != nil {
		t.Fatal(err)
	}
}

func TestWriteHook(t *testing.T) {
	dir := t.TempDir()
	path, err := WriteHook(dir)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if string(b) != string(hook) || !strings.Contains(string(b), KeyFileEnv) || !strings.Contains(string(b), ReadyEnv) {
		t.Fatal("the hook on disk is not the embedded one")
	}
	if st, _ := os.Stat(path); st.Mode().Perm()&0o077 != 0 && os.PathSeparator == '/' {
		t.Fatalf("the hook is mode %v", st.Mode())
	}
}

func TestLatestURLFor(t *testing.T) {
	if got := LatestURLFor("https://downloads.factory.ai/factory-cli/releases/"); got != "https://downloads.factory.ai/factory-cli/LATEST" {
		t.Fatal(got)
	}
	if got := LatestURLFor("https://mirror.example/droid"); got != "https://mirror.example/droid/LATEST" {
		t.Fatal(got)
	}
}

// Downloads go through the proxy set in the system's settings, as a
// browser's do, not only HTTP_PROXY's.
func TestTheStoreDownloadsThroughTheSystemProxy(t *testing.T) {
	if NewStore(t.TempDir()).HTTP.Transport != netproxy.Transport() {
		t.Fatal("the store does not follow the system's proxy")
	}
}

func fakeStore(t *testing.T, official []byte) (*Store, *int, *[]string) {
	t.Helper()
	var verified []string
	verifyFile = func(_ context.Context, exe, hook string) error {
		verified = append(verified, exe)
		if _, err := os.Stat(hook); err != nil {
			t.Errorf("verified without the hook: %v", err)
		}
		return nil
	}
	t.Cleanup(func() { verifyFile = Verify })
	sum := sha256.Sum256(official)
	downloads := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/LATEST":
			w.Write([]byte("9.9.9\n"))
		case strings.HasSuffix(r.URL.Path, "/9.9.9/darwin/x64/droid.sha256"):
			w.Write([]byte(hex.EncodeToString(sum[:]) + "  droid\n"))
		case strings.HasSuffix(r.URL.Path, "/9.9.9/darwin/x64/droid"):
			downloads++
			w.Header().Set("Content-Length", strconv.Itoa(len(official)))
			w.Write(official)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	s := NewStore(filepath.Join(t.TempDir(), "droid"))
	s.Platform = "darwin/amd64"
	s.DownloadBase = srv.URL + "/releases"
	s.LatestURL = LatestURLFor(s.DownloadBase)
	return s, &downloads, &verified
}

func TestPrepareDownloadsChecksAndVerifies(t *testing.T) {
	official := []byte("official droid")
	s, downloads, verified := fakeStore(t, official)
	v, err := s.Latest(context.Background())
	if err != nil || v != "9.9.9" {
		t.Fatalf("Latest %q, %v", v, err)
	}
	var progress []Progress
	path, err := s.Prepare(context.Background(), v, "", func(p Progress) { progress = append(progress, p) })
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); string(b) != string(official) || path != s.Path(v) || !s.Ready(v) {
		t.Fatalf("path %s ready %v", path, s.Ready(v))
	}
	if *downloads != 1 || len(*verified) != 1 || len(progress) == 0 {
		t.Fatalf("downloads %d, verified %v, progress %v", *downloads, *verified, progress)
	}
	if _, err := os.Stat(path + ".new"); !os.IsNotExist(err) {
		t.Fatal("the download was left beside the build")
	}
	if _, err := s.Prepare(context.Background(), v, "", nil); err != nil || *downloads != 1 || len(*verified) != 1 {
		t.Fatalf("a ready build was prepared again: %v, downloads %d, verified %d", err, *downloads, len(*verified))
	}
	// Another hook: the same build is verified again, not downloaded.
	os.WriteFile(s.verified(v), []byte("older-hook\n"), 0o600)
	if s.Ready(v) {
		t.Fatal("ready with another hook")
	}
	if _, err := s.Prepare(context.Background(), v, "", nil); err != nil || *downloads != 1 || len(*verified) != 2 {
		t.Fatalf("another hook: %v, downloads %d, verified %d", err, *downloads, len(*verified))
	}
}

func TestPrepareUsesTheLocalOfficialBuild(t *testing.T) {
	official := []byte("official droid")
	s, downloads, _ := fakeStore(t, official)
	local := filepath.Join(t.TempDir(), "droid")
	os.WriteFile(local, official, 0o700)
	if _, err := s.Prepare(context.Background(), "9.9.9", local, nil); err != nil || *downloads != 0 {
		t.Fatalf("Prepare: %v, downloads %d", err, *downloads)
	}
	// A local droid that is not Factory's build is not used.
	os.WriteFile(local, append(official, 'x'), 0o700)
	s.Prune()
	if _, err := s.Prepare(context.Background(), "9.9.9", local, nil); err != nil || *downloads != 1 {
		t.Fatalf("Prepare: %v, downloads %d", err, *downloads)
	}
}

func TestAFailedVerifyLeavesNoReadyBuild(t *testing.T) {
	s, _, _ := fakeStore(t, []byte("official droid"))
	verifyFile = func(context.Context, string, string) error { return ErrNotLoaded }
	if _, err := s.Prepare(context.Background(), "9.9.9", "", nil); err == nil || s.Ready("9.9.9") {
		t.Fatalf("a build the hook did not run in is ready: %v", err)
	}
}

func TestPrepareRefusesAnUnknownPlatform(t *testing.T) {
	s, _, _ := fakeStore(t, []byte("official droid"))
	s.Platform = "plan9/mips"
	if _, err := s.Prepare(context.Background(), "9.9.9", "", nil); err == nil {
		t.Fatal("prepared a droid Factory does not build")
	}
}

// Prune keeps the version in use and removes the rest, the Patched Droid's
// builds and recipes too.
func TestPrune(t *testing.T) {
	s, _, _ := fakeStore(t, []byte("official droid"))
	for _, v := range []string{"0.251.0", "9.9.9"} {
		os.MkdirAll(filepath.Join(s.Dir, v), 0o700)
	}
	os.WriteFile(filepath.Join(s.Dir, "recipes.json"), []byte("{}"), 0o600)
	hook, _ := s.Hook()
	s.Prune("9.9.9")
	for path, want := range map[string]bool{filepath.Join(s.Dir, "9.9.9"): true, filepath.Join(s.Dir, "0.251.0"): false, filepath.Join(s.Dir, "recipes.json"): false, hook: true} {
		if _, err := os.Stat(path); (err == nil) != want {
			t.Errorf("%s exists: %v, want %v", path, err == nil, want)
		}
	}
}

func TestCompareVersions(t *testing.T) {
	if CompareVersions("0.247.0", "0.99.1") <= 0 || CompareVersions("1.0.0", "1.0.0") != 0 || CompareVersions("x", "0.1.0") >= 0 {
		t.Fatal("version order")
	}
}

// DROI_HOOK_LIVE=1 runs Verify against the droid on PATH: the hook must
// run in the very build Factory ships. It talks to a local receiver only.
func TestVerifyWithTheInstalledDroid(t *testing.T) {
	if os.Getenv("DROI_HOOK_LIVE") == "" {
		t.Skip("set DROI_HOOK_LIVE=1 to run droid")
	}
	exe, err := exec.LookPath("droid")
	if err != nil {
		t.Skip("no droid on PATH")
	}
	dir := filepath.Join(t.TempDir(), "Application Support", "Droi 安蓝")
	hook, err := WriteHook(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(context.Background(), exe, hook); err != nil {
		t.Fatal(err)
	}
	// A missing hook stops droid; one Bun skipped would not check in either.
	if err := Verify(context.Background(), exe, filepath.Join(dir, "missing.js")); err == nil {
		t.Fatal("verified a droid without the hook")
	}
}

// droid has the key file open for a moment on each request it sends;
// replacing the file meanwhile must not fail. Windows refuses a rename over
// an open file, which WritePrivate retries. The reader here reads 500 times a
// second, far more often than droid sends requests.
func TestWritePrivateWhileTheFileIsRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "llm-key")
	if err := WritePrivate(path, []byte("fk-a")); err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
			}
			if f, err := os.Open(path); err == nil {
				_, _ = io.ReadAll(f)
				f.Close()
			}
			time.Sleep(2 * time.Millisecond)
		}
	}()
	for i := range 300 {
		if err := WritePrivate(path, []byte("fk-"+strconv.Itoa(i))); err != nil {
			close(stop)
			<-done
			t.Fatalf("write %d: %v", i, err)
		}
	}
	close(stop)
	<-done
	if b, _ := os.ReadFile(path); string(b) != "fk-299" {
		t.Fatalf("key file %q", b)
	}
}
