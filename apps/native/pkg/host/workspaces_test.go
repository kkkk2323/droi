package host

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kkkk2323/droi/apps/native/v2/pkg/gateway"
)

func TestAWorktreesNameIsItsFirstWords(t *testing.T) {
	for in, want := range map[string]string{
		"Fix the flaky login test": "fix-the-flaky-login-test",
		"  修复 bug #12!":            "bug-12",
		"":                         "session",
		"中文":                       "session",
	} {
		if got := WorktreeSlug(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
	if got := WorktreeSlug(strings.Repeat("word ", 30)); len(got) > 41 || strings.HasSuffix(got, "-") {
		t.Errorf("a long one: %q", got)
	}
}

func gitRepoForTest(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "web"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "web", "index.html"), []byte("<p>\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"add", "."},
		{"-c", "user.email=t@example.com", "-c", "user.name=T", "commit", "-q", "-m", "first"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestTheGatewaysWorkspacesAreThisComputersFoldersAndGit(t *testing.T) {
	ctx := context.Background()
	repo := gitRepoForTest(t)
	root := t.TempDir()
	ws := workspaces{root: func() string { return root }}

	got, err := ws.Inspect(ctx, repo)
	if err != nil || !got.IsGitRepository || got.CurrentBranch != "main" || len(got.Branches) != 1 {
		t.Fatalf("a repository: %+v %v", got, err)
	}
	plain := t.TempDir()
	if got, err := ws.Inspect(ctx, plain); err != nil || got.IsGitRepository || got.Path == "" {
		t.Fatalf("a plain folder: %+v %v", got, err)
	}
	var refused *gateway.NotAWorkspace
	if _, err := ws.Inspect(ctx, filepath.Join(plain, "missing")); !errors.As(err, &refused) {
		t.Fatalf("a missing folder: %v", err)
	}

	changes, err := ws.Changes(ctx, repo)
	if err != nil || !changes.IsGitRepository || changes.Additions != 2 || len(changes.Files) != 1 || changes.Files[0].Status != "untracked" {
		t.Fatalf("changes: %+v %v", changes, err)
	}
	if changes, err := ws.Changes(ctx, plain); err != nil || changes.IsGitRepository {
		t.Fatalf("a plain folder's changes: %+v %v", changes, err)
	}

	made, err := ws.AddWorktree(ctx, filepath.Join(repo, "web"), "", "Fix the login")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(made.Branch, "droi/fix-the-login-") || filepath.Base(made.Path) != "web" || realPath(made.RepoRoot) != realPath(repo) {
		t.Fatalf("made %+v", made)
	}
	if !strings.HasPrefix(realPath(made.Path), realPath(root)) {
		t.Fatalf("the worktree is not under the root: %s", made.Path)
	}
	if _, err := ws.AddWorktree(ctx, plain, "", "x"); !errors.As(err, &refused) {
		t.Fatalf("a folder outside a repository: %v", err)
	}
}
