package app

import (
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/protocol"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/session"
)

// gitChanges is the Session's branch and what it has not committed, as the
// Daemon's get_git_diff reports the working tree against HEAD.
type gitChanges struct {
	branch               string
	files                []protocol.DaemonGetGitDiffFile
	additions, deletions int
}

// gitState reads gitChanges for a sessionView: again 5 seconds after the
// last read when asked, every 15 seconds for edits made outside Droi, after
// each tool result while a turn runs and once when it rests.
type gitState struct {
	open bool

	mu      sync.Mutex
	changes *gitChanges // nil: not a Git repository, or not read yet
	readAt  time.Time
	reading bool
	stale   bool
	wasBusy bool
}

const (
	gitFresh   = 5 * time.Second
	gitRefresh = 15 * time.Second
)

// gitStale asks for a read at the next frame, as a tool result does.
func (v *sessionView) gitStale() {
	v.git.mu.Lock()
	v.git.stale = true
	v.git.mu.Unlock()
}

// loadGit reads the changes when they are due; c.After brings the frame
// of the next periodic read.
func (v *sessionView) loadGit(c *ui.Context, s *session.Session, loaded bool) *gitChanges {
	g := &v.git
	g.mu.Lock()
	defer g.mu.Unlock()
	busy := s != nil && s.WorkingState() != protocol.DroidWorkingStateIdle
	if g.wasBusy && !busy {
		g.stale = true
	}
	g.wasBusy = busy
	if !loaded || v.a.ctl == nil {
		return g.changes
	}
	since := time.Since(g.readAt)
	due := g.readAt.IsZero() || since >= gitRefresh || g.stale && since >= gitFresh
	if !due || g.reading {
		c.After(max(gitRefresh-since, time.Second))
		return g.changes
	}
	g.reading, g.stale = true, false
	go func() {
		defer v.a.redraw()
		next, ok := v.readGit()
		g.mu.Lock()
		defer g.mu.Unlock()
		g.reading, g.readAt = false, time.Now()
		if ok {
			g.changes = next
		}
	}()
	return g.changes
}

// readGit asks the Daemon; ok is false when the read itself failed, which
// keeps what was shown.
func (v *sessionView) readGit() (*gitChanges, bool) {
	cl, err := v.a.ctl.Client()
	if err != nil {
		return nil, false
	}
	stats := true
	res, err := cl.GetGitDiff(v.a.ctx, protocol.GetGitDiffParams{SessionID: v.id, StatsOnly: &stats})
	if err != nil {
		return nil, false
	}
	val, err := res.Value()
	if err != nil {
		return nil, false
	}
	ok, isOK := val.(*protocol.DaemonGetGitDiffSuccessResult)
	if !isOK {
		return nil, true
	}
	d := ok.Data
	g := &gitChanges{branch: d.Branch, files: d.UnstagedFiles}
	if d.IsDetachedHead != nil && *d.IsDetachedHead {
		g.branch = "detached HEAD"
	}
	if d.UnstagedTotalAdditions != nil {
		g.additions = int(*d.UnstagedTotalAdditions)
	}
	if d.UnstagedTotalDeletions != nil {
		g.deletions = int(*d.UnstagedTotalDeletions)
	}
	return g, true
}

var gitStatusLetters = map[string]string{
	"added": "A", "modified": "M", "deleted": "D", "renamed": "R", "copied": "C", "untracked": "A",
}

// gitButton is the GitChangesButton: the branch and the line counts at
// the header's right, opening the changed files. Absent outside a Git
// repository.
func (v *sessionView) gitButton(c *ui.Context, s *session.Session, loaded bool) {
	a := v.a
	k, t := a.kit, a.kit.T
	g := v.loadGit(c, s, loaded)
	if g == nil {
		return
	}
	dirty := len(g.files) > 0
	label := "Branch " + g.branch + ", no changes"
	if dirty {
		label = "Branch " + g.branch + ", " + plural(len(g.files), "changed file", "changed files")
	}
	b := ui.ButtonBase(c).Label(label).Height(k.Px(28)).Gap(k.Px(6)).PaddingX(k.Px(6)).Radius(k.Px(6)).Cursor(ui.CursorPointer).Expanded(v.git.open)
	color := t.MutedForeground
	if b.Hovered() || v.git.open {
		b.Background(t.Accent)
		color = t.Foreground
	}
	b.Children(func() {
		k.Icon(c, "git-branch", 14, color)
		k.Text(c, g.branch, 12, 16).TextColor(color).SingleLine().MaxWidth(k.Px(160))
		if dirty {
			v.lineCounts(c, g.additions, g.deletions)
		}
	})
	if b.Clicked() {
		v.git.open = !v.git.open
	}
	ui.PopoverBase(c, b, &v.git.open, func(p *ui.Element) {
		w, h := c.Size()
		p.AttachTo(b, ui.AnchorBottomRight, ui.AnchorTopRight).Margin(k.Px(6), 0, 0, 0).Role(ui.RoleDialog).Label("Changed files").
			Width(min(k.Px(416), w-k.Px(16))).MaxHeight(min(k.Px(384), h-k.Px(60))).Radius(k.Px(12)).Border(1, t.Border).
			Background(t.Popover).TextColor(t.PopoverForeground).Clip().Shadow(0, k.Px(20), k.Px(25), -k.Px(5), ui.RGBA(0, 0, 0, 0.1))
		ui.Column(c).FillWidth().Children(func() {
			ui.Row(c).Gap(k.Px(8)).Padding(k.Px(10), k.Px(12), k.Px(6), k.Px(12)).Children(func() {
				title := "Working tree clean"
				if dirty {
					title = itoa(len(g.files)) + " uncommitted " + pluralWord(len(g.files), "file")
				}
				k.Text(c, title, 12, 16).Role(ui.RoleHeading).TextColor(t.MutedForeground).SingleLine().Grow(1).MinWidth(0)
				if dirty {
					v.lineCounts(c, g.additions, g.deletions)
				}
			})
			if !dirty {
				return
			}
			ui.Scroll(c).FillWidth().Grow(1).MinHeight(0).Children(func() {
				ui.Column(c).Role(ui.RoleList).Label("Changed files").FillWidth().Padding(0, k.Px(6), k.Px(6), k.Px(6)).Children(func() {
					for _, f := range g.files {
						v.gitFileRow(c, f)
					}
				})
			})
		})
	})
}

func (v *sessionView) gitFileRow(c *ui.Context, f protocol.DaemonGetGitDiffFile) {
	k, t := v.a.kit, v.a.kit.T
	dir, name := "", f.Path
	if i := strings.LastIndex(f.Path, "/"); i >= 0 {
		dir, name = f.Path[:i+1], f.Path[i+1:]
	}
	letter := gitStatusLetters[f.Status]
	if letter == "" && f.Status != "" {
		letter = strings.ToUpper(f.Status[:1])
	}
	ui.Row(c).Role(ui.RoleListItem).Key(f.Path).Label(f.Path).Tooltip(f.Status + ": " + f.Path).Height(k.Px(28)).Gap(k.Px(8)).
		PaddingX(k.Px(6)).Radius(k.Px(6)).Children(func() {
		color := t.MutedForeground
		if f.Status == "deleted" {
			color = t.Removed
		}
		k.Text(c, letter, 12, 16).Font(k.Mono).TextColor(color).Width(k.Px(12)).Shrink(0).Label(f.Status)
		ui.Row(c).Grow(1).MinWidth(0).Gap(k.Px(6)).AlignItems(ui.End).Children(func() {
			k.Text(c, name, 12, 16).Font(k.Mono).SingleLine().Shrink(1).MinWidth(0)
			if dir != "" {
				k.Text(c, dir, 12, 16).Font(k.Mono).TextColor(t.MutedForeground).SingleLine().Grow(1).Basis(0).MinWidth(0)
			}
		})
		v.lineCounts(c, int(f.Additions), int(f.Deletions))
	})
}

// lineCounts is +12 −3 in the diff colours.
func (v *sessionView) lineCounts(c *ui.Context, additions, deletions int) {
	k, t := v.a.kit, v.a.kit.T
	ui.Row(c).Gap(k.Px(4)).Shrink(0).Children(func() {
		k.Text(c, "+"+itoa(additions), 12, 16).Font(k.Mono).FontFeatures("tnum").TextColor(t.Added)
		k.Text(c, "−"+itoa(deletions), 12, 16).Font(k.Mono).FontFeatures("tnum").TextColor(t.Removed)
	})
}

func pluralWord(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}
