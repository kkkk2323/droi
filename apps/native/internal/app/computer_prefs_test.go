package app

import (
	"testing"

	"github.com/kkkk2323/droid-sdk-go/fakedaemon"

	"github.com/kkkk2323/droi/apps/native/pkg/host"
	"github.com/kkkk2323/droi/apps/native/pkg/prefs"
)

// Both computers have Workspaces at the same two paths, as a Mac and a box
// that share a project layout do.
func sharedPathSessions(prefix string) fakedaemon.Scenario {
	user := []fakedaemon.Message{{Role: "user", Text: "hi"}}
	return fakedaemon.Scenario{Sessions: []fakedaemon.SessionSpec{
		{Title: prefix + " first task", Cwd: "/work/alpha", Messages: user},
		{Title: prefix + " second task", Cwd: "/work/beta", Messages: user},
	}}
}

func twoOpenInApps() []host.OpenInApp {
	return []host.OpenInApp{{ID: "finder", Label: "Finder"}, {ID: "zed", Label: "Zed"}}
}

// thisComputersWindow and otherComputersWindow are the window of each
// computer over one set of Client preferences, as it is after a relaunch
// that switches computers.
func thisComputersWindow(t *testing.T, store *prefs.Store) *harness {
	t.Helper()
	h := newHarnessWith(t, sharedPathSessions("Mac"), "", func(cfg *Config) {
		cfg.Prefs = store
		cfg.OpenInApps = twoOpenInApps
	})
	h.until("this computer's Sessions", func() bool { return h.hasText("Mac first task") })
	return h
}

func otherComputersWindow(t *testing.T, store *prefs.Store) *harness {
	t.Helper()
	o := startOtherComputer(t, sharedPathSessions("Box"), "")
	h := followHarness(t, localSessions(), o.paired(), nil, func(cfg *Config) {
		cfg.Prefs = store
		cfg.OpenInApps = twoOpenInApps
	})
	h.until("the other computer's Sessions", func() bool { return h.hasText("Box first task") })
	return h
}

// workspaceHeader is a point on a Workspace's header: a Workspace is a group
// whose name finds the group, not the header, so a click at its centre would
// land on a Session row.
func (h *harness) workspaceHeader(workspace string) (x, y float32) {
	h.t.Helper()
	r, ok := h.tt.Find(workspace)
	if !ok {
		h.t.Fatalf("no Workspace %q in %q", workspace, h.tt.Texts())
	}
	return r.X + r.W/2, r.Y + 16
}

func (h *harness) foldWorkspace(workspace string) {
	h.t.Helper()
	x, y := h.workspaceHeader(workspace)
	h.tt.ClickAt(x, y)
	h.frame()
}

func (h *harness) openWorkspaceMenu(workspace string) {
	h.t.Helper()
	x, y := h.workspaceHeader(workspace)
	h.tt.RightClickAt(x, y)
	h.frame()
}

// pinItem is what the Workspace's menu offers: "Pin workspace" or "Unpin
// workspace".
func (h *harness) pinItem(workspace string) string {
	h.t.Helper()
	h.openWorkspaceMenu(workspace)
	defer func() { h.tt.CloseMenu(); h.frame() }()
	for _, item := range h.tt.Menu() {
		if item == "Pin workspace" || item == "Unpin workspace" {
			return item
		}
	}
	h.t.Fatalf("no pin item in %q", h.tt.Menu())
	return ""
}

func (h *harness) pinWorkspace(workspace string) {
	h.t.Helper()
	h.openWorkspaceMenu(workspace)
	if err := h.tt.ChooseMenuItem("Pin workspace"); err != nil {
		h.t.Fatal(err)
	}
	h.frame()
}

func (h *harness) moveWorkspaceDown(workspace string) {
	h.t.Helper()
	h.openWorkspaceMenu(workspace)
	if err := h.tt.ChooseMenuItem("Move down"); err != nil {
		h.t.Fatal(err)
	}
	h.frame()
}

func (h *harness) sortWorkspacesManually() {
	h.t.Helper()
	h.click("Sort")
	if err := h.tt.ChooseMenuItem("Manual"); err != nil {
		h.t.Fatal(err)
	}
	h.frame()
}

// above reports whether the first Workspace's header is over the second's.
func (h *harness) above(upper, lower string) bool {
	h.t.Helper()
	u, ok1 := h.tt.Find(upper)
	l, ok2 := h.tt.Find(lower)
	if !ok1 || !ok2 {
		h.t.Fatalf("headers %q and %q not both shown in %q", upper, lower, h.tt.Texts())
	}
	return u.Y < l.Y
}

func TestPreferencesSavedBeforeComputersWereKeptApartAreThisComputers(t *testing.T) {
	store := prefs.Memory()
	store.Set("droi.pinnedWorkspaces", `["/work/alpha"]`)
	store.Set("droi.foldedWorkspaces", `["/work/beta"]`)
	store.Set("droi.openInApp", "zed")

	this := thisComputersWindow(t, store)
	if got := this.pinItem("alpha"); got != "Unpin workspace" {
		t.Fatalf("this computer lost its pin: %q", got)
	}
	if this.hasText("Mac second task") {
		t.Fatal("this computer lost its fold")
	}
	this.openSession("Mac first task")
	this.until("the preferred app", func() bool { return this.hasButton("Open in Zed") })

	other := otherComputersWindow(t, store)
	if got := other.pinItem("alpha"); got != "Pin workspace" {
		t.Fatalf("the other computer starts with this computer's pin: %q", got)
	}
	if !other.hasText("Box second task") {
		t.Fatal("the other computer starts with this computer's fold")
	}
	other.openSession("Box first task")
	other.until("the Session", func() bool { return other.hasText("Context used") })
	if other.hasButton("Open in Zed") || other.hasButton("Open in Finder") {
		t.Fatal("the other computer offers an app of this computer")
	}
}

func TestSwitchingBackAndForthKeepsEachComputersPinsAndFolds(t *testing.T) {
	store := prefs.Memory()

	this := thisComputersWindow(t, store)
	this.pinWorkspace("alpha")
	this.foldWorkspace("beta")
	if this.hasText("Mac second task") {
		t.Fatal("the header did not fold the Workspace")
	}

	other := otherComputersWindow(t, store)
	if got := other.pinItem("alpha"); got != "Pin workspace" {
		t.Fatalf("this computer's pin shows on the other: %q", got)
	}
	if !other.hasText("Box second task") {
		t.Fatal("this computer's fold shows on the other")
	}
	other.foldWorkspace("beta")
	if other.hasText("Box second task") {
		t.Fatal("the header did not fold the Workspace")
	}
	other.pinWorkspace("beta")

	back := thisComputersWindow(t, store)
	if got := back.pinItem("alpha"); got != "Unpin workspace" {
		t.Fatalf("this computer lost its pin: %q", got)
	}
	if got := back.pinItem("beta"); got != "Pin workspace" {
		t.Fatalf("the other computer's pin reached this computer: %q", got)
	}
	if back.hasText("Mac second task") {
		t.Fatal("this computer lost its fold")
	}

	again := otherComputersWindow(t, store)
	if got := again.pinItem("beta"); got != "Unpin workspace" {
		t.Fatalf("the other computer lost its pin: %q", got)
	}
	if again.hasText("Box second task") {
		t.Fatal("the other computer lost its fold")
	}
	if got := again.pinItem("alpha"); got != "Pin workspace" {
		t.Fatalf("this computer's pin reached the other: %q", got)
	}
}

func TestEachComputerKeepsItsOwnManualOrderOfTheSameWorkspaces(t *testing.T) {
	store := prefs.Memory()
	moveFirstDown := func(h *harness, alphaFirst bool) {
		t.Helper()
		if alphaFirst {
			h.moveWorkspaceDown("alpha")
		} else {
			h.moveWorkspaceDown("beta")
		}
	}

	this := thisComputersWindow(t, store)
	this.sortWorkspacesManually()
	alphaFirst := this.above("alpha", "beta")
	moveFirstDown(this, alphaFirst)
	if this.above("alpha", "beta") == alphaFirst {
		t.Fatal("Move down did not reorder this computer")
	}

	other := otherComputersWindow(t, store)
	other.sortWorkspacesManually()
	if other.above("alpha", "beta") != alphaFirst {
		t.Fatal("this computer's order reached the other computer")
	}

	back := thisComputersWindow(t, store)
	if back.above("alpha", "beta") == alphaFirst {
		t.Fatal("this computer lost its order")
	}
}

func TestThisComputersPreferredOpenInAppSurvivesFollowingAnother(t *testing.T) {
	store := prefs.Memory()

	this := thisComputersWindow(t, store)
	this.openSession("Mac first task")
	this.until("the default app", func() bool { return this.hasButton("Open in Finder") })
	this.click("Open in another app")
	this.click("Zed")
	this.until("Zed preferred", func() bool { return this.hasButton("Open in Zed") })

	other := otherComputersWindow(t, store)
	other.openSession("Box first task")
	other.until("the Session", func() bool { return other.hasText("Context used") })
	if other.hasButton("Open in Zed") || other.hasButton("Open in Finder") {
		t.Fatal("the other computer offers an app of this computer")
	}

	back := thisComputersWindow(t, store)
	back.openSession("Mac first task")
	back.until("Zed still preferred", func() bool { return back.hasButton("Open in Zed") })
}
