package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/kkkk2323/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/internal/slash"
)

// suggests types query into the focused composer and waits for the "/"
// suggestions to list every one of want; Tab then picks the first and the
// text becomes it. The query is cleared after.
func (h *harness) suggests(text func() string, query string, want ...string) {
	h.t.Helper()
	h.typeInto(query)
	h.until("the suggestions for "+query+" to list "+want[0], func() bool {
		list, ok := h.tt.Find("Commands and skills")
		if !ok || list.W == 0 {
			return false
		}
		for _, w := range want {
			if _, ok := h.tt.Find(w); !ok {
				return false
			}
		}
		return true
	})
	h.tt.Key(0, ui.KeyTab)
	h.frame()
	if got, picked := text(), want[0]+" "; got != picked {
		h.t.Fatalf("Tab left %q, want %q", got, picked)
	}
}

func (h *harness) newPageText() string { return h.a.newPage.text }

func (h *harness) clearNewPage() {
	h.a.newPage.text = ""
	h.frame()
}

// "/" offers every builtin where it runs: the Starters on the New session
// page, with a project and without one, and every builtin in a Session. A
// builtin added to one composer and not the other was lost there before;
// this goes through each of slash's lists, so a new builtin is checked too.
func TestSlashOffersTheBuiltinsWhereverTheyRun(t *testing.T) {
	h := newHarness(t, draftScenario(), "")
	h.until("the Session list", func() bool { return h.hasText("Fix the login race") })
	starts := map[string]bool{}
	for _, b := range slash.Starters {
		starts[b.Name] = true
	}
	// each types "/" and the start of every builtin's name in the
	// composer and checks what is offered.
	each := func(text func() string, clear func(), place string, offered func(slash.Item) bool) {
		for _, b := range slash.Builtins {
			if offered(b) {
				h.suggests(text, "/"+b.Name[:2], "/"+b.Name)
				clear()
				continue
			}
			h.typeInto("/" + b.Name[:2])
			h.settle()
			if _, ok := h.tt.Find("/" + b.Name); ok {
				t.Fatalf("/%s is offered %s", b.Name, place)
			}
			clear()
		}
	}
	isStarter := func(b slash.Item) bool { return starts[b.Name] }

	h.openStartPage()
	h.click("Message")
	each(h.newPageText, h.clearNewPage, "on the New session page", isStarter)
	// The project's own skills come with them.
	h.suggests(h.newPageText, "/", "/"+slash.Starters[0].Name, "/wait-what")
	h.clearNewPage()

	h.click("Workspace")
	h.click("Don't work in a project")
	h.until("no project", func() bool { return h.hasText("What should we work on?") })
	h.click("Message")
	each(h.newPageText, h.clearNewPage, "without a project", isStarter)

	h.openSession("Fix the login race")
	h.focusComposer()
	clearSession := func() {
		h.a.views[h.a.Route().SessionID].composer.text = ""
		h.frame()
	}
	each(h.sessionText, clearSession, "in a Session", func(slash.Item) bool { return true })
}

// A /loop sent from the New session page starts the Session and its loop,
// and sends nothing to the agent; one that cannot run stays on the page.
func TestTheNewSessionPageStartsALoop(t *testing.T) {
	h := newHarness(t, draftScenario(), "")
	h.until("the Session list", func() bool { return h.hasText("Fix the login race") })
	h.openStartPage()
	h.click("Message")

	h.typeInto("/loop 7m tidy")
	h.tt.Key(0, ui.KeyEnter)
	h.until("the interval error", func() bool { return h.hasText("A loop runs every 1, 2, 3") })
	if h.a.Route().Name != "new" || h.newPageText() != "/loop 7m tidy" {
		t.Fatalf("route %q, text %q", h.a.Route().Name, h.newPageText())
	}

	h.clearNewPage()
	h.typeInto("/loop 10m check the build")
	h.tt.Key(0, ui.KeyEnter)
	h.until("the Session", func() bool { return h.a.Route().Name == "session" })
	id := h.a.Route().SessionID
	p := h.sent(protocol.MethodCreateCron, func(map[string]any) bool { return true })
	scope, _ := p["scope"].(map[string]any)
	payload, _ := p["payload"].(map[string]any)
	if scope["sessionId"] != id || payload["prompt"] != "check the build" {
		t.Fatalf("create_cron %v", p)
	}
	h.until("the loop above the composer", func() bool { return h.hasText("check the build") && h.hasText("Every 10 minutes") })
	if n := h.count(protocol.MethodAddUserMessage); n != 0 {
		t.Fatalf("%d messages sent to the agent", n)
	}
}
