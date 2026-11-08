package host

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/kkkk2323/droi/apps/native/pkg/gateway"
	"github.com/kkkk2323/droi/apps/native/pkg/gitutil"
	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
)

// WorktreeBranchPrefix starts the branch of every worktree the Host makes.
const WorktreeBranchPrefix = "droi/"

// WorktreeSlug is a branch and folder name from a session's first message: its first words, in lower case.
func WorktreeSlug(prompt string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(prompt) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
		if b.Len() >= 40 {
			break
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		return "session"
	}
	return slug
}

// UsableDir is the directory a conversation works in, or why path cannot be one. The Kernel runs on this computer,
// so the check is the file system's.
func UsableDir(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err == nil {
		var info os.FileInfo
		if info, err = os.Stat(abs); err == nil && !info.IsDir() {
			err = errors.New("not a directory")
		}
	}
	if err != nil {
		return "", &gateway.NotAWorkspace{Reason: l10n.L("%s is not a usable directory.", path)}
	}
	return abs, nil
}

// AddWorktree makes a worktree of workspace's repository under root, on a new branch from base ("" for HEAD), and
// answers the folder the session works in: the worktree, or the same subfolder of it when workspace is one of the
// repository. Its error is a *gateway.NotAWorkspace when workspace is in no repository.
func AddWorktree(ctx context.Context, root, workspace, base, prompt string) (gateway.WorktreeMade, error) {
	top, err := gitutil.TopLevel(ctx, workspace)
	if err != nil {
		return gateway.WorktreeMade{}, &gateway.NotAWorkspace{Reason: l10n.L("%s is not in a git repository.", workspace)}
	}
	slug := WorktreeSlug(prompt)
	for range 5 {
		suffix := make([]byte, 2)
		_, _ = rand.Read(suffix)
		name := slug + "-" + hex.EncodeToString(suffix)
		dir := filepath.Join(root, filepath.Base(top), name)
		if _, err := os.Stat(dir); err == nil {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
			return gateway.WorktreeMade{}, errors.New(l10n.L("Could not make the worktree: %s", err))
		}
		branch := WorktreeBranchPrefix + name
		err := gitutil.AddWorktree(ctx, gitutil.AddOptions{Repo: top, Path: dir, Branch: branch, Base: base})
		if errors.Is(err, gitutil.ErrBranchExists) {
			continue
		}
		if err != nil {
			return gateway.WorktreeMade{}, errors.New(l10n.L("Could not make the worktree: %s", err))
		}
		made := gateway.WorktreeMade{Path: dir, Branch: branch, RepoRoot: top}
		if rel, err := filepath.Rel(realPath(top), realPath(workspace)); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			if info, err := os.Stat(filepath.Join(dir, rel)); err == nil && info.IsDir() {
				made.Path = filepath.Join(dir, rel)
			}
		}
		return made, nil
	}
	return gateway.WorktreeMade{}, errors.New(l10n.L("Could not make the worktree: %s", l10n.L("every name tried is taken")))
}

// realPath is path with its symbolic links followed, as written when it does not exist.
func realPath(path string) string {
	if p, err := filepath.EvalSymlinks(path); err == nil {
		return p
	}
	return filepath.Clean(path)
}

// workspaces is the Gateway's Workspaces on this computer: the file system and git.
type workspaces struct {
	// root is the folder worktrees go in.
	root func() string
}

func (w workspaces) Inspect(ctx context.Context, path string) (gateway.Workspace, error) {
	dir, err := UsableDir(path)
	if err != nil {
		return gateway.Workspace{}, err
	}
	out := gateway.Workspace{Path: dir}
	b, ok, err := gitutil.ListBranches(ctx, dir)
	if err != nil || !ok {
		return out, nil
	}
	out.IsGitRepository, out.Branches, out.CurrentBranch = true, b.Names, b.Current
	return out, nil
}

func (w workspaces) Changes(ctx context.Context, path string) (gateway.GitChanges, error) {
	c, ok, err := gitutil.Status(ctx, path)
	if err != nil || !ok {
		return gateway.GitChanges{}, err
	}
	out := gateway.GitChanges{IsGitRepository: true, Branch: c.Branch, Detached: c.Detached, Additions: c.Additions, Deletions: c.Deletions}
	for _, f := range c.Files {
		out.Files = append(out.Files, gateway.GitChangeFile{Path: f.Path, Status: f.Status, Additions: f.Additions, Deletions: f.Deletions})
	}
	return out, nil
}

func (w workspaces) AddWorktree(ctx context.Context, path, base, prompt string) (gateway.WorktreeMade, error) {
	dir, err := UsableDir(path)
	if err != nil {
		return gateway.WorktreeMade{}, err
	}
	return AddWorktree(ctx, w.root(), dir, base, prompt)
}
