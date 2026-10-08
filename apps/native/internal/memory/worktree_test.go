package memory

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// fakeWorktree lays out what `git worktree add` leaves behind: a main
// checkout whose .git keeps the worktree's repository, and the worktree,
// elsewhere, whose .git file points there.
func fakeWorktree(t *testing.T) (main, worktree string) {
	t.Helper()
	root := must[string](t)(evalSymlinks(t.TempDir()))
	main = filepath.Join(root, "repo")
	worktree = filepath.Join(root, "worktrees", "feature", "repo")
	gitdir := filepath.Join(main, ".git", "worktrees", "feature")
	for _, dir := range []string{gitdir, filepath.Join(worktree, "src")} {
		if err := os.MkdirAll(dir, 0o777); err != nil {
			t.Fatal(err)
		}
	}
	write(t, filepath.Join(gitdir, "commondir"), "../..\n")
	write(t, filepath.Join(worktree, ".git"), "gitdir: "+gitdir+"\n")
	return main, worktree
}

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o666); err != nil {
		t.Fatal(err)
	}
}

func TestAWorktreeSharesItsMainCheckoutsProjectMemory(t *testing.T) {
	main, worktree := fakeWorktree(t)
	eq(t, ProjectSlot(worktree), ProjectSlot(main))
	eq(t, ProjectSlot(filepath.Join(worktree, "src")), Slot{Scope: ScopeProject, Workspace: filepath.Join(main, "src")})
	eq(t, ProjectSlot(main), Slot{Scope: ScopeProject, Workspace: main})
}

func TestASubmoduleKeepsItsOwnProjectMemory(t *testing.T) {
	root := must[string](t)(evalSymlinks(t.TempDir()))
	modules := filepath.Join(root, ".git", "modules", "lib")
	lib := filepath.Join(root, "lib")
	for _, dir := range []string{modules, lib} {
		if err := os.MkdirAll(dir, 0o777); err != nil {
			t.Fatal(err)
		}
	}
	write(t, filepath.Join(lib, ".git"), "gitdir: ../.git/modules/lib\n")
	eq(t, ProjectSlot(lib), Slot{Scope: ScopeProject, Workspace: lib})
}

func TestAWorktreeMadeByGitSharesItsMainCheckoutsProjectMemory(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	root := must[string](t)(evalSymlinks(t.TempDir()))
	main := filepath.Join(root, "repo")
	worktree := filepath.Join(root, "elsewhere", "repo")
	git := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.MkdirAll(main, 0o777); err != nil {
		t.Fatal(err)
	}
	git(main, "init", "-q")
	git(main, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "start")
	git(main, "worktree", "add", "-q", worktree)
	eq(t, ProjectSlot(worktree), ProjectSlot(main))
}

func TestAWorktreesEarlierProjectMemoryMovesToItsMainCheckout(t *testing.T) {
	s, _ := openTestStore(t)
	main, worktree := fakeWorktree(t)
	old := Slot{Scope: ScopeProject, Workspace: worktree}
	kept := add(t, s, ProjectSlot(main), "kept")
	moved := add(t, s, old, "moved")
	other := add(t, s, project, "elsewhere")

	if err := s.MergeWorktrees(); err != nil {
		t.Fatal(err)
	}

	entries := must[[]Entry](t)(s.List(ProjectSlot(main), ""))
	ids := map[string]bool{}
	for _, e := range entries {
		ids[e.ID] = true
		eq(t, e.Workspace, main)
	}
	eq(t, ids, map[string]bool{kept.ID: true, moved.ID: true})
	eq(t, must[[]Entry](t)(s.List(project, "")), []Entry{other})
	for _, summary := range must[[]SlotSummary](t)(s.Summaries()) {
		if summary.Workspace == worktree {
			t.Fatalf("the worktree's Project Memory is still listed: %+v", summary)
		}
	}
}
