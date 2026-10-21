package app

import (
	"testing"

	"github.com/kkkk2323/droid-sdk-go/fakedaemon"
)

// magpieModels are what the Daemon reports when the Host hands it both its
// own models and a Magpie gateway's: the gateway's are Custom Models whose ids
// start "custom:magpie-", and their names repeat the Daemon's.
var magpieModels = []map[string]any{
	{"id": "auto", "displayName": "Auto Model", "shortDisplayName": "Auto Model", "modelProvider": "factory",
		"supportedReasoningEfforts": []string{"none"}, "defaultReasoningEffort": "none",
		"isCustom": false, "tokenMultiplier": 1, "kind": "router"},
	{"id": "gpt-6-1-sol", "displayName": "GPT-6.1-Sol", "shortDisplayName": "GPT-6.1-Sol", "modelProvider": "openai",
		"supportedReasoningEfforts": []string{"low", "high"}, "defaultReasoningEffort": "high",
		"isCustom": false, "tokenMultiplier": 1.6},
	{"id": "custom:magpie-codex-gpt-6-1-sol-0a1b2c", "displayName": "Magpie · GPT-6.1-Sol", "shortDisplayName": "Magpie · GPT-6.1-Sol",
		"modelProvider": "generic-chat-completion-api", "supportedReasoningEfforts": []string{"low", "high"},
		"defaultReasoningEffort": "high", "isCustom": true, "tokenMultiplier": 1},
	{"id": "custom:magpie-workbuddy-glm-5-3-4d5e6f", "displayName": "Magpie · GLM 5.3", "shortDisplayName": "Magpie · GLM 5.3",
		"modelProvider": "generic-chat-completion-api", "supportedReasoningEfforts": []string{"high"},
		"defaultReasoningEffort": "high", "isCustom": true, "tokenMultiplier": 1},
}

// The picker keeps a gateway's models in a tab of its own: beside the
// Daemon's models of the same name the list reads as one crowd, and the
// brands' tabs show only what the Daemon itself offers.
func TestMagpieModelsHaveTheirOwnTabInTheModelPicker(t *testing.T) {
	h := newHarness(t, fakedaemon.Scenario{
		Sessions: []fakedaemon.SessionSpec{{Title: "One", Cwd: "/Users/dev/acme-web",
			Messages: []fakedaemon.Message{{Role: "user", Text: "hi"}}}},
		Input: map[string]any{"models": magpieModels},
	}, "")
	h.openSession("One")
	h.until("the composer", func() bool { _, ok := h.tt.Find("Model and reasoning effort"); return ok })
	h.settle()

	h.click("Model and reasoning effort")
	h.until("the models", func() bool { _, ok := h.tt.Find("Models"); return ok })
	h.frame()
	if !h.hasText("Magpie") {
		t.Fatalf("no Magpie tab: %q", h.tt.Texts())
	}
	// The Daemon's own models, without the gateway's duplicates.
	if h.hasText("Magpie · GPT-6.1-Sol") || h.hasText("Magpie · GLM 5.3") {
		t.Fatalf("the gateway's models sit among the Daemon's: %q", h.tt.Texts())
	}

	h.click("Magpie")
	h.frame()
	if !h.hasText("Magpie · GPT-6.1-Sol") || !h.hasText("Magpie · GLM 5.3") {
		t.Fatalf("the Magpie tab lists %q", h.tt.Texts())
	}
	if _, ok := h.tt.Find("GPT-6.1-Sol"); ok {
		t.Fatalf("the Magpie tab lists the Daemon's model too: %q", h.tt.Texts())
	}

	// A brand's tab holds the Daemon's model only.
	if _, ok := h.tt.Find("OpenAI"); ok {
		h.click("OpenAI")
		h.frame()
		if h.hasText("Magpie · GPT-6.1-Sol") {
			t.Fatalf("the OpenAI tab lists the gateway's model: %q", h.tt.Texts())
		}
	}

	// Picking one opens the picker on the Magpie tab again, since the tab it
	// opens on otherwise never lists the model in use.
	h.click("Magpie")
	h.frame()
	h.click("Magpie · GPT-6.1-Sol")
	h.until("the Session on the gateway's model", func() bool {
		return h.hasText("Magpie · GPT-6.1-Sol")
	})
	h.click("Model and reasoning effort")
	h.until("the model list", func() bool { _, ok := h.tt.Find("Models"); return ok })
	h.frame()
	if !h.hasText("Magpie · GLM 5.3") {
		t.Fatalf("the picker of a Session on a Magpie model opened on %q", h.tt.Texts())
	}
}
