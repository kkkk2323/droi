package app

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/v2/pkg/defaults"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel/fakekernel"
)

// A script the model runs on Kernel is a Script card, and the calls it made hang under it with what they answered.
func TestAKernelScriptShowsTheCallsItMade(t *testing.T) {
	scriptKernel(t, func(sc *fakekernel.Scenario) {
		sc.Steps = append([]fakekernel.Step{
			{Match: "Read the notes in a script", Call: &fakekernel.Call{Name: "codemode", ID: "call_cm",
				Arguments: map[string]any{"code": `const notes = await tools.read({ path: "notes.txt" }); return notes.length`}}},
			{Match: "Read the notes in a script", Text: "The notes are short."},
		}, sc.Steps...)
	})
	h := kernelHarness(t)
	conv, _ := h.kernel.id(h.d.Sessions[1].SessionID)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("ship on friday\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	if _, err := h.testConn().Configure(ctx, kernel.ConfigureParams{ID: conv, Change: &kernel.AgentChange{Cwd: kernel.Some(dir)}}); err != nil {
		t.Fatal(err)
	}
	h.openSession("Add dark mode toggle")
	h.send("Read the notes in a script")
	// The Session defaults' level asks first for the script, then for nothing: read is a read.
	h.until("the approval card", func() bool { return h.hasText("Droi wants to run codemode") })
	h.click("Allow once")
	h.until("the script and its call", func() bool {
		return h.hasText("The notes are short.") && h.hasText("Used read") && h.hasButton("Script: read")
	})
	h.click("Script: read")
	h.until("the script's source", func() bool { return h.hasText("tools.read") })
}

// Direct, picked on the New session page, starts a Kernel conversation without scripts; the default keeps them.
func TestANewKernelSessionPickedDirectHasNoScripts(t *testing.T) {
	h := kernelHarness(t)
	for _, mode := range []defaults.ToolMode{defaults.DirectOnly, ""} {
		dir := t.TempDir()
		h.openStartPage()
		h.a.newPage.pick, h.a.newPage.picked = dir, true
		h.a.newPage.toolMode = mode
		h.frame()
		h.start("Plan the week " + string(mode))
		var conv kernel.ID
		h.until("the conversation", func() bool {
			for _, s := range h.a.listedNow() {
				if id, ok := h.a.kernelID(s.SessionID); ok && s.Cwd == dir {
					conv = id
					return true
				}
			}
			return false
		})
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		agent, err := h.testConn().Configure(ctx, kernel.ConfigureParams{ID: conv, Change: &kernel.AgentChange{}})
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		if has := slices.Contains(agent.Agent.Tools, "codemode"); has == (mode == defaults.DirectOnly) {
			t.Errorf("mode %q: codemode offered %v, tools %v", mode, has, agent.Agent.Tools)
		}
	}
}
