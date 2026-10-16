package app

import (
	"encoding/json"
	"testing"

	"github.com/egoist/mygo/ui"
)

// openSessionDefaults opens Settings on the Session defaults page.
func (h *harness) openSessionDefaults() {
	h.t.Helper()
	// Tall enough for the whole page, so every row and its popover is in view.
	h.tt.SetSize(refW, 2000)
	h.until("the Session list", func() bool { return h.hasText("Fix the login race") })
	h.click("Settings")
	h.click("Session defaults")
	h.until("the defaults", func() bool { return h.hasText("Limits for specific models") })
}

// defaultsPatchSent is the nth update_session_defaults the Daemon got.
func (h *harness) defaultsPatchSent(n int) map[string]any {
	h.t.Helper()
	req, err := h.d.WaitForRequest("daemon.update_session_defaults", n)
	if err != nil {
		h.t.Fatal(err)
	}
	var p map[string]any
	_ = json.Unmarshal(req.Params, &p)
	return p
}

// A model can have a compaction limit of its own, changed and removed again.
func TestSessionDefaultsSetsACompactionLimitForAModel(t *testing.T) {
	h := newHarness(t, sessionsScenario(), "")
	h.openSessionDefaults()
	h.click("Add a model limit")
	h.until("the model list", func() bool { _, ok := h.tt.Find("Choose a model"); return ok })
	h.click("GPT-5")
	p := h.defaultsPatchSent(1)
	if got, _ := json.Marshal(p["compactionTokenLimitPerModel"]); string(got) != `{"gpt-5":250000}` {
		t.Fatalf("update_session_defaults %v", p)
	}
	h.until("the GPT-5 row", func() bool { _, ok := h.tt.Find("GPT-5 compaction limit"); return ok })
	h.click("GPT-5 compaction limit")
	h.click("1M")
	p = h.defaultsPatchSent(2)
	if got, _ := json.Marshal(p["compactionTokenLimitPerModel"]); string(got) != `{"gpt-5":1000000}` {
		t.Fatalf("update_session_defaults %v", p)
	}
	h.until("the GPT-5 limit to show", func() bool { return h.hasText("1M") })
	h.click("Remove the GPT-5 limit")
	p = h.defaultsPatchSent(3)
	if got, _ := json.Marshal(p["compactionTokenLimitPerModel"]); string(got) != `{}` {
		t.Fatalf("update_session_defaults %v", p)
	}
	h.until("the row to go", func() bool { _, ok := h.tt.Find("GPT-5 compaction limit"); return !ok })
}

// The spec folder is the user's, the project's or one picked.
func TestSessionDefaultsSavesTheSpecFolder(t *testing.T) {
	h := newHarnessWith(t, sessionsScenario(), "", func(c *Config) {
		c.PickFolder = func(string) string { return "/Users/dev/specs" }
	})
	h.openSessionDefaults()
	if !h.hasText("Specs go to ~/.factory/docs.") {
		t.Fatalf("the spec folder row is missing: %q", h.tt.Texts())
	}
	h.click("Spec save folder")
	h.click("Project")
	if p := h.defaultsPatchSent(1); p["specSaveDir"] != ".factory/docs" {
		t.Fatalf("update_session_defaults %v", p)
	}
	h.until("the project folder", func() bool { return h.hasText("Specs go to the project's .factory/docs") })
	h.click("Spec save folder")
	h.click("Custom…")
	// The folder comes back off the UI thread and saves from the window's
	// queue, which only frames run.
	h.until("the custom folder", func() bool { return h.hasText("Custom: /Users/dev/specs") })
	if p := h.defaultsPatchSent(2); p["specSaveDir"] != "/Users/dev/specs" {
		t.Fatalf("update_session_defaults %v", p)
	}
}

// Each Subagent tier can have its own model and reasoning level.
func TestSessionDefaultsSetsASubagentTierModel(t *testing.T) {
	h := newHarness(t, sessionsScenario(), "")
	h.openSessionDefaults()
	if _, ok := h.tt.Find("Heavy task reasoning level"); ok {
		t.Fatal("an inherited tier offers a reasoning level")
	}
	h.click("Heavy task model")
	h.click("GPT-5")
	if got, _ := json.Marshal(h.defaultsPatchSent(1)["subagentModelSettings"]); string(got) != `{"heavyModel":"gpt-5"}` {
		t.Fatalf("subagentModelSettings %s", got)
	}
	h.until("the tier's reasoning level", func() bool { _, ok := h.tt.Find("Heavy task reasoning level"); return ok })
	h.click("Heavy task reasoning level")
	h.click("Extra high")
	if got, _ := json.Marshal(h.defaultsPatchSent(2)["subagentModelSettings"]); string(got) != `{"heavyModel":"gpt-5","heavyReasoningEffort":"xhigh"}` {
		t.Fatalf("subagentModelSettings %s", got)
	}
	h.click("Heavy task model")
	h.until("the model list", func() bool { _, ok := h.tt.Find("Choose a model"); return ok })
	// The list starts with the fallback, highlighted.
	h.tt.Key(0, ui.KeyEnter)
	h.frame()
	if got, _ := json.Marshal(h.defaultsPatchSent(3)["subagentModelSettings"]); string(got) != `{}` {
		t.Fatalf("subagentModelSettings %s", got)
	}
}

// The spec mode and summary models are picked in the model picker, which
// starts with the choice of no model of their own.
func TestSessionDefaultsPicksSpecAndSummaryModels(t *testing.T) {
	h := newHarness(t, sessionsScenario(), "")
	h.openSessionDefaults()
	h.click("Spec mode model")
	h.until("the model list", func() bool { _, ok := h.tt.Find("Choose a model"); return ok })
	h.click("GPT-5")
	if p := h.defaultsPatchSent(1); p["specModeModelId"] != "gpt-5" {
		t.Fatalf("update_session_defaults %v", p)
	}
	h.click("Compaction model")
	h.until("the model list", func() bool { _, ok := h.tt.Find("Choose a model"); return ok })
	h.tt.Type("gpt-5")
	h.tt.Key(0, ui.KeyEnter)
	h.frame()
	if p := h.defaultsPatchSent(2); p["compactionModel"] != "gpt-5" {
		t.Fatalf("update_session_defaults %v", p)
	}
	h.click("Compaction model")
	h.until("the model list", func() bool { _, ok := h.tt.Find("Choose a model"); return ok })
	h.click("Current model")
	if p := h.defaultsPatchSent(3); p["compactionModel"] != "current-model" {
		t.Fatalf("update_session_defaults %v", p)
	}
}
