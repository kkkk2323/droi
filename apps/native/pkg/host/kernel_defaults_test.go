package host

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// kernelHostFor is a Host in throwaway folders that runs a Kernel; the Kernel binary is an empty file, so it never
// answers.
func kernelHostFor(t *testing.T, home string) *Host {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "kernel")
	if err := os.WriteFile(bin, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"HOME": home, KernelEnv: "1", KernelBinEnv: bin}
	return New(Config{UserData: t.TempDir(), Home: home, Env: func(k string) string { return env[k] }})
}

// A Host that runs a Kernel runs no Daemon: nothing on the Kernel uses one, and a restart a setting asks for starts
// none either.
func TestAKernelHostRunsNoDaemon(t *testing.T) {
	h := kernelHostFor(t, t.TempDir())
	h.Start()
	t.Cleanup(h.Stop)
	h.RestartDaemon()
	time.Sleep(300 * time.Millisecond)
	if st := h.Daemon.State().Status; st != DaemonStopped && st != "" {
		t.Fatalf("the Daemon is %s on a Kernel Host", st)
	}
}

// The Kernel's Session defaults are Droi's own. The first read takes droid's (~/.factory/settings.json), so a Droi
// that stops running droid starts where it left off; a change is kept in Droi's settings and never in droid's.
func TestKernelSessionDefaultsStartFromDroidsAndStayDrois(t *testing.T) {
	home := t.TempDir()
	droids := filepath.Join(home, ".factory", "settings.json")
	if err := os.MkdirAll(filepath.Dir(droids), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"sessionDefaultSettings": {"model": "claude-opus-5-5", "reasoningEffort": "high", "autonomyLevel": "medium"}}`
	if err := os.WriteFile(droids, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	h := kernelHostFor(t, home)
	want := SessionDefaults{Model: "claude-opus-5-5", ReasoningEffort: "high", AutonomyLevel: "medium"}
	if got := h.KernelSessionDefaults(); got != want {
		t.Fatalf("the defaults are %+v, want droid's %+v", got, want)
	}
	if err := h.SetKernelSessionDefaults(func(d *SessionDefaults) { d.AutonomyLevel = "high" }); err != nil {
		t.Fatal(err)
	}
	want.AutonomyLevel = "high"
	if got := h.KernelSessionDefaults(); got != want {
		t.Fatalf("after the change: %+v, want %+v", got, want)
	}
	if b, _ := os.ReadFile(droids); string(b) != body {
		t.Fatalf("droid's settings changed: %s", b)
	}
	// Taken once: droid's later changes are droid's.
	if err := os.WriteFile(droids, []byte(`{"sessionDefaultSettings": {"model": "gpt-6"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := h.KernelSessionDefaults(); got != want {
		t.Fatalf("droid's later change came over: %+v", got)
	}
}

// With no droid settings, the defaults are empty, and the Kernel's own choices apply.
func TestKernelSessionDefaultsWithoutDroid(t *testing.T) {
	h := kernelHostFor(t, t.TempDir())
	if got := h.KernelSessionDefaults(); got != (SessionDefaults{}) {
		t.Fatalf("the defaults are %+v", got)
	}
}
