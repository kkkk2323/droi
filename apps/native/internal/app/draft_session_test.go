package app

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/kkkk2323/droid-sdk-go/fakedaemon"

	"github.com/kkkk2323/droi/apps/native/pkg/gateway"
)

func draftScenario() fakedaemon.Scenario {
	sc := sessionsScenario()
	sc.Turn = &fakedaemon.Turn{Kind: "reply", Deltas: []string{"On it."}}
	sc.Input = map[string]any{"skills": []map[string]any{{"name": "wait-what", "description": "Re-pitch the last message"}}}
	return sc
}

func (h *harness) count(method string) int {
	reqs, _ := h.d.Requests()
	n := 0
	for _, r := range reqs {
		if r.Method == method {
			n++
		}
	}
	return n
}

// The New session page opens a Draft Session for its Workspace, so "/"
// offers that Workspace's skills before anything is sent; the first send
// takes the draft over instead of creating another Session.
func TestSlashOnTheNewSessionPageSuggestsTheWorkspacesSkills(t *testing.T) {
	h := newHarness(t, draftScenario(), "")
	h.until("the Session list", func() bool { return h.hasText("Fix the login race") })
	h.click("New session")
	h.until("the start button", func() bool { _, ok := h.tt.Find("Start session"); return ok })
	h.click("Message")
	h.typeInto("/wa")
	h.until("the skill in the suggestions", func() bool { return h.hasText("wait-what") })
	h.tt.Key(0, ui.KeyTab)
	h.frame()
	h.typeInto("go")
	if got := h.a.newPage.text; got != "/wait-what go" {
		t.Fatalf("text after the pick: %q", got)
	}
	h.tt.Key(0, ui.KeyEnter)
	h.until("the Session", func() bool { return h.a.Route().Name == "session" })
	id := h.a.Route().SessionID
	if n := h.count("daemon.initialize_session"); n != 1 {
		t.Fatalf("%d Sessions created, want the draft alone", n)
	}
	req, err := h.d.WaitForRequest("daemon.update_session_settings", 1)
	if err != nil {
		t.Fatal(err)
	}
	var p struct {
		SessionID string            `json:"sessionId"`
		Tags      *[]map[string]any `json:"tags"`
	}
	_ = json.Unmarshal(req.Params, &p)
	if p.SessionID != id || p.Tags == nil || len(*p.Tags) != 0 {
		t.Fatalf("the take sent %s", req.Params)
	}
	h.until("the Session listed", func() bool {
		for _, s := range h.a.listed {
			if s.SessionID == id {
				return true
			}
		}
		return false
	})
}

// Leaving the New session page without sending closes its draft.
func TestLeavingTheNewSessionPageClosesItsDraft(t *testing.T) {
	h := newHarness(t, draftScenario(), "")
	h.until("the Session list", func() bool { return h.hasText("Fix the login race") })
	h.click("New session")
	h.until("the draft", func() bool { return h.count("daemon.list_skills") > 0 })
	h.click("Fix the login race")
	if _, err := h.d.WaitForRequest("daemon.close_session", 1); err != nil {
		t.Fatal(err)
	}
}

// trashedFolder is a madeFolder whose Trash is a list.
type trashedFolder struct {
	madeFolder
	mu      sync.Mutex
	trashed []string
}

func (f *trashedFolder) Trash(path string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.trashed = append(f.trashed, path)
	return nil
}

func (f *trashedFolder) moved() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.trashed)
}

// scratchDraftHarness is a harness whose Gateway makes one Scratch folder,
// which the Fake Daemon takes for a valid directory.
func scratchDraftHarness(t *testing.T) (*harness, *trashedFolder) {
	t.Helper()
	folder := &trashedFolder{madeFolder: madeFolder(filepath.Join(t.TempDir(), "chats", "2026-10-18-0a1b2c"))}
	sc := draftScenario()
	sc.ValidDirectories = []string{string(folder.madeFolder)}
	h := startHarnessGateway(t, sc, "", "", func(o *gateway.Options) { o.Scratch = folder }, nil)
	h.until("the Session list", func() bool { return h.hasText("Fix the login race") })
	return h, folder
}

// Without a project the draft works in a Scratch folder of its own, so "/"
// offers the personal and built-in skills; the first send takes the draft,
// and the Session keeps that folder with the Scratch tag.
func TestTheFirstSendWithoutAProjectTakesTheScratchDraft(t *testing.T) {
	h, folder := scratchDraftHarness(t)
	made := string(folder.madeFolder)
	h.a.Go(Route{Name: "new", Scratch: true})
	h.until("the start button", func() bool { _, ok := h.tt.Find("Start session"); return ok })
	h.click("Message")
	h.typeInto("/wa")
	h.until("the skill in the suggestions", func() bool { return h.hasText("wait-what") })
	h.clearNewPage()
	h.typeInto("hi")
	h.click("Start session")
	h.until("the Session", func() bool { return h.a.Route().Name == "session" })
	id := h.a.Route().SessionID

	var inScratch int
	for _, params := range initializeRequests(t, h) {
		var p struct {
			Cwd string `json:"cwd"`
		}
		_ = json.Unmarshal([]byte(params), &p)
		if p.Cwd == made {
			inScratch++
		}
	}
	if inScratch != 1 {
		t.Fatalf("%d Sessions created in %s, want the draft alone", inScratch, made)
	}
	req, err := h.d.WaitForRequest("daemon.update_session_settings", 1)
	if err != nil {
		t.Fatal(err)
	}
	var p struct {
		SessionID string           `json:"sessionId"`
		Tags      []map[string]any `json:"tags"`
	}
	_ = json.Unmarshal(req.Params, &p)
	if p.SessionID != id || len(p.Tags) != 1 || p.Tags[0]["name"] != "droi.scratch" {
		t.Fatalf("the take sent %s", req.Params)
	}
	if got := folder.moved(); len(got) != 0 {
		t.Fatalf("the Session's folder was trashed: %v", got)
	}
}

// Leaving the page without sending trashes the Scratch draft's folder too.
func TestLeavingTheNewSessionPageTrashesTheScratchDraftsFolder(t *testing.T) {
	h, folder := scratchDraftHarness(t)
	h.a.Go(Route{Name: "new", Scratch: true})
	h.until("the draft", func() bool { return h.count("daemon.list_skills") > 0 })
	h.click("Fix the login race")
	h.until("the folder trashed", func() bool { return slices.Contains(folder.moved(), string(folder.madeFolder)) })
	if _, err := h.d.WaitForRequest("daemon.close_session", 1); err != nil {
		t.Fatal(err)
	}
}
