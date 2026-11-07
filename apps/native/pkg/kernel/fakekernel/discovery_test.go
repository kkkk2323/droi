package fakekernel

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// TestPlatformDir pins the mapping between Go's architecture names and the directory names
// `apps/kernel/build.ts` writes. They disagree on one and it is the common one — Go says amd64, the build
// says x64 — so without this every Intel machine found no binary and skipped in silence.
func TestPlatformDir(t *testing.T) {
	cases := []struct {
		goos, goarch, want string
		ok                 bool
	}{
		{"darwin", "arm64", "darwin-arm64", true},
		{"darwin", "amd64", "darwin-x64", true},
		{"windows", "amd64", "windows-x64", true},
		{"windows", "arm64", "windows-arm64", true},
		{"linux", "amd64", "linux-x64", true},
		{"linux", "arm64", "linux-arm64", true},
		// Nothing builds these, and guessing a directory would make a test look for a binary that cannot
		// exist instead of skipping.
		{"linux", "386", "", false},
		{"plan9", "amd64", "", false},
	}
	for _, test := range cases {
		got, ok := platformDir(test.goos, test.goarch)
		if got != test.want || ok != test.ok {
			t.Errorf("platformDir(%q, %q) = (%q, %v), want (%q, %v)",
				test.goos, test.goarch, got, ok, test.want, test.ok)
		}
	}
}

// TestFindBinaryStopsAtTheRoot is the Windows hang: the walk compared its absolute path to "/", which on
// Windows never matches, so it climbed for ever from `C:\` instead of answering "none".
//
// A temp directory far from any built Kernel is the fixture; the assertion is that this returns at all.
func TestFindBinaryStopsAtTheRoot(t *testing.T) {
	done := make(chan string, 1)
	go func() {
		done <- findBinary(t.TempDir(), runtime.GOOS, runtime.GOARCH)
	}()
	select {
	case found := <-done:
		if found != "" {
			t.Fatalf("found %q in an empty tree", found)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("findBinary did not return: the walk does not terminate at the filesystem root")
	}
}

// TestFindBinaryFindsTheBuild is the other half: a binary laid out where build.ts puts it is found from a
// nested directory, with the amd64→x64 mapping applied.
func TestFindBinaryFindsTheBuild(t *testing.T) {
	root := t.TempDir()
	platform, ok := platformDir(runtime.GOOS, runtime.GOARCH)
	if !ok {
		t.Skipf("no build for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	exe := "kernel"
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	target := filepath.Join(root, "assets", "kernel", platform, exe)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("not a real binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "apps", "native", "pkg", "kernel", "fakekernel")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if found := findBinary(nested, runtime.GOOS, runtime.GOARCH); found != target {
		t.Fatalf("findBinary from %s = %q, want %q", nested, found, target)
	}
}

// TestIsolatedEnvKeepsKeysOut pins the whitelist: a variable that names a key, or a FACTORY_ one, does not
// reach a test's Kernel, while PATH does.
func TestIsolatedEnvKeepsKeysOut(t *testing.T) {
	t.Setenv("FACTORY_API_KEY", "secret")
	t.Setenv("OPENAI_API_KEY", "secret")
	t.Setenv("DROI_HOME_OVERRIDE", "/somewhere/real")
	home := t.TempDir()
	env := isolatedEnv(home)
	joined := ""
	for _, entry := range env {
		joined += entry + "\n"
	}
	for _, leaked := range []string{"FACTORY_API_KEY", "OPENAI_API_KEY", "DROI_HOME_OVERRIDE"} {
		if containsLine(joined, leaked+"=") {
			t.Errorf("%s reached the Kernel's environment", leaked)
		}
	}
	if !containsLine(joined, "HOME="+home) {
		t.Errorf("HOME is not the temporary directory:\n%s", joined)
	}
	if runtime.GOOS == "windows" && !containsLine(joined, "USERPROFILE="+home) {
		t.Errorf("USERPROFILE is not the temporary directory:\n%s", joined)
	}
}

func containsLine(haystack, needle string) bool {
	for _, line := range splitLines(haystack) {
		if len(line) >= len(needle) && line[:len(needle)] == needle {
			return true
		}
	}
	return false
}

func splitLines(text string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(text); i++ {
		if text[i] != '\n' {
			continue
		}
		lines = append(lines, text[start:i])
		start = i + 1
	}
	return lines
}
