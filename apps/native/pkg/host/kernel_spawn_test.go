package host

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestKernelSpawnRunsTheRealBinary exercises the production spawn: no Spawn
// hook, a real child process built from testdata/fakekernel, the contract's
// arguments, and its token in the environment only.
func TestKernelSpawnRunsTheRealBinary(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a helper binary")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go toolchain to build the helper")
	}
	dir := t.TempDir()
	binary := filepath.Join(dir, "fakekernel")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.Command("go", "build", "-o", binary, "./testdata/fakekernel")
	build.Env = os.Environ()
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build the helper: %v\n%s", err, out)
	}
	data := t.TempDir()
	record := filepath.Join(dir, "report.json")
	t.Setenv("FAKE_KERNEL_RECORD", record)

	w := newWatch()
	s := w.supervisor(nil)
	s.Binary = binary
	s.DataDir = data
	s.ReadyTimeout = 30 * time.Second
	s.HealthEvery = -1
	s.Start()
	running := w.waitState(t, KernelRunning)
	if running.Port == 0 {
		t.Fatalf("running state has no port: %+v", running)
	}
	if _, err := os.Stat(record); err != nil {
		s.Stop()
		t.Fatalf("the sidecar wrote no report: %v", err)
	}
	raw, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	var report struct {
		Argv          []string `json:"argv"`
		TokenInArgv   bool     `json:"tokenInArgv"`
		TokenInEnv    bool     `json:"tokenInEnv"`
		Cwd           string   `json:"cwd"`
		DataDir       string   `json:"dataDir"`
		DataDirInArgv bool     `json:"dataDirInArgv"`
	}
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatal(err)
	}
	token := s.Token()
	s.Stop()

	if report.TokenInArgv {
		t.Error("the token was in argv, which ps shows to every user")
	}
	if !report.TokenInEnv {
		t.Error("the token was not in DROI_KERNEL_TOKEN")
	}
	if token == "" || strings.Contains(strings.Join(report.Argv, " "), token) {
		t.Errorf("Token() = %q and argv = %v", token, report.Argv)
	}
	if !report.DataDirInArgv {
		t.Errorf("--data %q is not in argv %v", data, report.Argv)
	}
	// The contract's arguments, and only them.
	want := []string{"--port", "--host", "--data"}
	seen := map[string]bool{}
	for _, arg := range report.Argv {
		if strings.HasPrefix(arg, "--") {
			seen[arg] = true
		}
	}
	for _, flag := range want {
		if !seen[flag] {
			t.Errorf("argv %v has no %s", report.Argv, flag)
		}
	}
	if seen["--token"] {
		t.Errorf("argv %v carries a token flag", report.Argv)
	}
	// No working directory: a child whose cwd is the data directory holds a
	// handle to it on Windows, and fails to start when it is missing.
	if sameDir(report.Cwd, data) {
		t.Errorf("the sidecar runs with its cwd in the data directory (%s)", report.Cwd)
	}
}

// sameDir reports whether two paths name the same directory.
func sameDir(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	fa, err1 := os.Stat(a)
	fb, err2 := os.Stat(b)
	if err1 != nil || err2 != nil {
		return a == b
	}
	return os.SameFile(fa, fb)
}
