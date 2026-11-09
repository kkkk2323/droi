package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/v2/pkg/gitutil"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel"
)

// The Host's own git on Kernel (M4 4N): a conversation's changes, new worktrees and Settings → Worktrees, with git
// on this computer and nothing deleted on its own.

// gitRepo is a repository on main with one commit, in a new folder.
func gitRepo(t *testing.T) string {
	t.Helper()
	if _, err := gitutil.Path(); err != nil {
		t.Skip("no git")
	}
	dir := filepath.Join(t.TempDir(), "acme-web")
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"README.md": "# Acme\n", "src/login.ts": "a\nb\n", ".gitignore": "node_modules/\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"}, {"config", "user.email", "t@example.com"}, {"config", "user.name", "T"},
		{"config", "commit.gpgsign", "false"}, {"add", "."}, {"commit", "-q", "-m", "first"},
	} {
		gitIn(t, dir, args...)
	}
	return dir
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// kernelConversationIn makes a conversation titled title in dir, from the test's own connection, and has the window
// list it.
func (h *harness) kernelConversationIn(dir, title string) kernel.ID {
	h.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	res, err := h.testConn().Create(ctx, kernel.CreateParams{Cwd: dir, Kind: "chat"})
	if err != nil {
		h.t.Fatal(err)
	}
	if err := h.testConn().UpdateIndex(ctx, kernel.IndexUpdateParams{ID: res.ConversationID, Title: kernel.TitleText(title)}); err != nil {
		h.t.Fatal(err)
	}
	h.a.reloadList()
	// What reads the list (Settings → Worktrees, say) reads it once a while: the conversation must be in it first.
	want := h.a.sessionIDOf(res.ConversationID)
	h.until("the conversation listed", func() bool { return slices.Contains(h.listedIDs(), want) })
	return res.ConversationID
}

// useWorktreeRoot has the Session defaults name root as the worktree directory.
func (h *harness) useWorktreeRoot(root string) {
	h.t.Helper()
	h.until("the Session defaults", func() bool { return h.a.sessionDefaults() != nil })
	h.a.saveDefaults(*h.a.sessionDefaults(), defaultsPatch{WorktreeDirectory: &root})
	h.until("the worktree directory", func() bool {
		dv := h.a.sessionDefaults()
		return dv != nil && dv.WorktreeDirectory == root
	})
}

func TestOnKernelTheChangesButtonReadsGitHere(t *testing.T) {
	h := kernelHarness(t)
	repo := gitRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "src/login.ts"), []byte("a\nB\nc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "notes.md"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.kernelConversationIn(repo, "Repo work")
	h.openSession("Repo work")
	const label = "Branch main, 2 changed files"
	h.until("the git button", func() bool { _, ok := h.tt.Find(label); return ok })
	h.click(label)
	h.frame()
	for _, want := range []string{"2 uncommitted files", "login.ts", "src/", "notes.md", "+3", "−1"} {
		if !h.hasText(want) {
			t.Errorf("no %q: %q", want, h.tt.Texts())
		}
	}
	if n := h.count("daemon.get_git_diff"); n != 0 {
		t.Errorf("the Daemon was asked for the changes %d times", n)
	}
}

func TestOnKernelANewSessionStartsInAWorktreeTheHostMakes(t *testing.T) {
	h := kernelHarness(t)
	repo := gitRepo(t)
	gitIn(t, repo, "branch", "feature/login")
	root := filepath.Join(t.TempDir(), "trees")
	h.useWorktreeRoot(root)
	h.openStartPage()
	h.a.newPage.pick, h.a.newPage.picked = repo, true
	h.frame()
	h.until("the worktree control", func() bool { return h.hasText("Work locally") })
	h.click("Worktree")
	h.frame()
	if h.hasText("Lifecycle") || h.hasText("Ephemeral") {
		t.Errorf("the Host's worktrees offer a lifecycle: %q", h.tt.Texts())
	}
	h.click("feature/login")
	h.tt.Key(0, ui.KeyEscape)
	h.frame()
	if !h.hasText("New worktree") {
		t.Fatalf("picking a base branch did not turn the worktree on: %q", h.tt.Texts())
	}
	h.start("Fix the flaky login test")
	var cwd string
	h.until("the conversation in a worktree", func() bool {
		for _, s := range h.a.listedNow() {
			if strings.HasPrefix(s.Cwd, root) {
				cwd = s.Cwd
				return true
			}
		}
		return false
	})
	name := filepath.Base(cwd)
	if filepath.Dir(cwd) != filepath.Join(root, "acme-web") || !strings.HasPrefix(name, "fix-the-flaky-login-test-") {
		t.Fatalf("the worktree is %s", cwd)
	}
	if got := gitIn(t, cwd, "rev-parse", "--abbrev-ref", "HEAD"); got != "droi/"+name {
		t.Errorf("the worktree's branch is %q", got)
	}
	if base, tip := gitIn(t, repo, "rev-parse", "feature/login"), gitIn(t, cwd, "rev-parse", "HEAD"); base != tip {
		t.Errorf("the branch starts at %s, not feature/login's %s", tip, base)
	}
	if got := gitIn(t, repo, "rev-parse", "--abbrev-ref", "HEAD"); got != "main" {
		t.Errorf("the repository's own checkout moved to %q", got)
	}
}

func TestOnKernelSettingsDeletesAWorktreeAfterSayingWhatItHolds(t *testing.T) {
	h := kernelHarness(t)
	repo := gitRepo(t)
	tree := filepath.Join(t.TempDir(), "trees", "fix-login")
	if err := gitutil.AddWorktree(context.Background(), gitutil.AddOptions{Repo: repo, Path: tree, Branch: "droi/fix-login"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tree, "draft.md"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(tree, "node_modules", "left-pad"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tree, "node_modules", "left-pad", "index.js"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.kernelConversationIn(repo, "Repo work")
	inTree := h.kernelConversationIn(tree, "Fix login")
	h.until("both conversations", func() bool { return h.hasText("Repo work") && h.hasText("Fix login") })
	h.click("Settings")
	h.click("Worktrees")
	h.until("the worktree", func() bool { return h.hasText(tree) })
	if h.hasText("Ephemeral worktrees limit") {
		t.Error("the Host's worktrees have a limit that deletes them")
	}
	if h.hasText(repo) && !h.hasText("acme-web · 1 session") {
		t.Errorf("the row: %q", h.tt.Texts())
	}
	h.click("Delete worktree droi/fix-login")
	h.until("the sessions it archives", func() bool { return h.hasText("Deleting this worktree will also archive 1 associated session.") })
	h.until("the warning", func() bool {
		return h.hasText("Warning: this worktree has 1 untracked file, 1 ignored file, such as .env or node_modules.")
	})
	if _, err := os.Stat(tree); err != nil {
		t.Fatal("the worktree went before the delete was confirmed")
	}
	h.click("Delete local branch")
	h.click("Delete worktree")
	h.until("the worktree gone", func() bool {
		_, err := os.Stat(tree)
		return os.IsNotExist(err) && h.hasText("No worktrees.")
	})
	if strings.Contains(gitIn(t, repo, "branch", "--list", "droi/fix-login"), "fix-login") {
		t.Error("the local branch was kept")
	}
	h.until("the conversation archived", func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		archived := true
		res, err := h.testConn().List(ctx, kernel.ListParams{Archived: &archived, Limit: 200})
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range res.Items {
			if s.ID == inTree {
				return true
			}
		}
		return false
	})
}

// The Host's delete is forced, so a worktree it could not check is not deleted: the button stays off.
func TestOnKernelAWorktreeThatCannotBeCheckedIsNotDeleted(t *testing.T) {
	h := kernelHarness(t)
	repo := gitRepo(t)
	tree := filepath.Join(t.TempDir(), "trees", "fix-login")
	if err := gitutil.AddWorktree(context.Background(), gitutil.AddOptions{Repo: repo, Path: tree, Branch: "droi/fix-login"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tree, "draft.md"), []byte("unsaved\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.kernelConversationIn(repo, "Repo work")
	h.click("Settings")
	h.click("Worktrees")
	h.until("the worktree", func() bool { return h.hasText(tree) })
	h.click("Delete worktree droi/fix-login")
	h.until("the check", func() bool { return h.a.worktreeAsk.checked })
	// A check that failed (a slow volume, a repository moved meanwhile): git itself could still remove the worktree.
	h.a.worktreeAsk.inspect, h.a.worktreeAsk.inspectErr = nil, "timed out"
	h.frame()
	h.until("the refusal", func() bool {
		return h.hasText("Could not check this worktree for unsaved work, so Droi does not delete it.")
	})
	h.click("Delete worktree")
	h.settle()
	if _, err := os.Stat(filepath.Join(tree, "draft.md")); err != nil {
		t.Fatalf("the unchecked worktree was deleted: %v", err)
	}
}

// A worktree whose folder is gone may still hold commits on its branch only: the dialog says so before the branch
// can be deleted with it.
func TestOnKernelAWorktreeWhoseFolderIsGoneSaysWhatItsBranchHolds(t *testing.T) {
	h := kernelHarness(t)
	repo := gitRepo(t)
	tree := filepath.Join(t.TempDir(), "trees", "fix-login")
	if err := gitutil.AddWorktree(context.Background(), gitutil.AddOptions{Repo: repo, Path: tree, Branch: "droi/fix-login"}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.md", "b.md"} {
		if err := os.WriteFile(filepath.Join(tree, name), []byte(name+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		gitIn(t, tree, "add", name)
		gitIn(t, tree, "-c", "user.email=t@example.com", "-c", "user.name=T", "commit", "-q", "-m", name)
	}
	if err := os.RemoveAll(tree); err != nil {
		t.Fatal(err)
	}
	h.kernelConversationIn(repo, "Repo work")
	h.click("Settings")
	h.click("Worktrees")
	h.until("the worktree", func() bool { return h.hasText(tree) && h.hasText("acme-web · 0 sessions · folder missing") })
	h.click("Delete worktree droi/fix-login")
	h.until("the branch's commits", func() bool {
		return h.hasText("Warning: this worktree has 2 commits on no remote.") && h.hasText("Its folder is gone; deleting it takes it off git's list.")
	})
	h.click("Delete worktree")
	h.until("the worktree off the list", func() bool { return h.hasText("No worktrees.") })
	if !strings.Contains(gitIn(t, repo, "branch", "--list", "droi/fix-login"), "fix-login") {
		t.Error("the branch went with the worktree though it was not asked to")
	}
}

func TestAWorktreeMadeForAConversationThatFailedIsTakenAway(t *testing.T) {
	repo := gitRepo(t)
	tree := filepath.Join(t.TempDir(), "trees", "x")
	if err := gitutil.AddWorktree(context.Background(), gitutil.AddOptions{Repo: repo, Path: tree, Branch: "droi/x"}); err != nil {
		t.Fatal(err)
	}
	discardWorktree(context.Background(), tree)
	if _, err := os.Stat(tree); !os.IsNotExist(err) {
		t.Errorf("the worktree is still there: %v", err)
	}
	if strings.Contains(gitIn(t, repo, "branch", "--list", "droi/x"), "droi/x") {
		t.Error("its branch is still there")
	}
}
