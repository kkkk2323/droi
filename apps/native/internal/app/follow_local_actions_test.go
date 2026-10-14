package app

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/packages/droid-sdk-go/fakedaemon"
)

const onlyPictures = "Only pictures can be attached to another computer."

func homeProject(t *testing.T) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no home folder")
	}
	return filepath.Join(home, "box-project")
}

func projectScenario(cwd string) fakedaemon.Scenario {
	return fakedaemon.Scenario{Sessions: []fakedaemon.SessionSpec{{Title: "On the box", Cwd: cwd,
		Messages: []fakedaemon.Message{{Role: "user", Text: "hello from linux"}}}}}
}

func (h *harness) openStartPage() {
	h.t.Helper()
	h.click("New session")
	h.until("the start button", func() bool { _, ok := h.tt.Find("Start session"); return ok })
}

func TestOpenInAnotherAppIsOfferedOnThisComputerAndHiddenOnAnother(t *testing.T) {
	local := newHarness(t, oneSession(), "")
	local.openSession("One")
	local.until("the button", func() bool { return local.hasButton("Open in Finder") && local.hasButton("Open in another app") })

	o := startOtherComputer(t, boxSessions(), "")
	h := followHarness(t, localSessions(), o.paired(), nil, nil)
	h.openSession("On the box")
	h.until("its messages", func() bool { return h.hasText("hello from linux") })
	for _, gone := range []string{"Open in Finder", "Open in another app"} {
		if h.hasButton(gone) {
			t.Errorf("%q is shown while following another computer", gone)
		}
	}
}

func TestOtherFolderIsOfferedOnThisComputerAndHiddenOnAnotherWhichStillOffersRecentAndScratch(t *testing.T) {
	local := newHarness(t, sessionsScenario(), "")
	local.until("the Session list", func() bool { return local.hasText("Fix the login race") })
	local.openStartPage()
	local.click("Workspace")
	local.until("the search", func() bool { _, ok := local.tt.Find("Search projects"); return ok })
	if !local.hasButton("Other folder…") {
		t.Fatalf("no Other folder… on this computer: %q", local.tt.Texts())
	}

	project := homeProject(t)
	o := startOtherComputer(t, projectScenario(project), "")
	h := followHarness(t, localSessions(), o.paired(), nil, nil)
	h.until("the other computer's Session", func() bool { return h.hasText("On the box") })
	h.openStartPage()
	h.click("Workspace")
	h.until("the search", func() bool { _, ok := h.tt.Find("Search projects"); return ok })
	if h.hasButton("Other folder…") {
		t.Fatal("Other folder… is offered while following another computer")
	}
	if !h.hasButton("Don't work in a project") {
		t.Fatalf("no Scratch choice: %q", h.tt.Texts())
	}
	if !h.hasText(project) {
		t.Fatalf("the other computer's path is not shown in full: %q", h.tt.Texts())
	}
	for _, s := range h.tt.Texts() {
		if strings.HasPrefix(s, "~") {
			t.Fatalf("a path is abbreviated with this computer's home: %q", s)
		}
	}
	h.click("Don't work in a project")
	h.until("Scratch chosen", func() bool { return h.hasText("What should we work on?") })
}

func TestThisComputersPathsAreAbbreviatedWithItsHome(t *testing.T) {
	project := homeProject(t)
	h := newHarnessWith(t, projectScenario(project), "", nil)
	h.until("the Session list", func() bool { return h.hasText("On the box") })
	h.openStartPage()
	h.click("Workspace")
	h.until("the search", func() bool { _, ok := h.tt.Find("Search projects"); return ok })
	if !h.hasText("~/box-project") {
		t.Fatalf("no abbreviated path: %q", h.tt.Texts())
	}
}

func TestTheAutomationFormsFolderPickerAndTheFactoryAppNoticeAreThisComputersOnly(t *testing.T) {
	notice := "The Factory App is running."

	local := newHarnessWith(t, automationsScenario(), "", func(cfg *Config) { cfg.FactoryAppRunning = func() bool { return true } })
	local.until("the Session list", func() bool { return local.hasText("Fix the login race") })
	local.click("Automations")
	local.until("the notice", func() bool { return local.hasText(notice) })
	local.click("New automation")
	local.until("the form", func() bool { _, ok := local.tt.Find("Automation workspace"); return ok })
	if !local.hasButton("Choose a workspace") {
		t.Fatal("no folder picker on this computer")
	}

	o := startOtherComputer(t, automationsScenario(), "")
	h := followHarness(t, localSessions(), o.paired(), nil, func(cfg *Config) { cfg.FactoryAppRunning = func() bool { return true } })
	h.until("the other computer's Session", func() bool { return h.hasText("Fix the login race") })
	h.click("Automations")
	h.until("the list", func() bool { return h.hasText("Daily review") })
	h.settle()
	if h.hasText(notice) {
		t.Fatal("the Factory App notice is shown for another computer")
	}
	h.click("New automation")
	h.until("the form", func() bool { _, ok := h.tt.Find("Automation workspace"); return ok })
	if h.hasButton("Choose a workspace") {
		t.Fatal("a local folder picker is offered for an automation of another computer")
	}
}

func TestCustomFoldersOfTheDaemonsSettingsAreNotPickedFromThisComputersDisk(t *testing.T) {
	picked := false
	pick := func(c *Config) { c.PickFolder = func(string) string { picked = true; return "/Users/dev/x" } }
	o := startOtherComputer(t, worktreeScenario(nil), "")
	h := followHarness(t, localSessions(), o.paired(), nil, pick)
	h.tt.SetSize(refW, 2000)
	h.until("the other computer's Session", func() bool { return h.hasText("Add dark mode toggle") })
	h.click("Settings")
	h.click("Worktrees")
	h.until("the settings", func() bool { return h.hasText("Worktree directory") })
	h.click("Default (/Users/test/.factory/worktrees)")
	if h.hasButton("Custom…") {
		t.Error("Custom… folder of the worktree directory is offered")
	}
	h.tt.Key(0, ui.KeyEscape)
	h.frame()
	h.click("Session defaults")
	h.until("the defaults", func() bool { return h.hasText("Limits for specific models") })
	h.click("Spec save folder")
	if !h.hasButton("Project") {
		t.Fatalf("the spec folder choices did not open: %q", h.tt.Texts())
	}
	if h.hasButton("Custom…") {
		t.Error("Custom… folder of the spec save folder is offered")
	}
	if picked {
		t.Fatal("this computer's folder dialog was used")
	}
}

func pictureFile(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "shot.png")
	if err := os.WriteFile(p, pngBytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func textFile(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(p, []byte("remember\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestAPictureIsSentToAnotherComputerAsDataAndAFileIsRefused(t *testing.T) {
	picture, notes := pictureFile(t), textFile(t)
	files := []string{picture}
	o := startOtherComputer(t, deployScenario(&fakedaemon.Turn{Kind: "reply", Deltas: []string{"Seen."}}), "")
	h := followHarness(t, localSessions(), o.paired(), nil, func(cfg *Config) {
		cfg.ReadFiles = func() []string { return files }
	})
	h.openSession("Deploy")
	h.focusComposer()
	h.tt.Key(ui.Cmd, ui.KeyV)
	h.frame()
	if _, ok := h.tt.Find("shot.png"); !ok {
		t.Fatalf("the picture is not attached: %q", h.tt.Texts())
	}

	files = []string{notes}
	h.tt.Key(ui.Cmd, ui.KeyV)
	h.frame()
	if !h.hasText(onlyPictures) {
		t.Fatalf("no message that only pictures can be attached: %q", h.tt.Texts())
	}
	if got := h.sessionText(); strings.Contains(got, notes) || got != "" {
		t.Fatalf("the file's local path went into the message: %q", got)
	}

	h.typeInto("see this")
	h.click("Send")
	req, err := o.d.WaitForRequest("daemon.add_user_message", 1)
	if err != nil {
		t.Fatal(err)
	}
	params := string(req.Params)
	if !strings.Contains(params, base64.StdEncoding.EncodeToString(pngBytes())) {
		t.Fatalf("the picture was not sent as data: %s", params)
	}
	if strings.Contains(params, notes) || strings.Contains(params, picture) {
		t.Fatalf("a local path was sent to the other computer: %s", params)
	}
}

func TestAFileIsRefusedOnTheStartPageOfAnotherComputerButAPictureIsKept(t *testing.T) {
	picture, notes := pictureFile(t), textFile(t)
	files := []string{picture, notes}
	o := startOtherComputer(t, boxSessions(), "")
	h := followHarness(t, localSessions(), o.paired(), nil, func(cfg *Config) {
		cfg.ReadFiles = func() []string { return files }
	})
	h.until("the other computer's Session", func() bool { return h.hasText("On the box") })
	h.openStartPage()
	h.click("Message")
	h.tt.Key(ui.Cmd, ui.KeyV)
	h.frame()
	if len(h.a.newPage.images) != 1 {
		t.Fatalf("%d pictures attached", len(h.a.newPage.images))
	}
	if !h.hasText(onlyPictures) || strings.Contains(h.a.newPage.text, notes) {
		t.Fatalf("file not refused: text %q, %q", h.a.newPage.text, h.tt.Texts())
	}
}

func TestAFileOnThisComputerStillGoesInAsItsPath(t *testing.T) {
	notes := textFile(t)
	h := newHarnessWith(t, oneSession(), "", func(cfg *Config) { cfg.ReadFiles = func() []string { return []string{notes} } })
	h.openSession("One")
	h.focusComposer()
	h.tt.Key(ui.Cmd, ui.KeyV)
	h.frame()
	if got := h.sessionText(); !strings.Contains(got, notes) {
		t.Fatalf("the path is not in the message: %q", got)
	}
	if h.hasText(onlyPictures) {
		t.Fatal("refused on this computer")
	}
}

func TestAPictureDroidWroteAsAPathOnAnotherComputerShows(t *testing.T) {
	sc := fakedaemon.Scenario{
		Sessions: []fakedaemon.SessionSpec{{Title: "Shots", Cwd: "/home/clive/box-project", Messages: []fakedaemon.Message{
			{Role: "user", Text: "show me"},
			{Role: "assistant", Text: "Here it is:\n\n![The banner](/home/clive/box-project/shot.png)\n\n![Old shot](/tmp/gone.png)"},
		}}},
		Input: map[string]any{"files": map[string]any{
			"/home/clive/box-project/shot.png": map[string]any{"mimeType": "image/png", "base64": pngOf(800, 400)},
		}},
	}
	o := startOtherComputer(t, sc, "")
	h := followHarness(t, localSessions(), o.paired(), nil, nil)
	h.openSession("Shots")
	h.until("the picture", func() bool { _, ok := h.tt.Find("The banner"); return ok })
	h.until("the missing one's path", func() bool { return h.hasText("/tmp/gone.png") })
}
