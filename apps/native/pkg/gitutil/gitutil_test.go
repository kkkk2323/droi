package gitutil

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
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
	eq(t, in, Inspection{Branch: "droi/fix-login", LocalOnlyCommits: 1, HasRemoteBranch: false})
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
