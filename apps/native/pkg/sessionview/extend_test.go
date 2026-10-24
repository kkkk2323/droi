package sessionview

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/kkkk2323/droid-sdk-go/fakedaemon"

	"github.com/kkkk2323/droi/apps/native/pkg/kit"
)

// A registered ToolView shows its tool's calls open, with its own content
// in place of the input and the result.
func TestARegisteredToolViewDrawsItsCalls(t *testing.T) {
	const name = "corp-db___query"
	RegisterToolView(name, ToolView{Icon: "list-filter", Open: true, Detail: func(c *ui.Context, tc ToolContext) {
		rows := strings.Count(tc.Result.Text, "\n") + 1
		tc.Kit.Text(c, "Rows: "+strings.Repeat("|", rows), 13, 20)
	}})
	t.Cleanup(func() {
		extendMu.Lock()
		delete(toolViews, name)
		extendMu.Unlock()
	})
	h := newHarness(t, fakedaemon.Scenario{Sessions: []fakedaemon.SessionSpec{{Title: "Orders", Cwd: "/Users/dev/acme-web", Messages: []fakedaemon.Message{
		{Role: "user", Text: "How many orders today?"},
		{Role: "assistant", Content: []map[string]any{
			{"type": "tool_use", "id": "call_q", "name": name, "input": map[string]any{"sql": "select-everything-from-orders"}},
		}},
		{Role: "tool", Content: []map[string]any{{"type": "tool_result", "toolUseId": "call_q", "content": "a\nb\nc"}}},
		{Role: "assistant", Text: "Three."},
	}}}})
	h.until("the custom panel", func() bool { return h.hasText("Rows: |||") })
	for _, s := range h.tt.Texts() {
		if s == "a\nb\nc" {
			t.Errorf("the plain result shows too: %q", h.tt.Texts())
		}
	}
}

// A json-render element of a registered type draws with its Component, and
// its children inside it.
func TestARegisteredComponentDrawsItsElements(t *testing.T) {
	RegisterComponent("CorpGauge", func(c *ui.Context, k *kit.Kit, props map[string]any, children func()) {
		label, _ := props["label"].(string)
		ui.Column(c).Label("Gauge " + label).Children(children)
	})
	t.Cleanup(func() {
		extendMu.Lock()
		delete(components, "CorpGauge")
		extendMu.Unlock()
	})
	spec := `{"root":"g","elements":{"g":{"type":"CorpGauge","props":{"label":"CPU"},"children":["t"]},"t":{"type":"Text","props":{"text":"72 percent"},"children":[]}}}`
	h := newHarness(t, replyScenario("Load:\n\n<json-render>"+spec+"</json-render>\n"))
	h.until("the gauge", func() bool { _, ok := h.tt.Find("Gauge CPU"); return ok })
	if !h.hasText("72 percent") {
		t.Errorf("the child does not show: %q", h.tt.Texts())
	}
}
