package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kkkk2323/droi/apps/native/v2/pkg/host"
)

// kernelHostWithFactory is a Host that runs a Kernel, in throwaway folders whose ~/.factory has a skill and an
// AGENTS.md, and whose ~/.droi already has an AGENTS.md of its own. The Kernel is never started.
func kernelHostWithFactory(t *testing.T) (*host.Host, string) {
	t.Helper()
	home := t.TempDir()
	put := func(path, body string) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	put(filepath.Join(home, ".factory", "skills", "gofmt", "SKILL.md"), "gofmt\n")
	put(filepath.Join(home, ".factory", "AGENTS.md"), "droid's\n")
	put(filepath.Join(home, ".droi", "AGENTS.md"), "Droi's\n")
	bin := filepath.Join(t.TempDir(), "kernel")
	put(bin, "")
	env := map[string]string{"HOME": home, host.KernelEnv: "1", host.KernelBinEnv: bin}
	return host.New(host.Config{UserData: t.TempDir(), Home: home, Env: func(k string) string { return env[k] }}), home
}

// Settings → Advanced imports from ~/.factory again: what Droi lacks comes over, what differs is named and kept, and
// taking droid's copy moves Droi's aside.
func TestSettingsImportsFromDroidAgain(t *testing.T) {
	h0, home := kernelHostWithFactory(t)
	h := newHarnessWith(t, oneSession(), "", func(cfg *Config) { cfg.Host = h0 })
	h.until("the Session list", func() bool { return h.hasText("One") })
	h.openSettingsTab("advanced")
	h.until("the import", func() bool { return h.hasText("Import from droid") })
	h.click("Import from ~/.factory")
	h.until("the import's answer", func() bool { return h.hasText("Imported 1 skill.") })
	if !h.hasText("Different in ~/.factory, and Droi's kept: AGENTS.md") {
		t.Fatal("the AGENTS.md that differs is not named")
	}
	if got, _ := os.ReadFile(filepath.Join(home, ".droi", "skills", "gofmt", "SKILL.md")); string(got) != "gofmt\n" {
		t.Fatalf("the skill did not come over: %q", got)
	}
	h.click("Use droid's copies")
	h.until("the replace's answer", func() bool { return h.hasText("Droi's own copies were moved to") })
	if got, _ := os.ReadFile(filepath.Join(home, ".droi", "AGENTS.md")); string(got) != "droid's\n" {
		t.Fatalf("AGENTS.md is %q after taking droid's", got)
	}
}

// Without a Kernel, nothing reads Droi's own folder, and Settings does not offer the import.
func TestSettingsOffersNoImportWithoutAKernel(t *testing.T) {
	h := newHarnessWith(t, oneSession(), "", func(cfg *Config) { cfg.Host = testHost(t) })
	h.openSettingsTab("advanced")
	h.until("Settings", func() bool { return h.hasText("Offer Magpie's models") })
	if h.hasText("Import from droid") {
		t.Fatal("Settings offers the import without a Kernel")
	}
}
