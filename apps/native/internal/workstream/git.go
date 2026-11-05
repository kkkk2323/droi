package workstream

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"
)

// gitTimeout bounds one git command.
const gitTimeout = 15 * time.Second

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
			return "", errors.New(strings.TrimSpace(string(exit.Stderr)))
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// isGitRepo reports that dir is in a git repository's work tree.
func isGitRepo(ctx context.Context, dir string) bool {
	out, err := git(ctx, dir, "rev-parse", "--is-inside-work-tree")
	return err == nil && out == "true"
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
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", worktree, "merge-base", "--is-ancestor", "HEAD", base)
	hideConsole(cmd)
	err = cmd.Run()
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
