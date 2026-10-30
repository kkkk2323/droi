package app

import (
	"testing"

	"github.com/kkkk2323/droid-sdk-go/fakedaemon"

	"github.com/kkkk2323/droi/apps/native/pkg/prefs"
)

var pickerModels = []map[string]any{
	{"id": "auto", "displayName": "Auto Model", "shortDisplayName": "Auto Model", "modelProvider": "factory",
		"supportedReasoningEfforts": []string{"none"}, "defaultReasoningEffort": "none",
		"isCustom": false, "tokenMultiplier": 1, "kind": "router"},
	{"id": "gpt-6-1-sol", "displayName": "GPT-6.1-Sol", "shortDisplayName": "GPT-6.1-Sol", "modelProvider": "openai",
		"supportedReasoningEfforts": []string{"low", "high"}, "defaultReasoningEffort": "high",
		"isCustom": false, "tokenMultiplier": 1.6},
	{"id": "claude-opus-4-6", "displayName": "Claude Opus 4.6", "shortDisplayName": "Claude Opus 4.6", "modelProvider": "anthropic",
		"supportedReasoningEfforts": []string{"high"}, "defaultReasoningEffort": "high",
		"isCustom": false, "tokenMultiplier": 1},
}

// With models starred, the picker opens on them: they are the ones picked
// most, and the full list is a click away.
func TestTheModelPickerOpensOnTheFavorites(t *testing.T) {
	h := newHarness(t, fakedaemon.Scenario{
		Sessions: []fakedaemon.SessionSpec{{Title: "One", Cwd: "/Users/dev/acme-web",
			Messages: []fakedaemon.Message{{Role: "user", Text: "hi"}}}},
		Input: map[string]any{"models": pickerModels},
	}, "")
	h.openSession("One")
	h.until("the composer", func() bool { _, ok := h.tt.Find("Model and reasoning effort"); return ok })
	h.settle()

	h.click("Model and reasoning effort")
	h.until("the models", func() bool { _, ok := h.tt.Find("Models"); return ok })
	h.frame()
	if !h.hasText("Claude Opus 4.6") {
		t.Fatalf("with no favorites the picker opened on %q", h.tt.Texts())
	}
	h.click("Model and reasoning effort")
	h.settle()

	prefs.FavoriteModels.Set(h.a.prefs, []string{"claude-opus-4-6"})
	h.click("Model and reasoning effort")
	h.until("the models", func() bool { _, ok := h.tt.Find("Models"); return ok })
	h.frame()
	if !h.hasText("Claude Opus 4.6") || h.hasText("GPT-6.1-Sol") {
		t.Fatalf("the picker did not open on the favorites: %q", h.tt.Texts())
	}

	// A starred model the Daemon no longer offers leaves the favorites empty:
	// the picker opens on every model instead.
	prefs.FavoriteModels.Set(h.a.prefs, []string{"gone"})
	h.click("Model and reasoning effort")
	h.settle()
	h.click("Model and reasoning effort")
	h.until("the models", func() bool { _, ok := h.tt.Find("Models"); return ok })
	h.frame()
	if !h.hasText("Claude Opus 4.6") || !h.hasText("GPT-6.1-Sol") {
		t.Fatalf("with no favorite on offer the picker opened on %q", h.tt.Texts())
	}
}
