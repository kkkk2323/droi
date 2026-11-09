package app

// Worktrees on Kernel (M4 4N): the Host makes and removes them with git on this computer (pkg/gitutil). There is no
// lifecycle and no limit that deletes one on its own: a worktree goes only when a person deletes it in Settings, after
// seeing what it holds. Another computer's stay its Daemon's.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/kkkk2323/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/v2/pkg/gitutil"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/host"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/l10n"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/sessions"
)

// worktreesHere reports that this window's worktrees are the Host's own, made with git on this computer.
func (a *App) worktreesHere() bool {
	return a.onKernel() && !a.following()
}

// worktreeRoot is the folder the Host makes worktrees in: Settings → Worktrees' directory, else the worktrees
// folder of the Factory folder, as the droid CLI makes them.
func (a *App) worktreeRoot() (string, error) {
	base := ""
	if dv := a.sessionDefaults(); dv != nil {
		// A folder that is no full path would put worktrees wherever Droi runs from.
		if filepath.IsAbs(dv.WorktreeDirectory) {
			return dv.WorktreeDirectory, nil
		}
		base = dv.UserFactoryDir
	}
	if !filepath.IsAbs(base) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".factory")
	}
	return filepath.Join(base, "worktrees"), nil
}

// addWorktree makes a worktree of workspace's repository on a new branch from base ("" for HEAD), and answers the
// folder the session works in (host.AddWorktree).
func (a *App) addWorktree(ctx context.Context, workspace, base, prompt string) (string, error) {
	root, err := a.worktreeRoot()
	if err != nil {
		return "", err
	}
	made, err := host.AddWorktree(ctx, root, workspace, base, prompt)
	return made.Path, err
}

// discardWorktree takes away a worktree addWorktree just made, with its branch, when the conversation it was made
// for could not be created: nothing is in it yet, and no lifecycle would ever collect it.
func discardWorktree(ctx context.Context, cwd string) {
	top, err := gitutil.TopLevel(ctx, cwd)
	if err != nil {
		return
	}
	_, _ = gitutil.RemoveWorktree(ctx, gitutil.RemoveOptions{Path: top, Force: true, DeleteLocalBranch: true})
}

// hereWorktree is a worktree of a repository a Session works in, for Settings → Worktrees.
type hereWorktree struct {
	gitutil.Worktree
	Repo string
	// Sessions are the listed Sessions that work in it.
	Sessions []sessions.Summary
}

// inFolder reports that path is dir or inside it, through symbolic links: git names a folder by where it really
// is (/private/var on macOS), a conversation by the path it was given.
func inFolder(path, dir string) bool {
	path, dir = realPath(path), realPath(dir)
	return path == dir || strings.HasPrefix(path, dir+string(filepath.Separator))
}

// realPath is path with its symbolic links followed, as written when it does not exist.
func realPath(path string) string {
	if p, err := filepath.EvalSymlinks(path); err == nil {
		return p
	}
	return filepath.Clean(path)
}

// readHereWorktrees lists the worktrees of every repository a listed Session works in, but the repositories' own.
func readHereWorktrees(ctx context.Context, listed []sessions.Summary) ([]hereWorktree, error) {
	var repos []string
	seenRepo, seenCwd := map[string]bool{}, map[string]bool{}
	add := func(top string) {
		if top != "" && !seenRepo[top] {
			seenRepo[top] = true
			repos = append(repos, top)
		}
	}
	for _, s := range listed {
		// The Kernel says which repository a conversation works in; git is asked only where it did not.
		if s.RepoRoot != "" {
			add(s.RepoRoot)
			continue
		}
		if s.Cwd == "" || seenCwd[s.Cwd] || sessions.IsScratchSession(s) {
			continue
		}
		seenCwd[s.Cwd] = true
		if _, err := os.Stat(s.Cwd); err != nil {
			continue
		}
		if top, err := gitutil.TopLevel(ctx, s.Cwd); err == nil {
			add(top)
		}
	}
	var out []hereWorktree
	var firstErr error
	for _, repo := range repos {
		list, err := gitutil.Worktrees(ctx, repo)
		if err != nil {
			firstErr = cmpErr(firstErr, err)
			continue
		}
		var main string
		for _, w := range list {
			if w.Main {
				main = w.Path
			}
		}
		for _, w := range list {
			// A worktree is listed once, under the repository whose own working tree it is not.
			if w.Main || slices.ContainsFunc(out, func(x hereWorktree) bool { return x.Path == w.Path }) {
				continue
			}
			hw := hereWorktree{Worktree: w, Repo: main}
			for _, s := range listed {
				if s.Cwd != "" && inFolder(s.Cwd, w.Path) {
					hw.Sessions = append(hw.Sessions, s)
				}
			}
			out = append(out, hw)
		}
	}
	if len(out) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return out, nil
}

func cmpErr(first, next error) error {
	if first != nil {
		return first
	}
	return next
}

// loadHereWorktrees reads Settings → Worktrees' list from git, off the frame.
func (a *App) loadHereWorktrees() {
	m := &a.settings.worktrees
	if m.loading || (!m.readAt.IsZero() && time.Since(m.readAt) < 30*time.Second) {
		return
	}
	m.loading, m.readAt = true, time.Now()
	a.mu.Lock()
	listed := slices.Clone(a.listed)
	a.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, time.Minute)
		defer cancel()
		list, err := readHereWorktrees(ctx, listed)
		a.cfg.Update(func() {
			m.loading = false
			if err != nil {
				m.err = l10n.N("Failed to load worktrees.")
				return
			}
			m.err, m.here = "", list
			if m.here == nil {
				m.here = []hereWorktree{}
			}
		})
	}()
}

// askDeleteHere opens the dialog for deleting one of the Host's worktrees. The delete is forced, so the dialog says
// all it would lose: what the folder holds, or, when the folder is gone, the commits only its branch has. When that
// cannot be read the dialog deletes nothing.
func (a *App) askDeleteHere(w hereWorktree) {
	a.worktreeAsk = worktreeDialog{open: true, path: w.Path, branch: w.Branch, sessions: len(w.Sessions), repo: w.Repo, archive: w.Sessions, locked: w.Locked}
	path := w.Path
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, time.Minute)
		defer cancel()
		res := &protocol.InspectWorktreeDeletionResult{WorktreePath: path, Branch: w.Branch}
		ignored := 0
		_, statErr := os.Stat(path)
		gone := w.Prunable != "" || errors.Is(statErr, os.ErrNotExist)
		var err error
		if gone {
			if w.Branch != "" {
				if n := gitutil.BranchLocalOnlyCommits(ctx, w.Repo, w.Branch); n >= 0 {
					count := float64(n)
					res.LocalOnlyCommits = &count
				} else {
					err = errors.New("the branch's commits could not be counted")
				}
			}
		} else {
			var in gitutil.Inspection
			if in, err = gitutil.Inspect(ctx, path); err == nil {
				res.Branch, res.ChangedFiles, res.UntrackedFiles, res.HasRemoteBranch = in.Branch, float64(in.ChangedFiles), float64(in.UntrackedFiles), in.HasRemoteBranch
				if in.LocalOnlyCommits >= 0 {
					count := float64(in.LocalOnlyCommits)
					res.LocalOnlyCommits = &count
				} else {
					err = errors.New("the worktree's commits could not be counted")
				}
				ignored = in.IgnoredFiles
			}
		}
		a.cfg.Update(func() {
			d := &a.worktreeAsk
			if d.path != path {
				return
			}
			d.checked, d.gone = true, gone
			if err != nil {
				d.inspectErr = err.Error()
				return
			}
			d.inspect, d.ignored = res, ignored
		})
	}()
}

// deleteHere removes one of the Host's worktrees with git, and archives the Sessions that worked in it. It is only
// called once a person saw what the worktree holds, so whatever it holds goes with it.
func (a *App) deleteHere(d worktreeDialog) {
	ctx, cancel := context.WithTimeout(a.ctx, 3*time.Minute)
	res, err := gitutil.RemoveWorktree(ctx, gitutil.RemoveOptions{
		Repo: d.repo, Path: d.path, Force: true, DeleteLocalBranch: d.deleteLocal, DeleteRemoteBranch: d.deleteRemote,
	})
	cancel()
	msg, notice := "", ""
	switch {
	case err != nil:
		msg = L("Failed to delete worktree: %s", err.Error())
	case !res.Removed:
		msg = L("Worktree could not be removed. Remove it manually with git worktree remove.")
	case len(res.Warnings) > 0:
		notice = L("Worktree deleted with warnings: %s", strings.Join(res.Warnings, "; "))
	}
	if msg == "" {
		for _, s := range d.archive {
			if s.ArchivedAt != "" {
				continue
			}
			if aerr := a.archive(s); aerr != nil {
				notice = strings.TrimSpace(notice + " " + L("Could not archive %s: %s", s.Title, aerr.Error()))
			}
		}
	}
	go a.reloadList()
	a.cfg.Update(func() {
		dd := &a.worktreeAsk
		if dd.path != d.path {
			a.settings.worktrees.readAt = time.Time{}
			return
		}
		dd.busy = false
		a.settings.worktrees.readAt = time.Time{}
		a.settings.worktrees.notice = notice
		if msg != "" {
			dd.err = msg
			return
		}
		dd.open = false
	})
}
