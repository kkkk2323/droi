package app

import (
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/internal/kit"
	"github.com/kkkk2323/droi/apps/native/internal/prefs"
	"github.com/kkkk2323/droi/apps/native/internal/sessions"
)

// The lifecycles a new worktree can have, as the Factory App words them.
var worktreeLifecycles = []struct{ value, label, description string }{
	{string(protocol.WorktreeLifecycleEphemeral), "Ephemeral", "Single-session worktrees that are cleaned up automatically."},
	{string(protocol.WorktreeLifecyclePersistent), "Persistent", "Multi-session worktrees that can only be deleted manually."},
}

func lifecycleLabel(value string) string {
	for _, l := range worktreeLifecycles {
		if l.value == value {
			return l.label
		}
	}
	return "Ephemeral"
}

// repoInfo is what the New session page knows of a Workspace's repository.
type repoInfo struct {
	loaded   bool
	git      bool
	current  string
	branches []string
	profiles []protocol.WorktreeSetupProfileSummary
	// lastProfile is the setup profile the Daemon says was used last.
	lastProfile string
}

// worktreeChoice is how a new Session is to be started: in a worktree of
// its repository or in the folder itself.
type worktreeChoice struct {
	On                         bool
	Lifecycle, Base, ProfileID string
}

// noProfile is the setup profile choice "none".
const noProfile = "none"

// repoOf is the repository at path, read from the Daemon once per visit
// of the page; nil until it answers.
func (a *App) repoOf(path string) *repoInfo {
	s := &a.newPage
	if path == "" || path == scratchPick || a.ctl == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.repos == nil {
		s.repos = map[string]*repoInfo{}
	}
	if info, ok := s.repos[path]; ok {
		if !info.loaded {
			return nil
		}
		return info
	}
	if !a.ctl.Status().Connected {
		return nil
	}
	info := &repoInfo{}
	s.repos[path] = info
	go func() {
		cl, err := a.ctl.Client()
		if err != nil {
			s.mu.Lock()
			delete(s.repos, path)
			s.mu.Unlock()
			return
		}
		next := repoInfo{loaded: true}
		if res, err := cl.ListGitBranches(a.ctx, protocol.ListGitBranchesParams{Cwd: path}); err == nil && res.IsGitRepository {
			next.git, next.current, next.branches = true, res.CurrentBranch, res.Branches
			// An older Daemon has no setup profiles; the page then offers none.
			if p, err := cl.ListWorktreeSetupProfiles(a.ctx, protocol.ListWorktreeSetupProfilesParams{Cwd: path}); err == nil {
				next.profiles, next.lastProfile = p.Profiles, p.LastUsedProfileID
			}
		}
		s.mu.Lock()
		*info = next
		s.mu.Unlock()
		a.redraw()
	}()
	return nil
}

// worktreeChoiceFor is what the page would start in the Workspace now.
func (a *App) worktreeChoiceFor(repo *repoInfo) worktreeChoice {
	s := &a.newPage
	if repo == nil || !repo.git {
		return worktreeChoice{}
	}
	ch := worktreeChoice{On: prefs.NewSessionWorktree.Get(a.prefs), Lifecycle: s.lifecycle, Base: s.base, ProfileID: s.profile}
	if s.worktreeSet {
		ch.On = s.worktree
	}
	if ch.Lifecycle == "" {
		ch.Lifecycle = prefs.WorktreeLifecycle.Get(a.prefs)
	}
	if ch.Base == "" {
		ch.Base = repo.current
	}
	if ch.ProfileID == "" {
		ch.ProfileID = repo.lastProfile
	}
	if ch.ProfileID == noProfile || !hasProfile(repo.profiles, ch.ProfileID) {
		ch.ProfileID = ""
	}
	return ch
}

func hasProfile(list []protocol.WorktreeSetupProfileSummary, id string) bool {
	for _, p := range list {
		if p.ID == id {
			return true
		}
	}
	return false
}

// apply puts the choice into the request for the Session; prompt is its
// first message, of which the Daemon names the branch.
func (ch worktreeChoice) apply(p *protocol.InitializeSessionParams, prompt string) {
	if !ch.On {
		return
	}
	yes := true
	p.Worktree = &yes
	p.WorktreeLifecycle = protocol.WorktreeLifecycle(ch.Lifecycle)
	if slug := strings.TrimSpace(prompt); slug != "" {
		if r := []rune(slug); len(r) > 200 {
			slug = string(r[:200])
		}
		p.WorktreePromptSlug = slug
	}
	if ch.Base != "" {
		p.WorktreeBaseBranch = ch.Base
		p.WorktreeBranchMode = protocol.WorktreeBranchModeCopy
	}
	p.WorktreeSetupProfileID = ch.ProfileID
}

// worktreeControl is the New session page's choice between working in the
// folder and in a new worktree, with the worktree's lifecycle, base branch
// and setup profile: the Factory App's worktree selector.
func (a *App) worktreeControl(c *ui.Context, repo *repoInfo, ch worktreeChoice) {
	k, t := a.kit, a.kit.T
	s := &a.newPage
	label, icon := "Work locally", "laptop"
	if ch.On {
		label, icon = "New worktree", "git-fork"
	}
	tr := k.QuietTrigger(c, "Worktree", s.worktreeOpen).Role(ui.RoleComboBox).Gap(k.Px(4)).Margin(0, 0, 0, -k.Px(8))
	if ch.On {
		tr.Tooltip(lifecycleLabel(ch.Lifecycle) + " worktree from " + ch.Base)
	}
	fg := t.MutedForeground
	if tr.Hovered() || s.worktreeOpen {
		fg = t.Foreground
	}
	tr.Children(func() {
		k.Icon(c, icon, 14, fg)
		k.Text(c, label, 12, 16).TextColor(fg).SingleLine()
		k.Icon(c, "chevron-down", 12, fg).Opacity(0.6)
	})
	if tr.Clicked() {
		s.worktreeOpen = !s.worktreeOpen
	}
	pick := func(fn func()) {
		s.worktreeSet, s.worktree = true, true
		fn()
	}
	ui.PopoverBase(c, tr, &s.worktreeOpen, func(p *ui.Element) {
		p.Label("Worktree").Width(k.Px(304)).Margin(k.Px(6), 0, 0, 0).Radius(k.Px(12)).Border(1, t.Border).
			Background(t.Popover).TextColor(t.PopoverForeground).Clip().Shadow(0, k.Px(10), k.Px(15), -k.Px(3), ui.RGBA(0, 0, 0, 0.1))
		ui.Scroll(c).MaxHeight(k.Px(420)).Children(func() {
			ui.Column(c).Padding(k.Px(4)).Children(func() {
				ui.Column(c).Role(ui.RoleGroup).Label("Where to work").Children(func() {
					if a.choiceRow(c, "local", "laptop", "Work locally", "", !ch.On) {
						s.worktreeSet, s.worktree = true, false
					}
					if a.choiceRow(c, "worktree", "git-fork", "New worktree", "", ch.On) {
						s.worktreeSet, s.worktree = true, true
					}
				})
				caption := func(text string) {
					ui.Box(c).Height(1).Background(t.Border).Margin(k.Px(4), -k.Px(4))
					k.Text(c, text, 11, 16.5).FontWeight(500).TextColor(t.MutedForeground).Padding(k.Px(4), k.Px(8))
				}
				caption("Lifecycle")
				ui.Column(c).Role(ui.RoleGroup).Label("Lifecycle").Children(func() {
					for _, l := range worktreeLifecycles {
						if a.choiceRow(c, "lifecycle:"+l.value, "", l.label, l.description, ch.On && ch.Lifecycle == l.value) {
							pick(func() {
								s.lifecycle = l.value
								prefs.WorktreeLifecycle.Set(a.prefs, l.value)
							})
						}
					}
				})
				if len(repo.branches) > 0 {
					caption("Base branch")
					ui.Scroll(c).MaxHeight(k.Px(150)).Children(func() {
						ui.Column(c).Role(ui.RoleGroup).Label("Base branch").Children(func() {
							for _, b := range repo.branches {
								note := ""
								if b == repo.current {
									note = "Current branch"
								}
								if a.choiceRow(c, "base:"+b, "git-branch", b, note, ch.On && ch.Base == b) {
									pick(func() { s.base = b })
								}
							}
						})
					})
				}
				if len(repo.profiles) > 0 {
					caption("Setup profile")
					ui.Column(c).Role(ui.RoleGroup).Label("Setup profile").Children(func() {
						if a.choiceRow(c, "profile:none", "", "Start without setup profile", "", ch.On && ch.ProfileID == "") {
							pick(func() { s.profile = noProfile })
						}
						for _, pr := range repo.profiles {
							note := ""
							if pr.Source == protocol.WorktreeSetupProfileSourceRepository {
								note = "Shared by repository"
							}
							if a.choiceRow(c, "profile:"+pr.ID, "", pr.Name, note, ch.On && ch.ProfileID == pr.ID) {
								pick(func() { s.profile = pr.ID })
							}
						}
					})
				}
				ui.Box(c).Height(1).Background(t.Border).Margin(k.Px(4), -k.Px(4))
				def := prefs.NewSessionWorktree.Get(a.prefs)
				ui.Row(c).Gap(k.Px(8)).Padding(k.Px(6), k.Px(8)).Tooltip("Whether new sessions start with worktrees enabled.").Children(func() {
					k.Text(c, "Start new sessions in a worktree", 13, 19.5).Grow(1).MinWidth(0)
					if a.toggle(c, "Start new sessions in a worktree", def, false) {
						prefs.NewSessionWorktree.Set(a.prefs, !def)
					}
				})
				k.Text(c, "The branch is created in the new worktree when the session starts. Your current checkout stays on its branch.", 11.5, 17).
					TextColor(t.MutedForeground).Padding(k.Px(2), k.Px(8), k.Px(6), k.Px(8))
			})
		})
	})
}

// choiceRow is a radio row of a popover: an icon, a label with a note under
// it, and a check when chosen. It reports a click.
func (a *App) choiceRow(c *ui.Context, key, icon, label, note string, checked bool) bool {
	k, t := a.kit, a.kit.T
	b := ui.ButtonBase(c).Key(key).Role(ui.RoleMenuItemRadio).Checked(checked).Label(label).Gap(k.Px(8)).Padding(k.Px(6), k.Px(8)).
		Radius(k.Px(6)).Justify(ui.Start).AlignItems(ui.Start).Cursor(ui.CursorPointer)
	if note != "" {
		b.Tooltip(note)
	}
	color := t.PopoverForeground
	if b.Hovered() || b.Focused() {
		b.Background(t.Accent)
		color = t.AccentForeground
	}
	b.Children(func() {
		if icon != "" {
			k.Icon(c, icon, 14, t.MutedForeground).Margin(k.Px(3), 0, 0, 0)
		}
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			k.Text(c, label, 13, 20).TextColor(color).SingleLine()
			if note != "" {
				k.Text(c, note, 11.5, 16).TextColor(t.MutedForeground)
			}
		})
		ui.Box(c).Size(k.Px(16), k.Px(20)).Shrink(0).Center().Children(func() {
			if checked {
				k.Icon(c, "check", 14, color)
			}
		})
	})
	return b.Clicked()
}

// worktreeDialog asks before a worktree goes: archiving a Session in an
// ephemeral one, or deleting one from Settings. It shows what the
// worktree holds that would be lost, as the Factory App's does.
type worktreeDialog struct {
	open bool
	// session is the Session to archive; nil when deleting the worktree.
	session      *sessions.Summary
	path, branch string
	// sessions is how many Sessions deleting the worktree archives.
	sessions int

	checked                   bool
	inspect                   *protocol.InspectWorktreeDeletionResult
	inspectErr                string
	deleteLocal, deleteRemote bool
	busy                      bool
	err                       string
}

// askArchive opens the dialog for archiving a Session in an ephemeral worktree.
func (a *App) askArchive(s sessions.Summary) {
	a.worktreeAsk = worktreeDialog{open: true, session: &s, path: s.Worktree.Path, branch: s.Worktree.Branch}
	a.inspectWorktree(s.Worktree.Path)
}

// askDeleteWorktree opens the dialog for deleting a managed worktree.
func (a *App) askDeleteWorktree(w protocol.DaemonManagedWorktree) {
	a.worktreeAsk = worktreeDialog{open: true, path: w.Path, branch: w.Branch, sessions: len(w.Sessions)}
	a.inspectWorktree(w.Path)
}

func (a *App) inspectWorktree(path string) {
	go func() {
		var res *protocol.InspectWorktreeDeletionResult
		cl, err := a.ctl.Client()
		if err == nil {
			res, err = cl.InspectWorktreeDeletion(a.ctx, protocol.InspectWorktreeDeletionParams{WorktreePath: path})
		}
		a.cfg.Update(func() {
			d := &a.worktreeAsk
			if d.path != path {
				return
			}
			d.checked = true
			if err != nil {
				d.inspectErr = err.Error()
				return
			}
			d.inspect = res
		})
	}()
}

// unsaved is whether the worktree holds work only it has.
func (d *worktreeDialog) unsaved() bool {
	in := d.inspect
	return in != nil && (in.ChangedFiles+in.UntrackedFiles > 0 || (in.LocalOnlyCommits != nil && *in.LocalOnlyCommits > 0))
}

func (a *App) worktreeDialogView(c *ui.Context) {
	d := &a.worktreeAsk
	if !d.open {
		return
	}
	k, t := a.kit, a.kit.T
	w, _ := c.Size()
	archive := d.session != nil
	title, action := "Delete worktree?", "Delete worktree"
	message := "Deleting this worktree will also archive all associated sessions."
	if d.sessions > 0 {
		message = "Deleting this worktree will also archive " + plural(d.sessions, "associated session", "associated sessions") + "."
	}
	if archive {
		title, action = "Archive session?", "Archive"
		message = "Archiving this session will delete its worktree."
	}
	ui.DialogBase(c, &d.open, func(backdrop, panel *ui.Element) {
		backdrop.Background(ui.RGBA(0, 0, 0, 0.3))
		panel.Role(ui.RoleAlertDialog).Label(title).Width(min(k.Px(440), w-k.Px(32))).Radius(k.Px(12)).Border(1, t.Border).
			Background(t.Popover).TextColor(t.PopoverForeground).Clip().Shadow(0, k.Px(20), k.Px(25), -k.Px(5), ui.RGBA(0, 0, 0, 0.1))
		ui.Column(c).FillWidth().Gap(k.Px(12)).Padding(k.Px(16)).Children(func() {
			k.Text(c, title, 15, 22).Role(ui.RoleHeading).FontWeight(600)
			k.Text(c, message, 13, 20).TextColor(t.MutedForeground)
			ui.Column(c).Gap(k.Px(2)).Padding(k.Px(8), k.Px(10)).Radius(k.Px(8)).Background(t.Card).Children(func() {
				fact := func(name, value string) {
					ui.Row(c).Gap(k.Px(6)).AlignItems(ui.Start).Children(func() {
						k.Text(c, name, 12, 18).TextColor(t.MutedForeground).Width(k.Px(52)).Shrink(0)
						k.Text(c, value, 12, 18).Font(k.Mono).Selectable().Grow(1).MinWidth(0)
					})
				}
				if d.branch != "" {
					fact("Branch:", d.branch)
				}
				fact("Path:", d.path)
			})
			warn := func(s string) { k.Text(c, s, 13, 20).Role(ui.RoleStatus).TextColor(t.AttentionForeground) }
			in := d.inspect
			switch {
			case !d.checked:
				k.Text(c, "Checking current git status...", 13, 20).Role(ui.RoleStatus).TextColor(t.MutedForeground)
			case d.inspectErr != "":
				warn("Could not check this worktree for unsaved work. Anything it holds will be deleted with it.")
			case in != nil:
				var parts []string
				if in.ChangedFiles > 0 {
					parts = append(parts, plural(int(in.ChangedFiles), "changed file", "changed files"))
				}
				if in.UntrackedFiles > 0 {
					parts = append(parts, plural(int(in.UntrackedFiles), "untracked file", "untracked files"))
				}
				if in.LocalOnlyCommits != nil && *in.LocalOnlyCommits > 0 {
					parts = append(parts, plural(int(*in.LocalOnlyCommits), "commit on no remote", "commits on no remote"))
				}
				if len(parts) > 0 {
					warn("Warning: this worktree has " + strings.Join(parts, ", ") + ".")
					if archive && in.ChangedFiles+in.UntrackedFiles > 0 {
						k.Text(c, "Droid keeps a worktree with uncommitted changes; delete it in Settings → Worktrees.", 12, 18).TextColor(t.MutedForeground)
					}
				}
				if pr := in.PullRequest; pr != nil {
					label := "PR " + string(pr.State)
					if pr.Title != "" {
						label += ": " + pr.Title
					}
					k.Text(c, label, 13, 20).TextColor(t.MutedForeground)
				}
				if in.RemoteRefsStale {
					k.Text(c, "Origin could not be reached, so branch details may be out of date.", 12, 18).TextColor(t.MutedForeground)
				}
			}
			if !archive && d.branch != "" {
				ui.Column(c).Gap(k.Px(4)).Children(func() {
					if a.checkRow(c, "Delete local branch", d.deleteLocal) {
						d.deleteLocal = !d.deleteLocal
					}
					if in != nil && in.HasRemoteBranch {
						if a.checkRow(c, "Delete origin branch", d.deleteRemote) {
							d.deleteRemote = !d.deleteRemote
						}
						if d.deleteRemote && in.PullRequest != nil && in.PullRequest.State == protocol.DaemonWorktreeBranchPullRequestStateOpen {
							k.Text(c, "Deleting the origin branch closes the open pull request.", 12, 18).TextColor(t.AttentionForeground)
						}
					}
				})
			}
			if d.err != "" {
				k.Text(c, d.err, 13, 20).Role(ui.RoleStatus).TextColor(t.DestructiveForeground)
			}
			ui.Row(c).Gap(k.Px(8)).Justify(ui.End).Children(func() {
				if a.smallButton(c, kit.Outline, "Cancel", "", d.busy).Clicked() || c.Shortcut(0, ui.KeyEscape) {
					d.open = false
				}
				go_ := a.dangerButton(c, action, d.busy || !d.checked)
				if go_.Clicked() {
					d.busy, d.err = true, ""
					if archive {
						s := *d.session
						go a.archiveConfirmed(s)
					} else {
						go a.deleteWorktree(d.path, d.deleteLocal, d.deleteRemote, d.unsaved())
					}
				}
			})
		})
	})
}

// dangerButton is the action that deletes: solid red with white text, so
// it stands out from Cancel beside it.
func (a *App) dangerButton(c *ui.Context, label string, disabled bool) *ui.Element {
	k, t := a.kit, a.kit.T
	b := ui.ButtonBase(c).Label(label).Height(k.Px(32)).PaddingX(k.Px(12)).Radius(k.Px(8)).Disabled(disabled).Cursor(ui.CursorPointer)
	bg := t.Destructive
	switch {
	case disabled:
		bg = bg.Alpha(0.5)
	case b.Hovered():
		bg = bg.Alpha(0.88)
	}
	b.Background(bg).Children(func() {
		k.Text(c, label, 13, 20).FontWeight(500).TextColor(ui.RGBA(255, 255, 255, 1))
	})
	return b
}

// checkRow is a checkbox with its label.
func (a *App) checkRow(c *ui.Context, label string, on bool) bool {
	k, t := a.kit, a.kit.T
	b := ui.ButtonBase(c).Role(ui.RoleCheckBox).Checked(on).Label(label).Gap(k.Px(8)).Justify(ui.Start).Cursor(ui.CursorPointer)
	b.Children(func() {
		box := ui.Box(c).Size(k.Px(16), k.Px(16)).Radius(k.Px(4)).Border(1, t.Border).Center().Shrink(0)
		if on {
			box.Background(t.Primary).Border(1, t.Primary)
		}
		box.Children(func() {
			if on {
				k.Icon(c, "check", 12, t.PrimaryForeground)
			}
		})
		k.Text(c, label, 13, 20)
	})
	return b.Clicked()
}

func (a *App) archiveConfirmed(s sessions.Summary) {
	err := a.archive(s)
	a.cfg.Update(func() {
		d := &a.worktreeAsk
		d.busy = false
		if err != nil {
			d.err = err.Error()
			return
		}
		d.open = false
	})
}

// deleteWorktree has the Daemon delete a worktree it manages, forcing it
// when the user saw what it holds and confirmed.
func (a *App) deleteWorktree(path string, local, remote, force bool) {
	var res *protocol.CleanupWorktreeResult
	cl, err := a.ctl.Client()
	if err == nil {
		res, err = cl.CleanupWorktree(a.ctx, protocol.CleanupWorktreeParams{WorktreePath: path, DeleteLocalBranch: &local, DeleteRemoteBranch: &remote, Force: &force})
	}
	msg := ""
	switch {
	case err != nil:
		msg = "Failed to delete worktree: " + err.Error()
	case res.PreservedReason == protocol.DaemonWorktreePreservedReasonUncommittedChanges:
		msg = "Worktree was kept: it holds uncommitted changes. Commit or stash them, or remove it with git worktree remove --force."
	case !res.WorktreeRemoved:
		msg = "Worktree could not be removed. Remove it manually with git worktree remove."
	}
	notice := ""
	if err == nil && res.WorktreeRemoved && len(res.Warnings) > 0 {
		notice = "Worktree deleted with warnings: " + strings.Join(res.Warnings, "; ")
	}
	go a.refreshList()
	a.cfg.Update(func() {
		d := &a.worktreeAsk
		d.busy = false
		a.settings.worktrees.readAt = time.Time{}
		a.settings.worktrees.notice = notice
		if msg != "" {
			d.err = msg
			return
		}
		d.open = false
		if res != nil && a.route.Name == "session" {
			for _, id := range res.ArchivedSessionIDs {
				if id == a.route.SessionID {
					a.Go(Route{Name: "home"})
				}
			}
		}
	})
}

// managedWorktrees is Settings → Worktrees' list, read when the tab shows.
type managedWorktrees struct {
	readAt  time.Time
	loading bool
	list    []protocol.DaemonManagedWorktree
	err     string
	notice  string

	directoryOpen bool
}

func (a *App) loadManagedWorktrees() {
	m := &a.settings.worktrees
	if m.loading || (!m.readAt.IsZero() && time.Since(m.readAt) < 30*time.Second) || a.ctl == nil {
		return
	}
	m.loading, m.readAt = true, time.Now()
	go func() {
		var res *protocol.ListManagedWorktreesResult
		cl, err := a.ctl.Client()
		if err == nil {
			res, err = cl.ListManagedWorktrees(a.ctx, protocol.ListManagedWorktreesParams{})
		}
		a.cfg.Update(func() {
			m.loading = false
			if err != nil {
				m.err = "Failed to load managed worktrees."
				return
			}
			m.err, m.list = "", res.Worktrees
		})
	}()
}

// worktreesTab is Settings → Worktrees: where the Daemon puts worktrees,
// how many ephemeral ones it keeps, and the ones it manages.
func (a *App) worktreesTab(c *ui.Context) {
	k, t := a.kit, a.kit.T
	k.Text(c, "Worktrees let a session work on its own checkout and branch, beside your own. Droid makes and cleans them up; these settings are shared with the droid CLI and the Factory App on this computer.", 13, 19.5).
		TextColor(t.MutedForeground).Margin(-k.Px(16), 0, 0, 0)
	dv := a.sessionDefaults()
	if dv == nil {
		k.Text(c, "Loading worktree settings…", 14, 20).TextColor(t.MutedForeground)
		return
	}
	save := func(p defaultsPatch) { a.saveDefaults(*dv, p) }
	base := "~/.factory"
	if dv.UserFactoryDir != "" {
		base = dv.UserFactoryDir
	}
	defaultDir := strings.TrimSuffix(base, "/") + "/worktrees"
	a.settingGroup(c, "Worktree settings", "",
		func() {
			const custom = "custom…"
			opts := []kit.Option{{Value: "", Label: "Default (" + defaultDir + ")"}}
			if dv.WorktreeDirectory != "" {
				opts = append(opts, kit.Option{Value: dv.WorktreeDirectory, Label: "Custom: " + dv.WorktreeDirectory})
			}
			opts = append(opts, kit.Option{Value: custom, Label: "Custom…"})
			a.settingRow(c, "Worktree directory", "Choose a custom parent directory for local worktrees.", func() {
				next, ok := a.fieldSelect(c, "Worktree directory", dv.WorktreeDirectory, opts, false)
				switch {
				case !ok:
				case next == custom:
					go func() {
						if dir := a.pickFolder("Choose a worktree directory"); dir != "" {
							a.cfg.Update(func() { save(defaultsPatch{WorktreeDirectory: &dir}) })
						}
					}()
				default:
					save(defaultsPatch{WorktreeDirectory: &next})
				}
			}, nil)
		},
		func() {
			limit := dv.WorktreeAutoDeleteLimit
			if limit == 0 {
				limit = defaultWorktreeLimit
			}
			var opts []kit.Option
			for _, n := range []int{5, 10, 15, 20, 30, 50, 100} {
				if n == defaultWorktreeLimit {
					opts = append(opts, kit.Option{Value: strconv.Itoa(n), Label: strconv.Itoa(n) + " worktrees (default)"})
					continue
				}
				opts = append(opts, kit.Option{Value: strconv.Itoa(n), Label: strconv.Itoa(n) + " worktrees"})
			}
			a.settingRow(c, "Ephemeral worktrees limit", "Droid deletes old ephemeral worktrees to stay within this limit. It keeps one that is in use, holds uncommitted changes or unpublished commits, or has an open pull request, and never deletes the local branch.", func() {
				if next, ok := a.fieldSelect(c, "Ephemeral worktrees limit", strconv.Itoa(limit), opts, false); ok {
					n, _ := strconv.Atoi(next)
					save(defaultsPatch{WorktreeAutoDeleteLimit: &n})
				}
			}, nil)
		},
	)
	a.loadManagedWorktrees()
	m := &a.settings.worktrees
	var ephemeral, persistent int
	for _, w := range m.list {
		if w.Lifecycle == protocol.WorktreeLifecyclePersistent {
			persistent++
		} else {
			ephemeral++
		}
	}
	footer := "Factory-managed worktrees on this computer."
	if len(m.list) > 0 {
		footer += " Ephemeral · " + strconv.Itoa(ephemeral) + ", Persistent · " + strconv.Itoa(persistent) + "."
	}
	rows := []func(){}
	switch {
	case m.err != "":
		rows = append(rows, func() { a.settingRow(c, m.err, "", nil, nil) })
	case m.list == nil && m.loading:
		rows = append(rows, func() { a.settingRow(c, "Loading worktrees…", "", nil, nil) })
	case len(m.list) == 0:
		rows = append(rows, func() { a.settingRow(c, "No Factory-managed worktrees.", "", nil, nil) })
	}
	for _, w := range m.list {
		rows = append(rows, func() {
			title := w.Branch
			if title == "" {
				title = sessions.WorkspaceLabel(w.Path)
			}
			desc := sessions.WorkspaceLabel(w.RepoRoot) + " · " + lifecycleLabel(string(w.Lifecycle)) + " · " + plural(len(w.Sessions), "session", "sessions")
			if w.IsClean != nil && !*w.IsClean {
				desc += " · uncommitted changes"
			}
			a.settingRowWith(c, func() {
				ui.Row(c).Gap(k.Px(6)).Children(func() {
					k.Icon(c, "git-branch", 14, t.MutedForeground)
					k.Text(c, title, 14, 20).Role(ui.RoleHeading).FontWeight(500).SingleLine().Shrink(1)
				})
			}, desc, func() {
				if a.smallButton(c, kit.Ghost, "Delete", "trash", false).Label("Delete worktree " + title).Clicked() {
					a.askDeleteWorktree(w)
				}
			}, func() {
				k.Text(c, w.Path, 12, 18).Font(k.Mono).TextColor(t.MutedForeground).Selectable()
			})
		})
	}
	if m.notice != "" {
		k.Text(c, m.notice, 13, 20).Role(ui.RoleStatus).TextColor(t.AttentionForeground)
	}
	a.settingGroup(c, "Managed worktrees", footer, rows...)
}

// defaultWorktreeLimit is the Daemon's worktreeAutoDeleteLimit when unset.
const defaultWorktreeLimit = 15
