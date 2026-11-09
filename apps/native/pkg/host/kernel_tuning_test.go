package host

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel"
)

// A context window set for a model takes the place of its source's, which stays known for Settings; an output limit
// larger than the window set comes down to it, and the other models keep theirs.
func TestAContextWindowSetTakesThePlaceOfTheSources(t *testing.T) {
	providers := []kernelProvider{{ID: "p", Models: []kernelModel{
		{ID: "big", Name: "Big", ContextWindow: 1_000_000, MaxTokens: 128_000},
		{ID: "other", ContextWindow: 200_000, MaxTokens: 64_000},
	}}}
	got, listed := withContextWindows(providers, map[string]int{ModelKey("p", "big"): 100_000})
	if m := got[0].Models[0]; m.ContextWindow != 100_000 || m.MaxTokens != 100_000 {
		t.Errorf("big is %d/%d", m.ContextWindow, m.MaxTokens)
	}
	if m := got[0].Models[1]; m.ContextWindow != 200_000 || m.MaxTokens != 64_000 {
		t.Errorf("other is %d/%d", m.ContextWindow, m.MaxTokens)
	}
	if len(listed) != 2 || listed[0].Listed != 1_000_000 || listed[0].Name != "Big" {
		t.Errorf("listed %+v", listed)
	}
}

// Codemode's mode reaches a Kernel at launch, on unless Settings says only.
func TestTheKernelIsToldCodemodesMode(t *testing.T) {
	h := testHost(t)
	t.Cleanup(h.KeyPool().Stop)
	mode := func() string {
		for _, kv := range h.kernelEnv() {
			if v, ok := strings.CutPrefix(kv, kernel.CodemodeEnv+"="); ok {
				return v
			}
		}
		return ""
	}
	if got := mode(); got != kernel.CodemodeOn {
		t.Fatalf("by default the Kernel got %q", got)
	}
	if err := h.Settings.Update(func(s *Settings) { s.CodemodeMode = kernel.CodemodeOnly }); err != nil {
		t.Fatal(err)
	}
	if got := mode(); got != kernel.CodemodeOnly {
		t.Fatalf("with only set the Kernel got %q", got)
	}
}

// Codemode's mode and a model's context window, changed in Settings, reach the running Kernel without a restart:
// harness.info reports the window, and in only the model's request declares no bash of its own.
func TestCodemodeAndContextWindowsReachTheRunningKernel(t *testing.T) {
	f := newFakeFactory(t)
	hk := startHostKernel(t, f, true)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	c := hk.next(t, ctx)
	first := hk.generation()
	chat, _ := offered(t, ctx, c, "factory-chat", "deepseek-v4.1-flash-1017")
	answer(t, ctx, c, chat)
	const declared = `"name":"bash"`
	if !strings.Contains(f.lastBody(), declared) {
		t.Fatal("in on, the request declares no bash")
	}
	if err := hk.h.SetKernelCodemode(kernel.CodemodeOnly); err != nil {
		t.Fatal(err)
	}
	harnessChanged(t, ctx, c)
	hk.stillRunning(t, first)
	answer(t, ctx, c, chat)
	if body := f.lastBody(); strings.Contains(body, declared) || !strings.Contains(body, `"name":"codemode"`) {
		t.Fatal("in only, the request still declares bash, or not codemode")
	}

	if err := hk.h.SetModelContextWindow(chat.Provider, chat.ModelID, 64_000); err != nil {
		t.Fatal(err)
	}
	harnessChanged(t, ctx, c)
	hk.stillRunning(t, first)
	info, err := c.Harness(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range info.Models {
		if m.Provider == chat.Provider && m.ID == chat.ModelID && m.ContextWindow != 64_000 {
			t.Fatalf("the Kernel's window is %d", m.ContextWindow)
		}
	}
	var listed int
	for _, m := range hk.h.KernelModelContexts() {
		if m.Provider == chat.Provider && m.ID == chat.ModelID {
			listed = m.Listed
			if m.Set != 64_000 {
				t.Fatalf("Settings sets %d", m.Set)
			}
		}
	}
	if listed == 0 || listed == 64_000 {
		t.Fatalf("the source's window is %d", listed)
	}
}
