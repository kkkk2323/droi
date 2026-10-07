package app

import (
	"encoding/json"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/fakedaemon"

	"github.com/kkkk2323/droi/apps/native/internal/prefs"
)

const (
	acmeWeb      = "/Users/dev/acme-web"
	acmeWorktree = "/Users/test/.factory/worktrees/abcd1234/acme-web"
)

func gitRepos() map[string]any {
	return map[string]any{acmeWeb: map[string]any{"currentBranch": "main", "branches": []string{"main", "feature/login"}}}
}

func worktreeScenario(extra map[string]any) fakedaemon.Scenario {
	input := map[string]any{"gitRepos": gitRepos()}
	for k, v := range extra {
		input[k] = v
	}
	return fakedaemon.Scenario{
		Sessions: []fakedaemon.SessionSpec{
			{Title: "Refactor invoice generator", Cwd: "/Users/dev/billing-service", Messages: []fakedaemon.Message{{Role: "user", Text: "Refactor"}}},
			{Title: "Add dark mode toggle", Cwd: acmeWeb, Messages: []fakedaemon.Message{{Role: "user", Text: "Add dark mode"}}},
		},
		Turn:  &fakedaemon.Turn{Kind: "reply", Deltas: []string{"On it."}},
		Input: input,
	}
}

// inWorktree is a Session the Daemon runs in an ephemeral worktree of acme-web.
func inWorktree(lifecycle string) fakedaemon.SessionSpec {
	return fakedaemon.SessionSpec{Title: "Fix the flaky login test", Cwd: acmeWorktree,
		Messages: []fakedaemon.Message{{Role: "user", Text: "Fix the flaky login test"}},
		Extra:    map[string]any{"worktree": map[string]any{"path": acmeWorktree, "branch": "droid/fix-flaky-login", "lifecycle": lifecycle, "repoRoot": acmeWeb}}}
}

func (h *harness) initializeParams() map[string]any {
	h.t.Helper()
	req, err := h.d.WaitForRequest("daemon.initialize_session", 1)
	if err != nil {
		h.t.Fatal(err)
	}
	var p map[string]any
	_ = json.Unmarshal(req.Params, &p)
	return p
}

// The New session page offers a worktree for a Git Workspace and asks the
// Daemon for one with the lifecycle and base branch picked; the Session
// then lists under its repository with its branch.
func TestTheNewSessionPageStartsASessionInAWorktree(t *testing.T) {
	h := newHarness(t, worktreeScenario(nil), "")
	h.until("the Session list", func() bool { return h.hasText("Add dark mode toggle") })
	h.click("New session")
	h.until("the worktree control", func() bool { return h.hasText("Work locally") })
	h.click("Worktree")
	h.click("Persistent")
	h.click("feature/login")
	if !h.hasText("New worktree") {
		t.Fatalf("picking a lifecycle did not turn the worktree on: %q", h.tt.Texts())
	}
	if got := prefs.WorktreeLifecycle.Get(h.a.prefs); got != "persistent" {
		t.Fatalf("the lifecycle picked is not kept: %q", got)
	}
	h.tt.Key(0, ui.KeyEscape)
	h.frame()
	h.start("Fix the flaky login test")
	p := h.initializeParams()
	for key, want := range map[string]any{
		"cwd": acmeWeb, "worktree": true, "worktreeLifecycle": "persistent", "worktreeBaseBranch": "feature/login",
		"worktreeBranchMode": "copy", "worktreePromptSlug": "Fix the flaky login test",
	} {
		if p[key] != want {
			t.Errorf("initialize_session %s = %v, want %v", key, p[key], want)
		}
	}
	h.until("the Session's branch in the sidebar", func() bool { return h.hasText("droid/fix-the-flaky-login-test") })
	var found bool
	for _, s := range h.a.listed {
		if s.Worktree != nil {
			found = true
			if s.Worktree.RepoRoot != acmeWeb || s.Worktree.Lifecycle != "persistent" {
				t.Errorf("listed worktree %+v", s.Worktree)
			}
		}
	}
	if !found {
		t.Fatal("no listed Session has a worktree")
	}
}

// Work locally asks for no worktree, and a folder that is no Git
// repository is offered none.
func TestTheNewSessionPageWorksLocallyByDefault(t *testing.T) {
	h := newHarness(t, worktreeScenario(nil), "")
	h.until("the Session list", func() bool { return h.hasText("Add dark mode toggle") })
	h.click("New session")
	h.until("the worktree control", func() bool { return h.hasText("Work locally") })
	h.start("Say hello")
	if p := h.initializeParams(); p["worktree"] != nil {
		t.Fatalf("working locally asked for a worktree: %v", p)
	}

	plain := newHarness(t, fakedaemon.Scenario{Sessions: []fakedaemon.SessionSpec{
		{Title: "Notes", Cwd: "/Users/dev/notes", Messages: []fakedaemon.Message{{Role: "user", Text: "hi"}}},
	}}, "")
	plain.until("the Session list", func() bool { return plain.hasText("Notes") })
	plain.click("New session")
	plain.settle()
	if plain.hasText("Work locally") || plain.hasText("New worktree") {
		t.Fatal("a folder outside Git offers a worktree")
	}
}

// Archiving a Session in an ephemeral worktree says the worktree goes
// with it, and only then archives.
func TestArchivingASessionInAnEphemeralWorktreeAsksFirst(t *testing.T) {
	sc := worktreeScenario(nil)
	sc.Sessions = append(sc.Sessions, inWorktree("ephemeral"))
	h := newHarness(t, sc, "")
	h.until("the worktree Session's branch", func() bool { return h.hasText("droid/fix-flaky-login") })
	if err := h.tt.RightClick("Fix the flaky login test"); err != nil {
		t.Fatal(err)
	}
	h.frame()
	if err := h.tt.ChooseMenuItem("Archive"); err != nil {
		t.Fatal(err)
	}
	h.frame()
	h.until("the warning", func() bool { return h.hasText("Archiving this session will delete its worktree.") })
	h.until("the check", func() bool { return !h.hasText("Checking current git status...") })
	if reqs, _ := h.d.Requests(); hasMethod(reqs, "daemon.archive_session") {
		t.Fatal("archived before the answer")
	}
	h.click("Archive")
	if _, err := h.d.WaitForRequest("daemon.archive_session", 1); err != nil {
		t.Fatal(err)
	}
	h.until("the Session to leave the list", func() bool { return !h.hasText("Fix the flaky login test") })
}

// Settings → Worktrees lists the Daemon's worktrees and deletes one after
// showing what it holds.
func TestSettingsDeletesAManagedWorktree(t *testing.T) {
	sc := worktreeScenario(map[string]any{"worktreeInspections": map[string]any{acmeWorktree: map[string]any{"changedFiles": 2, "untrackedFiles": 1}}})
	sc.Sessions = append(sc.Sessions, inWorktree("persistent"))
	h := newHarness(t, sc, "")
	h.until("the Session list", func() bool { return h.hasText("Fix the flaky login test") })
	h.click("Settings")
	h.click("Worktrees")
	h.until("the managed worktree", func() bool { return h.hasText(acmeWorktree) })
	if !h.hasText("Worktree directory") || !h.hasText("Ephemeral worktrees limit") {
		t.Fatalf("the worktree settings are missing: %q", h.tt.Texts())
	}
	h.click("Delete worktree droid/fix-flaky-login")
	h.until("the warning", func() bool {
		return h.hasText("Warning: this worktree has 2 changed files, 1 untracked file.")
	})
	h.click("Delete local branch")
	h.click("Delete worktree")
	req, err := h.d.WaitForRequest("daemon.cleanup_worktree", 1)
	if err != nil {
		t.Fatal(err)
	}
	var p map[string]any
	_ = json.Unmarshal(req.Params, &p)
	if p["worktreePath"] != acmeWorktree || p["force"] != true || p["deleteLocalBranch"] != true || p["deleteRemoteBranch"] != false {
		t.Fatalf("cleanup_worktree %v", p)
	}
	h.until("the list to empty", func() bool { return h.hasText("No Factory-managed worktrees.") })
}

// The worktree directory is the Daemon's setting, saved from Settings.
func TestSettingsSavesTheWorktreeDirectory(t *testing.T) {
	h := newHarnessWith(t, worktreeScenario(nil), "", func(c *Config) {
		c.PickFolder = func(string) string { return "/Users/dev/trees" }
	})
	h.until("the Session list", func() bool { return h.hasText("Add dark mode toggle") })
	h.click("Settings")
	h.click("Worktrees")
	h.until("the settings", func() bool { return h.hasText("Worktree directory") })
	h.click("Default (/Users/test/.factory/worktrees)")
	h.click("Custom…")
	req, err := h.d.WaitForRequest("daemon.update_session_defaults", 1)
	if err != nil {
		t.Fatal(err)
	}
	var p map[string]any
	_ = json.Unmarshal(req.Params, &p)
	if p["worktreeDirectory"] != "/Users/dev/trees" {
		t.Fatalf("update_session_defaults %v", p)
	}
	h.until("the custom folder", func() bool { return h.hasText("Custom: /Users/dev/trees") })
}

// start types the first message on the New session page and starts the Session.
func (h *harness) start(text string) {
	h.t.Helper()
	h.click("Message")
	h.tt.Type(text)
	h.frame()
	h.click("Start session")
}

func hasMethod(reqs []fakedaemon.Request, method string) bool {
	for _, r := range reqs {
		if r.Method == method {
			return true
		}
	}
	return false
}
