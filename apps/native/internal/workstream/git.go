package workstream

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// gitTimeout bounds one git command.
const gitTimeout = 15 * time.Second

// gitError is a git command's failure: its message, wrapping its exit.
type gitError struct {
	msg string
	err error
}

func (e *gitError) Error() string { return e.msg }
func (e *gitError) Unwrap() error { return e.err }

// git runs git in dir, without a shell, and answers its trimmed output.
func git(ctx context.Context, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	hideConsole(cmd)
	out, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && len(exit.Stderr) > 0 {
			return "", &gitError{msg: strings.TrimSpace(string(exit.Stderr)), err: err}
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// isGitRepo reports that dir is in the work tree of a git repository with a
// commit: one without has nothing to cut an agent's branch from.
func isGitRepo(ctx context.Context, dir string) bool {
	out, err := git(ctx, dir, "rev-parse", "--is-inside-work-tree")
	if err != nil || out != "true" {
		return false
	}
	_, err = git(ctx, dir, "rev-parse", "--verify", "-q", "HEAD")
	return err == nil
}

// currentBranch is dir's branch, or its commit when it has none checked out.
func currentBranch(ctx context.Context, dir string) string {
	out, err := git(ctx, dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err == nil && out != "HEAD" {
		return out
	}
	out, _ = git(ctx, dir, "rev-parse", "HEAD")
	return out
}

// repositoryOf is the top folder of dir's repository, and dir's folder in
// it, "" when dir is the top.
func repositoryOf(ctx context.Context, dir string) (repository, subfolder string) {
	prefix, err := git(ctx, dir, "rev-parse", "--show-prefix")
	if err != nil || prefix == "" {
		return dir, ""
	}
	subfolder = filepath.FromSlash(strings.TrimSuffix(prefix, "/"))
	// Cut from dir as the user gave it, which git may name otherwise (through
	// a symbolic link, say).
	if clean := filepath.Clean(dir); strings.HasSuffix(clean, string(filepath.Separator)+subfolder) {
		return strings.TrimSuffix(clean, string(filepath.Separator)+subfolder), subfolder
	}
	top, err := git(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return dir, ""
	}
	return filepath.FromSlash(top), subfolder
}

// hasBranch reports that dir's repository has the local branch name.
func hasBranch(ctx context.Context, dir, name string) bool {
	_, err := git(ctx, dir, "show-ref", "--verify", "--quiet", "refs/heads/"+name)
	return err == nil
}

// unmergedWork says what a worktree holds that its base does not: changes
// not committed, or commits not in base; "" for nothing, which includes a
// branch merged into base.
func unmergedWork(ctx context.Context, worktree, base string) (string, error) {
	status, err := git(ctx, worktree, "status", "--porcelain")
	if err != nil {
		return "", err
	}
	if status != "" {
		return "it has uncommitted changes", nil
	}
	if base == "" {
		return "its base branch is not known", nil
	}
	_, err = git(ctx, worktree, "merge-base", "--is-ancestor", "HEAD", base)
	var exit *exec.ExitError
	switch {
	case err == nil:
		return "", nil
	case errors.As(err, &exit) && exit.ExitCode() == 1:
		return "its branch has commits not merged into " + base, nil
	default:
		return "", err
	}
}
