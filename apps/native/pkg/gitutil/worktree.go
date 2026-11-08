package gitutil

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
)

// Worktree is one working tree of a repository, as git lists it.
type Worktree struct {
	Path string
	// Head is the commit checked out; Branch the branch, "" when detached.
	Head, Branch string
	Detached     bool
	// Main is the repository's own working tree, which is never removed as a worktree.
	Main bool
	// Locked and Prunable carry git's reason, "" when there is none.
	Locked, Prunable string
}

// Worktrees lists the working trees of dir's repository, its own first.
func Worktrees(ctx context.Context, dir string) ([]Worktree, error) {
	out, err := run(ctx, Timeout, dir, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return nil, err
	}
	var list []Worktree
	var cur *Worktree
	for _, field := range strings.Split(string(out), "\x00") {
		key, value, _ := strings.Cut(field, " ")
		switch key {
		case "worktree":
			list = append(list, Worktree{Path: fromGitPath(value), Main: len(list) == 0})
			cur = &list[len(list)-1]
		case "HEAD":
			if cur != nil {
				cur.Head = value
			}
		case "branch":
			if cur != nil {
				cur.Branch = strings.TrimPrefix(value, "refs/heads/")
			}
		case "detached":
			if cur != nil {
				cur.Detached = true
			}
		case "locked":
			if cur != nil {
				cur.Locked = "locked"
				if value != "" {
					cur.Locked = value
				}
			}
		case "prunable":
			if cur != nil {
				cur.Prunable = "prunable"
				if value != "" {
					cur.Prunable = value
				}
			}
		}
	}
	return list, nil
}

// AddOptions is a new worktree: where, on which new branch, and from what.
type AddOptions struct {
	// Repo is any folder of the repository.
	Repo string
	// Path is the new worktree's folder; it must not exist, or be empty.
	Path string
	// Branch is the new branch the worktree checks out.
	Branch string
	// Base is what the branch starts from: a branch, a tag or a commit; "" is the repository's HEAD.
	Base string
}

// ErrBranchExists is AddWorktree's answer when the branch it was asked to make is already there.
var ErrBranchExists = errors.New("the branch already exists")

// AddWorktree makes a worktree on a new branch. It never reuses a branch: a branch of that name is someone's work.
func AddWorktree(ctx context.Context, o AddOptions) error {
	if o.Branch == "" || o.Path == "" {
		return errors.New("a worktree needs a path and a branch")
	}
	if HasBranch(ctx, o.Repo, o.Branch) {
		return ErrBranchExists
	}
	args := []string{"worktree", "add", "-b", o.Branch, filepath.ToSlash(o.Path)}
	if o.Base != "" {
		args = append(args, o.Base)
	}
	_, err := run(ctx, Timeout, o.Repo, args...)
	return err
}

// Inspection is what removing a worktree would lose.
type Inspection struct {
	Branch string
	// ChangedFiles are tracked files changed against HEAD, staged or not; UntrackedFiles the files git does not know.
	ChangedFiles, UntrackedFiles int
	// LocalOnlyCommits are the branch's commits on no remote branch this computer knows of; -1 when that could not
	// be counted.
	LocalOnlyCommits int
	// HasRemoteBranch says origin has a branch of the same name, as of the last fetch.
	HasRemoteBranch bool
}

// Unsaved reports that the worktree holds work nothing else has.
func (in Inspection) Unsaved() bool {
	return in.ChangedFiles+in.UntrackedFiles > 0 || in.LocalOnlyCommits > 0
}

// Inspect reads what is in the worktree at path that removing it would lose. It stays on this computer: what the
// remote has is what the last fetch saw.
func Inspect(ctx context.Context, path string) (Inspection, error) {
	c, ok, err := Status(ctx, path)
	if err != nil {
		return Inspection{}, err
	}
	if !ok {
		return Inspection{}, errors.New(path + " is not a git working tree")
	}
	in := Inspection{Branch: c.Branch, LocalOnlyCommits: -1}
	for _, f := range c.Files {
		if f.Status == "untracked" {
			in.UntrackedFiles++
		} else {
			in.ChangedFiles++
		}
	}
	if n, err := text(ctx, path, "rev-list", "--count", "HEAD", "--not", "--remotes"); err == nil {
		in.LocalOnlyCommits, _ = strconv.Atoi(n)
	}
	if in.Branch != "" {
		_, err := run(ctx, Timeout, path, "show-ref", "--verify", "--quiet", "refs/remotes/origin/"+in.Branch)
		in.HasRemoteBranch = err == nil
	}
	return in, nil
}

// RemoveOptions is a worktree to remove, and what goes with it. A caller sets them only after a person confirmed.
type RemoveOptions struct {
	Path string
	// Force removes a worktree that holds changes; without it such a worktree is kept.
	Force bool
	// DeleteLocalBranch and DeleteRemoteBranch remove the worktree's branch here and on origin.
	DeleteLocalBranch, DeleteRemoteBranch bool
}

// RemoveResult is what RemoveWorktree did.
type RemoveResult struct {
	Removed bool
	// KeptForChanges says the worktree was kept because it holds changes and Force was not set.
	KeptForChanges bool
	// Warnings are what went wrong after the worktree was removed: a branch that could not be deleted.
	Warnings []string
}

// RemoveWorktree removes the worktree at path, and its branch when asked. The repository's own working tree is
// never removed.
func RemoveWorktree(ctx context.Context, o RemoveOptions) (RemoveResult, error) {
	list, err := Worktrees(ctx, o.Path)
	if err != nil {
		return RemoveResult{}, err
	}
	var target, main *Worktree
	for i := range list {
		if list[i].Main {
			main = &list[i]
		}
		if samePath(list[i].Path, o.Path) {
			target = &list[i]
		}
	}
	if target == nil {
		return RemoveResult{}, errors.New(o.Path + " is not a worktree of its repository")
	}
	if target.Main || main == nil {
		return RemoveResult{}, errors.New(o.Path + " is the repository itself, not a worktree")
	}
	if !o.Force {
		c, _, err := Status(ctx, o.Path)
		if err != nil {
			return RemoveResult{}, err
		}
		if !c.Clean() {
			return RemoveResult{KeptForChanges: true}, nil
		}
	}
	args := []string{"worktree", "remove", filepath.ToSlash(target.Path)}
	if o.Force {
		args = append(args, "--force")
	}
	// From the main working tree: the worktree being removed is not a place to run git from.
	if _, err := run(ctx, Timeout, main.Path, args...); err != nil {
		return RemoveResult{}, err
	}
	res := RemoveResult{Removed: true}
	if target.Branch == "" {
		return res, nil
	}
	if o.DeleteLocalBranch {
		if _, err := run(ctx, Timeout, main.Path, "branch", "-D", target.Branch); err != nil {
			res.Warnings = append(res.Warnings, "the local branch "+target.Branch+" was kept: "+err.Error())
		}
	}
	if o.DeleteRemoteBranch {
		if _, err := run(ctx, RemoteTimeout, main.Path, "push", "origin", "--delete", target.Branch); err != nil {
			res.Warnings = append(res.Warnings, "the origin branch "+target.Branch+" was kept: "+err.Error())
		}
	}
	return res, nil
}

// samePath compares two paths of this computer, through symbolic links (macOS's /var is /private/var).
func samePath(a, b string) bool {
	if ea, err := filepath.EvalSymlinks(a); err == nil {
		a = ea
	}
	if eb, err := filepath.EvalSymlinks(b); err == nil {
		b = eb
	}
	return filepath.Clean(a) == filepath.Clean(b)
}
