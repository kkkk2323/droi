package gitutil

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// repo makes a repository with one commit on main, in a folder of its own.
func repo(t *testing.T) string {
	t.Helper()
	if _, err := Path(); err != nil {
		t.Skip("no git:", err)
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "repo")
	must(t, os.MkdirAll(dir, 0o755))
	gitIn(t, dir, "init", "-q", "-b", "main")
	gitIn(t, dir, "config", "user.email", "t@example.com")
	gitIn(t, dir, "config", "user.name", "T")
	gitIn(t, dir, "config", "commit.gpgsign", "false")
	write(t, filepath.Join(dir, "a.txt"), "one\ntwo\nthree\n")
	write(t, filepath.Join(dir, "old.txt"), "keep\nme\n")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "first")
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

func write(t *testing.T, path, body string) {
	t.Helper()
	must(t, os.MkdirAll(filepath.Dir(path), 0o755))
	must(t, os.WriteFile(path, []byte(body), 0o644))
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func eq[T any](t *testing.T, got, want T) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %#v\nwant %#v", got, want)
	}
}

func TestStatusNamesEveryKindOfChangeWithItsLines(t *testing.T) {
	ctx := context.Background()
	dir := repo(t)
	c, ok, err := Status(ctx, dir)
	must(t, err)
	eq(t, []any{ok, c.Branch, c.Clean()}, []any{true, "main", true})

	write(t, filepath.Join(dir, "a.txt"), "one\nTWO\nthree\nfour\n") // +2 −1
	write(t, filepath.Join(dir, "staged.txt"), "s\n")                // added, staged
	gitIn(t, dir, "add", "staged.txt")
	gitIn(t, dir, "mv", "old.txt", "new name.txt")             // renamed
	write(t, filepath.Join(dir, "sub", "新文件.md"), "x\ny\nz\n") // untracked, 3 lines
	write(t, filepath.Join(dir, "bin.dat"), "a\x00b")          // untracked binary
	c, _, err = Status(ctx, dir)
	must(t, err)
	var got []string
	for _, f := range c.Files {
		got = append(got, f.Status+" "+f.Path+" "+f.From+" "+itoa(f.Additions)+"/"+itoa(f.Deletions)+map[bool]string{true: " bin"}[f.Binary])
	}
	eq(t, got, []string{
		"modified a.txt  2/1",
		"untracked bin.dat  0/0 bin",
		"renamed new name.txt old.txt 0/0",
		"added staged.txt  1/0",
		"untracked sub/新文件.md  3/0",
	})
	eq(t, []int{c.Additions, c.Deletions}, []int{6, 1})
}

func itoa(n int) string { return strconv.Itoa(n) }

func TestStatusOutsideARepositoryAndBeforeTheFirstCommit(t *testing.T) {
	ctx := context.Background()
	if _, err := Path(); err != nil {
		t.Skip(err)
	}
	plain := t.TempDir()
	_, ok, err := Status(ctx, plain)
	eq(t, []any{ok, err}, []any{false, error(nil)})
	_, ok, _ = ListBranches(ctx, plain)
	eq(t, ok, false)

	fresh := filepath.Join(t.TempDir(), "fresh")
	must(t, os.MkdirAll(fresh, 0o755))
	gitIn(t, fresh, "init", "-q", "-b", "trunk")
	write(t, filepath.Join(fresh, "a.txt"), "1\n2\n")
	gitIn(t, fresh, "add", "a.txt")
	c, ok, err := Status(ctx, fresh)
	must(t, err)
	eq(t, []any{ok, c.Branch, len(c.Files), c.Additions}, []any{true, "trunk", 1, 2})
}

func TestBranchesAndADetachedHead(t *testing.T) {
	ctx := context.Background()
	dir := repo(t)
	gitIn(t, dir, "branch", "feature")
	b, ok, err := ListBranches(ctx, dir)
	must(t, err)
	eq(t, []any{ok, b.Current, b.Detached, len(b.Names)}, []any{true, "main", false, 2})
	eq(t, HasBranch(ctx, dir, "feature"), true)
	gitIn(t, dir, "checkout", "-q", "--detach")
	b, _, err = ListBranches(ctx, dir)
	must(t, err)
	eq(t, []any{b.Current, b.Detached}, []any{"", true})
	c, _, err := Status(ctx, dir)
	must(t, err)
	eq(t, []any{c.Branch, c.Detached}, []any{"", true})
}

// A worktree on a new branch; what removing it would lose; it is kept while it holds changes unless forced; the
// repository itself is never removed; a branch of that name is never reused.
func TestAWorktreeIsMadeInspectedAndRemovedOnlyAsAsked(t *testing.T) {
	ctx := context.Background()
	dir := repo(t)
	wt := filepath.Join(filepath.Dir(dir), "wt one")
	must(t, AddWorktree(ctx, AddOptions{Repo: dir, Path: wt, Branch: "droi/fix-login", Base: "main"}))
	if err := AddWorktree(ctx, AddOptions{Repo: dir, Path: wt + "2", Branch: "droi/fix-login"}); !errors.Is(err, ErrBranchExists) {
		t.Fatalf("a second worktree on the same branch: %v", err)
	}
	list, err := Worktrees(ctx, dir)
	must(t, err)
	eq(t, len(list), 2)
	eq(t, []any{list[0].Main, list[0].Branch, list[1].Main, list[1].Branch, list[1].Path}, []any{true, "main", false, "droi/fix-login", wt})

	in, err := Inspect(ctx, wt)
	must(t, err)
	// Made from main, it has no commit of its own yet.
	eq(t, in, Inspection{Branch: "droi/fix-login"})
	write(t, filepath.Join(wt, "mine.txt"), "m\n")
	gitIn(t, wt, "add", "mine.txt")
	gitIn(t, wt, "commit", "-q", "-m", "mine")
	in, err = Inspect(ctx, wt)
	must(t, err)
	eq(t, in.LocalOnlyCommits, 1)
	write(t, filepath.Join(wt, "a.txt"), "changed\n")
	write(t, filepath.Join(wt, "new.txt"), "n\n")
	in, err = Inspect(ctx, wt)
	must(t, err)
	eq(t, []int{in.ChangedFiles, in.UntrackedFiles}, []int{1, 1})
	eq(t, in.Unsaved(), true)

	res, err := RemoveWorktree(ctx, RemoveOptions{Path: wt, DeleteLocalBranch: true})
	must(t, err)
	eq(t, res, RemoveResult{KeptForChanges: true})
	if _, err := os.Stat(wt); err != nil {
		t.Fatal("the worktree went although it held changes")
	}
	if _, err := RemoveWorktree(ctx, RemoveOptions{Path: dir, Force: true}); err == nil {
		t.Fatal("the repository itself was removed as a worktree")
	}

	res, err = RemoveWorktree(ctx, RemoveOptions{Path: wt, Force: true, DeleteLocalBranch: true})
	must(t, err)
	eq(t, res, RemoveResult{Removed: true})
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatalf("the worktree is still there: %v", err)
	}
	eq(t, HasBranch(ctx, dir, "droi/fix-login"), false)
	eq(t, HasBranch(ctx, dir, "main"), true)
}

// A remote branch that cannot be deleted is a warning, not a failure: the worktree is gone either way.
func TestADeleteOfAnOriginBranchThatFailsIsAWarning(t *testing.T) {
	ctx := context.Background()
	dir := repo(t)
	wt := filepath.Join(filepath.Dir(dir), "wt")
	must(t, AddWorktree(ctx, AddOptions{Repo: dir, Path: wt, Branch: "b"}))
	res, err := RemoveWorktree(ctx, RemoveOptions{Path: wt, DeleteRemoteBranch: true})
	must(t, err)
	if !res.Removed || len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "origin branch b was kept") {
		t.Fatalf("%+v", res)
	}
}

func TestAnOriginBranchIsDeletedOnlyWhenItHoldsNothingElse(t *testing.T) {
	ctx := context.Background()
	dir := repo(t)
	root := filepath.Dir(dir)
	origin := filepath.Join(root, "origin.git")
	gitIn(t, root, "clone", "-q", "--bare", dir, origin)
	gitIn(t, dir, "remote", "add", "origin", origin)
	gitIn(t, dir, "fetch", "-q", "origin")

	// Someone else's droi/fix-login on origin, with a commit nobody here has.
	other := filepath.Join(root, "other")
	gitIn(t, root, "clone", "-q", origin, other)
	gitIn(t, other, "config", "user.email", "o@example.com")
	gitIn(t, other, "config", "user.name", "O")
	gitIn(t, other, "checkout", "-q", "-b", "droi/fix-login")
	write(t, filepath.Join(other, "theirs.txt"), "theirs\n")
	gitIn(t, other, "add", ".")
	gitIn(t, other, "commit", "-q", "-m", "theirs")
	gitIn(t, other, "push", "-q", "origin", "droi/fix-login")

	wt := filepath.Join(root, "wt")
	must(t, AddWorktree(ctx, AddOptions{Repo: dir, Path: wt, Branch: "droi/fix-login", Base: "main"}))
	res, err := RemoveWorktree(ctx, RemoveOptions{Path: wt, DeleteLocalBranch: true, DeleteRemoteBranch: true})
	must(t, err)
	if !res.Removed || len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "has commits this branch does not") {
		t.Fatalf("%+v", res)
	}
	if out := gitIn(t, root, "--git-dir", origin, "branch", "--list", "droi/fix-login"); out == "" {
		t.Fatal("origin's branch of another clone was deleted")
	}

	// A branch this worktree pushed is deleted with it.
	wt2 := filepath.Join(root, "wt2")
	must(t, AddWorktree(ctx, AddOptions{Repo: dir, Path: wt2, Branch: "droi/mine", Base: "main"}))
	gitIn(t, wt2, "push", "-q", "origin", "droi/mine")
	res, err = RemoveWorktree(ctx, RemoveOptions{Path: wt2, DeleteLocalBranch: true, DeleteRemoteBranch: true})
	must(t, err)
	eq(t, res, RemoveResult{Removed: true})
	if out := gitIn(t, root, "--git-dir", origin, "branch", "--list", "droi/mine"); out != "" {
		t.Fatalf("origin still has %q", out)
	}
}

func TestAWorktreeWhoseFolderIsGoneIsStillRemoved(t *testing.T) {
	ctx := context.Background()
	dir := repo(t)
	wt := filepath.Join(filepath.Dir(dir), "gone")
	must(t, AddWorktree(ctx, AddOptions{Repo: dir, Path: wt, Branch: "gone"}))
	must(t, os.RemoveAll(wt))
	res, err := RemoveWorktree(ctx, RemoveOptions{Repo: dir, Path: wt, DeleteLocalBranch: true})
	must(t, err)
	eq(t, res, RemoveResult{Removed: true})
	list, err := Worktrees(ctx, dir)
	must(t, err)
	eq(t, len(list), 1)
	eq(t, HasBranch(ctx, dir, "gone"), false)
}

func TestAWorktreeThatCouldNotBeMadeLeavesNothingBehind(t *testing.T) {
	ctx := context.Background()
	dir := repo(t)
	wt := filepath.Join(filepath.Dir(dir), "bad")
	if err := AddWorktree(ctx, AddOptions{Repo: dir, Path: wt, Branch: "bad", Base: "no-such-base"}); err == nil {
		t.Fatal("a worktree from a base that does not exist")
	}
	eq(t, HasBranch(ctx, dir, "bad"), false)
	list, err := Worktrees(ctx, dir)
	must(t, err)
	eq(t, len(list), 1)
	// And the same call goes through once the base is right.
	must(t, AddWorktree(ctx, AddOptions{Repo: dir, Path: wt, Branch: "bad", Base: "main"}))
	for _, name := range []string{"--force", "-b", "a..b", "with space"} {
		if err := AddWorktree(ctx, AddOptions{Repo: dir, Path: wt + "x", Branch: name}); err == nil || !strings.Contains(err.Error(), "not a branch name") {
			t.Errorf("branch %q: %v", name, err)
		}
	}
}

func TestALockedWorktreeGoesOnlyWhenForced(t *testing.T) {
	ctx := context.Background()
	dir := repo(t)
	wt := filepath.Join(filepath.Dir(dir), "locked")
	must(t, AddWorktree(ctx, AddOptions{Repo: dir, Path: wt, Branch: "locked"}))
	gitIn(t, dir, "worktree", "lock", "--reason", "in use", wt)
	res, err := RemoveWorktree(ctx, RemoveOptions{Path: wt})
	must(t, err)
	eq(t, res, RemoveResult{KeptForChanges: true})
	res, err = RemoveWorktree(ctx, RemoveOptions{Path: wt, Force: true})
	must(t, err)
	eq(t, res, RemoveResult{Removed: true})
}

func TestIgnoredFilesAreWhatAWorktreeWouldLoseToo(t *testing.T) {
	ctx := context.Background()
	dir := repo(t)
	write(t, filepath.Join(dir, ".gitignore"), ".env\nnode_modules/\n")
	gitIn(t, dir, "add", ".gitignore")
	gitIn(t, dir, "commit", "-q", "-m", "ignore")
	wt := filepath.Join(filepath.Dir(dir), "env")
	must(t, AddWorktree(ctx, AddOptions{Repo: dir, Path: wt, Branch: "env"}))
	write(t, filepath.Join(wt, ".env"), "SECRET=1\n")
	write(t, filepath.Join(wt, "node_modules", "a", "index.js"), "x\n")
	in, err := Inspect(ctx, wt)
	must(t, err)
	eq(t, []any{in.ChangedFiles, in.UntrackedFiles, in.IgnoredFiles, in.LocalOnlyCommits, in.Unsaved()}, []any{0, 0, 2, 0, true})
	res, err := RemoveWorktree(ctx, RemoveOptions{Path: wt})
	must(t, err)
	eq(t, res, RemoveResult{KeptForChanges: true})
	if _, err := os.Stat(filepath.Join(wt, ".env")); err != nil {
		t.Fatal(".env went with a worktree that was kept")
	}
}

func TestABranchListIsBranchNamesEvenBesideATagOfTheSameName(t *testing.T) {
	ctx := context.Background()
	dir := repo(t)
	gitIn(t, dir, "branch", "release")
	gitIn(t, dir, "tag", "release")
	b, _, err := ListBranches(ctx, dir)
	must(t, err)
	slices.Sort(b.Names)
	eq(t, b.Names, []string{"main", "release"})
}

func TestAnUntrackedLinkIsNotFollowed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no /dev/zero")
	}
	ctx := context.Background()
	dir := repo(t)
	must(t, os.Symlink("/dev/zero", filepath.Join(dir, "zero")))
	done := make(chan Changes, 1)
	go func() {
		c, _, _ := Status(ctx, dir)
		done <- c
	}()
	select {
	case c := <-done:
		eq(t, len(c.Files), 1)
		eq(t, c.Files[0].Additions, 0)
	case <-time.After(10 * time.Second):
		t.Fatal("Status read through a link to /dev/zero")
	}
}
