// Package gitutil is what Droi asks of git on this computer: a working tree's branch and changes, a repository's
// branches, and the worktrees the Host makes for conversations. It runs the git executable, never a shell, with a
// timeout, and with no prompt that would wait for a person (a credential, an editor).
//
// It decides nothing on its own: removing a worktree, or a branch, is something a caller asks for after a person
// confirmed it. Nothing here removes a worktree because there are many of them.
package gitutil

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Timeout bounds one git command that stays on this computer. A command that reaches a remote has RemoteTimeout.
const (
	Timeout       = 15 * time.Second
	RemoteTimeout = 60 * time.Second
)

// ErrNoGit is the answer when no git executable can be found.
var ErrNoGit = errors.New("git is not installed or not on PATH")

// Error is a git command's failure: what git said on stderr, wrapping its exit.
type Error struct {
	Args   []string
	Stderr string
	Err    error
}

func (e *Error) Error() string {
	if e.Stderr != "" {
		return e.Stderr
	}
	return "git " + strings.Join(e.Args, " ") + ": " + e.Err.Error()
}

func (e *Error) Unwrap() error { return e.Err }

// ExitCode is the command's exit status, -1 when it did not run to an exit.
func (e *Error) ExitCode() int {
	var exit *exec.ExitError
	if errors.As(e.Err, &exit) {
		return exit.ExitCode()
	}
	return -1
}

var (
	pathMu  sync.Mutex
	gitPath string
)

// Path is the git executable: the one on PATH, or on Windows one of Git for Windows' usual places when PATH does
// not have it (an app started from the Start menu may not inherit the PATH a terminal has). Only a git found is
// remembered, so one installed while Droi runs is found at the next call.
func Path() (string, error) {
	pathMu.Lock()
	defer pathMu.Unlock()
	if gitPath != "" {
		return gitPath, nil
	}
	if p, err := exec.LookPath("git"); err == nil {
		gitPath = p
		return p, nil
	}
	for _, p := range candidates(os.Getenv) {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			gitPath = p
			return p, nil
		}
	}
	return "", ErrNoGit
}

// run runs git in dir and answers its standard output, untrimmed.
func run(ctx context.Context, timeout time.Duration, dir string, args ...string) ([]byte, error) {
	git, err := Path()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	full := append([]string{"-C", dir, "-c", "core.quotepath=off"}, args...)
	cmd := exec.CommandContext(ctx, git, full...)
	// No prompt can be answered here: a credential helper or an editor that waits would hold the call to its
	// timeout. C keeps git's own words stable for the few places that read them.
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_EDITOR=true", "LC_ALL=C")
	hideConsole(cmd)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, &Error{Args: args, Stderr: strings.TrimSpace(stderr.String()), Err: err}
	}
	return stdout.Bytes(), nil
}

// text is run's output, trimmed.
func text(ctx context.Context, dir string, args ...string) (string, error) {
	out, err := run(ctx, Timeout, dir, args...)
	return strings.TrimSpace(string(out)), err
}

// IsRepo reports whether dir is inside the working tree of a git repository.
func IsRepo(ctx context.Context, dir string) bool {
	out, err := text(ctx, dir, "rev-parse", "--is-inside-work-tree")
	return err == nil && out == "true"
}

// TopLevel is the top folder of dir's working tree.
func TopLevel(ctx context.Context, dir string) (string, error) {
	out, err := text(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return fromGitPath(out), nil
}

// fromGitPath is a path as git prints it, in this system's form: git for Windows prints forward slashes.
func fromGitPath(p string) string { return filepath.FromSlash(p) }

// Branches is a repository's local branches and the one checked out.
type Branches struct {
	// Current is the branch checked out; "" on a detached HEAD or before the first commit has a branch name.
	Current  string
	Detached bool
	Names    []string
}

// ListBranches answers dir's local branches. ok is false when dir is not in a git repository.
func ListBranches(ctx context.Context, dir string) (b Branches, ok bool, err error) {
	if !IsRepo(ctx, dir) {
		return Branches{}, false, nil
	}
	current, err := text(ctx, dir, "symbolic-ref", "--short", "-q", "HEAD")
	if err != nil {
		var gerr *Error
		if !errors.As(err, &gerr) || gerr.ExitCode() != 1 {
			return Branches{}, true, err
		}
		b.Detached = true
	}
	b.Current = current
	out, err := text(ctx, dir, "for-each-ref", "--format=%(refname:lstrip=2)", "--sort=-committerdate", "refs/heads")
	if err != nil {
		return b, true, err
	}
	for _, name := range strings.Split(out, "\n") {
		if name != "" {
			b.Names = append(b.Names, name)
		}
	}
	return b, true, nil
}

// HasBranch reports that dir's repository has the local branch name.
func HasBranch(ctx context.Context, dir, name string) bool {
	_, err := run(ctx, Timeout, dir, "show-ref", "--verify", "--quiet", "refs/heads/"+name)
	return err == nil
}
