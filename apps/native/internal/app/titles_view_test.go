package app

import (
	"testing"
)

// A Session started on Kernel is named once its first round ends: the title the model gave (the Fake Kernel's
// model answers "Fake title"), in place of its first line.
func TestANewKernelSessionIsNamedAfterItsFirstRound(t *testing.T) {
	h := kernelHarness(t)
	dir := t.TempDir()
	h.openStartPage()
	h.a.newPage.pick, h.a.newPage.picked = dir, true
	h.frame()
	h.start("Write the release notes for 2.0")
	h.until("the generated title", func() bool { return h.hasText("Fake title") })
	if n := len(h.kernel.k.Completions()); n != 1 {
		t.Errorf("%d model.complete calls, want 1", n)
	}
}

// A title somebody chose stays: a turn in a Session the scenario named does not rename it.
func TestATurnLeavesAChosenTitleAlone(t *testing.T) {
	h := kernelHarness(t)
	h.openSession("Add dark mode toggle")
	h.send("Go on")
	h.sentText("Go on")
	h.until("the turn to end", func() bool {
		h.a.mu.Lock()
		defer h.a.mu.Unlock()
		return h.a.busy[h.d.Sessions[1].SessionID] == "idle"
	})
	h.settle()
	if !h.hasText("Add dark mode toggle") || h.hasText("Fake title") {
		t.Fatalf("the chosen title was replaced: %q", h.tt.Texts())
	}
	if n := len(h.kernel.k.Completions()); n != 0 {
		t.Errorf("%d model.complete calls for a named Session", n)
	}
}
