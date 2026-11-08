package gitutil

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// AddTimeout bounds making a worktree: a checkout of a whole tree, which a large repository or a virus scanner
// makes slow.
const AddTimeout = 10 * time.Minute

// Worktree is one working tree of a repository, as git lists it.
type Worktree struct {
	Path string
	// Head is the commit checked out; Branch the branch, "" when detached.
	Head, Branch string
	Detached     bool
	// Main is the repository's own working tree, which is never removed as a worktree.
	Main bool
	// Locked and Prunable carry git's reason, "" when there is none. A prunable worktree's folder is gone.
	Locked, Prunable string
}

// Worktrees lists the working trees of dir's repository, its own first.
func Worktrees(ctx context.Context, dir string) ([]Worktree, error) {
	sep := "\x00"
	out, err := run(ctx, Timeout, dir, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		// -z came with git 2.36; an older git lists one attribute per line, which a path with a newline in it
		// would confuse, and nothing else does.
		var gerr *Error
		if !errors.As(err, &gerr) || gerr.ExitCode() != 129 {
			return nil, err
		}
		if out, err = run(ctx, Timeout, dir, "worktree", "list", "--porcelain"); err != nil {
			return nil, err
		}
		sep = "\n"
	}
	var list []Worktree
	var cur *Worktree
	for _, field := range strings.Split(string(out), sep) {
		key, value, _ := strings.Cut(field, " ")
		if key != "worktree" && cur == nil {
			continue
		}
		switch key {
		case "worktree":
			list = append(list, Worktree{Path: fromGitPath(value), Main: len(list) == 0})
			cur = &list[len(list)-1]
		case "HEAD":
			cur.Head = value
		case "branch":
			cur.Branch = strings.TrimPrefix(value, "refs/heads/")
		case "detached":
			cur.Detached = true
		case "locked":
			cur.Locked = orWord(value, "locked")
		case "prunable":
			cur.Prunable = orWord(value, "prunable")
		}
	}
	return list, nil
}

func orWord(reason, word string) string {
	if reason == "" {
		return word
	}
	return reason
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
// A worktree that could not be made is taken away again, with the branch it made, so the same call can be tried
// again.
func AddWorktree(ctx context.Context, o AddOptions) error {
	if o.Branch == "" || o.Path == "" {
		return errors.New("a worktree needs a path and a branch")
	}
	if err := checkBranchName(ctx, o.Repo, o.Branch); err != nil {
		return err
	}
	if HasBranch(ctx, o.Repo, o.Branch) {
		return ErrBranchExists
	}
	args := []string{"worktree", "add", "-b", o.Branch, "--", filepath.ToSlash(o.Path)}
	if o.Base != "" {
		args = append(args, o.Base)
	}
	_, err := run(ctx, AddTimeout, o.Repo, args...)
	if err == nil {
		return nil
	}
	// A checkout cut short leaves a locked worktree and the new branch behind; neither is anyone's work yet.
	clean := context.WithoutCancel(ctx)
	if list, lerr := Worktrees(clean, o.Repo); lerr == nil {
		for _, w := range list {
			if !w.Main && samePath(w.Path, o.Path) {
				_, _ = run(clean, Timeout, o.Repo, "worktree", "remove", "--force", "--force", filepath.ToSlash(w.Path))
			}
		}
	}
	_, _ = run(clean, Timeout, o.Repo, "worktree", "prune")
	if HasBranch(clean, o.Repo, o.Branch) {
		_, _ = run(clean, Timeout, o.Repo, "branch", "-D", "--", o.Branch)
	}
	return err
}

// checkBranchName refuses a name git would read as something else: an option, or not a branch name at all.
func checkBranchName(ctx context.Context, repo, name string) error {
	if strings.HasPrefix(name, "-") {
		return errors.New(name + " is not a branch name")
	}
	if _, err := run(ctx, Timeout, repo, "check-ref-format", "--branch", name); err != nil {
		return errors.New(name + " is not a branch name")
	}
	return nil
}

// Inspection is what removing a worktree would lose.
type Inspection struct {
	Branch string
	// ChangedFiles are tracked files changed against HEAD, staged or not; UntrackedFiles the files git does not
	// know; IgnoredFiles the files and folders .gitignore hides, such as .env or node_modules, which go with the
	// worktree too.
	ChangedFiles, UntrackedFiles, IgnoredFiles int
	// LocalOnlyCommits are the commits of the worktree's HEAD that no other branch here and no remote branch this
	// computer knows of has; -1 when that could not be counted.
	LocalOnlyCommits int
	// HasRemoteBranch says origin has a branch of the same name, as of the last fetch.
	HasRemoteBranch bool
}

// Unsaved reports that the worktree holds something nothing else has.
func (in Inspection) Unsaved() bool {
	return in.ChangedFiles+in.UntrackedFiles+in.IgnoredFiles > 0 || in.LocalOnlyCommits > 0
}

// Inspect reads what is in the worktree at path that removing it would lose. It stays on this computer: what the
// remote has is what the last fetch saw.
func Inspect(ctx context.Context, path string) (Inspection, error) {
	if _, err := os.Stat(path); err != nil {
		return Inspection{}, err
	}
	out, err := run(ctx, Timeout, path, "status", "--porcelain=v2", "-z", "--branch", "--untracked-files=all", "--ignored=traditional")
	if err != nil {
		return Inspection{}, err
	}
	in := Inspection{LocalOnlyCommits: -1}
	records := strings.Split(string(out), "\x00")
	for i := 0; i < len(records); i++ {
		rec := records[i]
		switch {
		case strings.HasPrefix(rec, "# branch.head "):
			if name := strings.TrimPrefix(rec, "# branch.head "); name != "(detached)" {
				in.Branch = name
			}
		case strings.HasPrefix(rec, "1 "), strings.HasPrefix(rec, "u "):
			in.ChangedFiles++
		case strings.HasPrefix(rec, "2 "):
			in.ChangedFiles++
			i++ // the original path
		case strings.HasPrefix(rec, "? "):
			in.UntrackedFiles++
		case strings.HasPrefix(rec, "! "):
			in.IgnoredFiles++
		}
	}
	args := []string{"rev-list", "--count", "HEAD", "--not"}
	if in.Branch != "" {
		// The branch itself would count as having its own commits. With --branches git matches the pattern without
		// refs/heads/, and a branch name holds no glob character.
		args = append(args, "--exclude="+in.Branch)
	}
	args = append(args, "--branches", "--remotes")
	if n, err := text(ctx, path, args...); err == nil {
		in.LocalOnlyCommits, _ = strconv.Atoi(n)
	}
	if in.Branch != "" {
		_, err := run(ctx, Timeout, path, "show-ref", "--verify", "--quiet", "refs/remotes/origin/"+in.Branch)
		in.HasRemoteBranch = err == nil
	}
	return in, nil
}

// BranchLocalOnlyCommits counts the commits of the local branch that no other branch here and no remote branch this
// computer knows of has: what deleting the branch would lose. It reads the repository, so it answers for a worktree
// whose folder is gone. -1 when it could not be counted.
func BranchLocalOnlyCommits(ctx context.Context, repo, branch string) int {
	if branch == "" || strings.HasPrefix(branch, "-") {
		return -1
	}
	n, err := text(ctx, repo, "rev-list", "--count", "refs/heads/"+branch, "--not", "--exclude="+branch, "--branches", "--remotes")
	if err != nil {
		return -1
	}
	count, err := strconv.Atoi(n)
	if err != nil {
		return -1
	}
	return count
}

// RemoveOptions is a worktree to remove, and what goes with it. A caller sets them only after a person confirmed.
type RemoveOptions struct {
	// Repo is any folder of the repository; "" is Path, which then must still exist.
	Repo string
	Path string
	// Force removes a worktree that holds changes, untracked or ignored files, or is locked; without it such a
	// worktree is kept.
	Force bool
	// DeleteLocalBranch and DeleteRemoteBranch remove the worktree's branch here and on origin. Origin's is
	// deleted only when it has nothing this branch does not.
	DeleteLocalBranch, DeleteRemoteBranch bool
}

// RemoveResult is what RemoveWorktree did.
type RemoveResult struct {
	Removed bool
	// KeptForChanges says the worktree was kept because it holds something and Force was not set.
	KeptForChanges bool
	// Warnings are what went wrong after the worktree was removed: a branch that could not, or must not, be deleted.
	Warnings []string
}

// RemoveWorktree removes the worktree at path, and its branch when asked. The repository's own working tree is
// never removed. A worktree whose folder is already gone is removed from git's list.
func RemoveWorktree(ctx context.Context, o RemoveOptions) (RemoveResult, error) {
	repo := o.Repo
	if repo == "" {
		repo = o.Path
	}
	list, err := Worktrees(ctx, repo)
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
	_, statErr := os.Stat(target.Path)
	gone := target.Prunable != "" || errors.Is(statErr, os.ErrNotExist)
	if !o.Force && !gone {
		in, err := Inspect(ctx, target.Path)
		if err != nil {
			return RemoveResult{}, err
		}
		if in.ChangedFiles+in.UntrackedFiles+in.IgnoredFiles > 0 || target.Locked != "" {
			return RemoveResult{KeptForChanges: true}, nil
		}
	}
	// The branch's tip, read while the branch is there: origin's branch is compared with it.
	localTip := ""
	if target.Branch != "" {
		localTip, _ = text(ctx, main.Path, "rev-parse", "--verify", "-q", "refs/heads/"+target.Branch)
	}
	args := []string{"worktree", "remove"}
	if o.Force || gone {
		// Twice: once for changes, once for a lock, which a checkout cut short leaves.
		args = append(args, "--force", "--force")
	}
	// From the main working tree: the worktree being removed is not a place to run git from.
	if _, err := run(ctx, Timeout, main.Path, append(args, filepath.ToSlash(target.Path))...); err != nil {
		return RemoveResult{}, err
	}
	res := RemoveResult{Removed: true}
	if target.Branch == "" {
		return res, nil
	}
	if o.DeleteRemoteBranch {
		if warning := deleteRemoteBranch(ctx, main.Path, target.Branch, localTip); warning != "" {
			res.Warnings = append(res.Warnings, warning)
		}
	}
	if o.DeleteLocalBranch {
		if _, err := run(ctx, Timeout, main.Path, "branch", "-D", "--", target.Branch); err != nil {
			res.Warnings = append(res.Warnings, "the local branch "+target.Branch+" was kept: "+err.Error())
		}
	}
	return res, nil
}

// deleteRemoteBranch deletes origin's branch of that name when it holds nothing localTip does not: a branch of the
// same name with other commits is someone else's work, and stays. It answers a warning, "" when all went well.
func deleteRemoteBranch(ctx context.Context, repo, branch, localTip string) string {
	out, err := run(ctx, RemoteTimeout, repo, "ls-remote", "--heads", "origin", "refs/heads/"+branch)
	if err != nil {
		return "the origin branch " + branch + " was kept: " + err.Error()
	}
	remoteTip, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\t")
	if remoteTip == "" {
		return ""
	}
	if localTip == "" || remoteTip != localTip {
		if _, err := run(ctx, Timeout, repo, "merge-base", "--is-ancestor", remoteTip, localTip); localTip == "" || err != nil {
			return "the origin branch " + branch + " was kept: it has commits this branch does not"
		}
	}
	// The lease makes the delete fail if origin's branch moved since it was read.
	if _, err := run(ctx, RemoteTimeout, repo, "push", "--force-with-lease=refs/heads/"+branch+":"+remoteTip, "origin", "--delete", "refs/heads/"+branch); err != nil {
		return "the origin branch " + branch + " was kept: " + err.Error()
	}
	return ""
}

// samePath reports that two paths of this computer are one folder: through symbolic links, and in the case a
// case-insensitive volume ignores. A path that no longer exists is compared as written.
func samePath(a, b string) bool {
	if ia, err := os.Stat(a); err == nil {
		if ib, err := os.Stat(b); err == nil {
			return os.SameFile(ia, ib)
		}
	}
	if ea, err := filepath.EvalSymlinks(a); err == nil {
		a = ea
	}
	if eb, err := filepath.EvalSymlinks(b); err == nil {
		b = eb
	}
	return filepath.Clean(a) == filepath.Clean(b)
}
