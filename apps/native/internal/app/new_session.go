package app

import (
	"errors"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/google/uuid"
	droid "github.com/kkkk2323/droid-sdk-go"
	"github.com/kkkk2323/droid-sdk-go/controller"
	"github.com/kkkk2323/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/internal/loops"
	"github.com/kkkk2323/droi/apps/native/internal/slash"
	"github.com/kkkk2323/droi/apps/native/pkg/attachments"
	"github.com/kkkk2323/droi/apps/native/pkg/defaults"
	"github.com/kkkk2323/droi/apps/native/pkg/drafts"
	"github.com/kkkk2323/droi/apps/native/pkg/gateway"
	"github.com/kkkk2323/droi/apps/native/pkg/kit"
	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
	"github.com/kkkk2323/droi/apps/native/pkg/models"
	"github.com/kkkk2323/droi/apps/native/pkg/pasted"
	"github.com/kkkk2323/droi/apps/native/pkg/sessions"
	"github.com/kkkk2323/droi/apps/native/pkg/sessionview"
)

// newSessionState is the New session page's: the Workspace picked, the
// settings changed on the page, and the composer.
type newSessionState struct {
	// pick is "" until the user picks (the most recent Workspace is the
	// target), "scratch" for None, or a path.
	pick     string
	picked   bool
	menuOpen bool
	// query, highlight and allRecent are the Workspace picker's while it
	// is open; highlight is -1 until the rows are known.
	query     string
	highlight int
	allRecent bool

	modelID, effort, autonomy string
	// interaction is "auto" or "spec" once toggled on the page.
	interaction string
	toolMode    defaults.ToolMode

	text    string
	images  []attachments.Image
	pasted  []pasted.Text
	bitmaps map[string]*ui.Bitmap
	err     string
	menu    slashMenu
	tagField
	// caretToEnd puts the caret after a picked command, on the next build.
	caretToEnd bool

	modelOpen bool
	picker    pickerState
	autoOpen  bool

	// worktree is the page's choice once worktreeSet; until then the
	// preference decides. lifecycle, base and profile are "" for the
	// preference, the current branch and the last setup profile.
	worktreeSet, worktree    bool
	lifecycle, base, profile string
	worktreeOpen             bool

	mu       sync.Mutex
	creating bool
	draft    *draftSession
	// repos are the Workspaces' repositories, read again on each visit.
	repos map[string]*repoInfo
}

const scratchPick = "scratch"

// reset starts the page afresh: from a sidebar group, with its Workspace.
func (s *newSessionState) reset(r Route) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pick, s.picked = "", false
	s.modelID, s.effort, s.autonomy, s.toolMode = "", "", "", ""
	s.err = ""
	s.worktreeSet, s.worktree, s.lifecycle, s.base, s.profile, s.worktreeOpen = false, false, "", "", "", false
	s.repos = nil
	switch {
	case r.Scratch:
		s.pick, s.picked = scratchPick, true
	case r.Workspace != "":
		s.pick, s.picked = r.Workspace, true
	}
}

// sessionDefaults are the Daemon's defaults for a new Session, read again
// a minute after they were.
func (a *App) sessionDefaults() *defaults.View { return a.defaults.Get() }

// newSessionPage groups the Workspace question and composer in one centred
// start surface. The first message rides along into the Session.
func (a *App) newSessionPage(c *ui.Context, listed []sessions.Summary, status controller.Status) {
	k, t := a.kit, a.kit.T
	s := &a.newPage
	recent := a.recentWorkspaces(listed)
	workspace := s.pick
	if !s.picked && len(recent) > 0 {
		workspace = recent[0].Path
	}
	scratch := workspace == scratchPick
	repo := a.repoOf(workspace)
	worktree := a.worktreeChoiceFor(repo)
	dv := a.sessionDefaults()
	var choices []models.Choice
	modelID, effort, autonomy := s.modelID, s.effort, s.autonomy
	if dv != nil {
		choices = dv.Models
		if modelID == "" {
			modelID = dv.ModelID
		}
		if effort == "" {
			effort = dv.ReasoningEffort
		}
		if autonomy == "" {
			autonomy = dv.AutonomyLevel
		}
	}
	shownMode, requestedMode := defaults.NewSessionToolMode(s.toolMode, defaults.DefaultToolMode(a.prefs), "")
	s.mu.Lock()
	creating := s.creating
	s.mu.Unlock()

	ui.Column(c).Role(ui.RoleGroup).Label(L("New session")).Fill().Children(func() {
		ui.Row(c).Height(k.Px(44)).Shrink(0).PaddingX(k.Px(8)).AlignItems(ui.Center).DragWindow().Children(func() {
			if a.narrow {
				a.openSessionsButton(c)
			}
			if !a.narrow && !a.sidebarShown() {
				leading := float32(36)
				if a.cfg.InsetTop {
					leading = 104
				}
				ui.Box(c).Width(leading).Shrink(0)
			}
			k.Text(c, L("New session"), 12, 18).TextColor(t.MutedForeground)
		})
		_, height := c.Size()
		ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
			// Centre the start surface while it fits; overflow stays scrollable.
			ui.Column(c).FillWidth().MinHeight(max(0, height-k.Px(44))).Center().Gap(k.Px(24)).PaddingY(k.Px(24)).Children(func() {
				ui.Column(c).FillWidth().AlignItems(ui.Center).Gap(k.Px(12)).PaddingX(k.Px(24)).Children(func() {
					droiMark(c, k, 28)
					// The heading is the question's text alone: as the row, it would
					// hide the Workspace button in it from VoiceOver.
					ui.Row(c).FillWidth().Wrap().Justify(ui.Center).GapX(k.Px(6)).Children(func() {
						h := func(s string) ui.Element { return k.Text(c, s, 22, 30).FontWeight(500).LetterSpacing(-k.Px(0.5)) }
						switch {
						case workspace != "" && !scratch:
							h(L("What do you want to build in")).Role(ui.RoleHeading)
							ui.Row(c).Children(func() {
								a.workspaceMenu(c, recent, workspace, labelOf(recent, workspace))
								h("?")
							})
						case scratch:
							h(L("What should we work on?")).Role(ui.RoleHeading)
						default:
							h(L("Where should Droid work?")).Role(ui.RoleHeading)
							if len(listed) > 0 || status.Connected {
								a.workspaceMenu(c, recent, "", L("Choose a workspace"))
							}
						}
					})
					if s.err != "" {
						k.Text(c, s.err, 14, 20).Role(ui.RoleStatus).TextColor(t.DestructiveForeground)
					}
				})
				col := ui.Column(c).FillWidth().MaxWidth(k.Px(sessionview.ColumnWidth)).AlignSelf(ui.Center).PaddingX(k.Px(32)).Shrink(0)
				col.Children(func() {
					a.newSessionComposer(c, workspace, worktree, creating, choices, modelID, effort, autonomy, shownMode, requestedMode, a.draftItems(workspace, requestedMode))
					ui.Row(c).Height(k.Px(28)).Gap(k.Px(12)).PaddingX(k.Px(8)).Children(func() {
						switch {
						case scratch:
							tooltip := L("Not in a project: the session gets a new folder in %s", a.scratchRoot())
							if a.following() {
								tooltip = L("Not in a project: the session gets a new folder on %s", a.computerName())
							}
							tr := k.QuietTrigger(c, L("Work in a project"), s.menuOpen).Margin(0, 0, 0, -k.Px(8)).Tooltip(tooltip)
							tr.Children(func() {
								k.Icon(c, "folder", 14, t.MutedForeground)
								k.Text(c, L("Work in a project"), 12, 16).TextColor(t.MutedForeground).SingleLine()
							})
							a.workspacePicker(c, tr, recent, scratchPick)
						case workspace != "":
							if repo != nil && repo.git {
								a.worktreeControl(c, repo, worktree)
							}
							ui.Row(c).Gap(k.Px(6)).MinWidth(0).Tooltip(workspace).Children(func() {
								k.Icon(c, "folder", 14, t.MutedForeground)
								k.Text(c, workspace, 12, 16).TextColor(t.MutedForeground).SingleLine()
							})
						}
					})
				})
			})
		})
	})
}

// newFolderIn says where the Scratch choice makes its folder.
func (a *App) newFolderIn() string {
	if a.following() {
		return L("A new folder of its own on %s", a.computerName())
	}
	return L("A new folder of its own in %s", a.scratchRoot())
}

// noScratchFolder is the reason a Scratch Workspace cannot be made.
func (a *App) noScratchFolder() string {
	if a.following() {
		return L("%s has no Scratch folder.", a.computerName())
	}
	return L("This computer has no Scratch folder.")
}

func (a *App) scratchRoot() string {
	if a.cfg.Host != nil {
		return a.cfg.Host.ScratchFolder()
	}
	return ""
}

func labelOf(recent []sessions.RecentWorkspace, path string) string {
	for _, w := range recent {
		if w.Path == path {
			return w.Label
		}
	}
	return sessions.WorkspaceLabel(path)
}

// workspaceMenu is the question's Workspace, underlined with dashes,
// opening the workspacePicker.
func (a *App) workspaceMenu(c *ui.Context, recent []sessions.RecentWorkspace, value, label string) {
	k, t := a.kit, a.kit.T
	s := &a.newPage
	tr := ui.ButtonBase(c).Label(L("Workspace")).Expanded(s.menuOpen).Gap(k.Px(2)).BorderWidth(0, 0, 1, 0).Cursor(ui.CursorPointer)
	line := t.MutedForeground.Alpha(0.5)
	if tr.Hovered() || s.menuOpen {
		line = t.Foreground
	}
	tr.BorderColor(line).BorderStyle(ui.BorderDashed)
	tr.Children(func() {
		k.Text(c, label, 22, 30).FontWeight(500).LetterSpacing(-k.Px(0.5))
		k.Icon(c, "chevron-down", 14, t.Foreground).Opacity(0.5)
	})
	a.workspacePicker(c, tr, recent, value)
}

// recentShown is how many recent Workspaces the picker lists until "Show
// N more" or a search.
const recentShown = 5

// workspacePicker is the panel a Workspace trigger opens: a search over
// the recent Workspaces, another folder, and no project at all (a Scratch
// folder). The arrows walk the rows and Enter picks one.
func (a *App) workspacePicker(c *ui.Context, tr ui.Element, recent []sessions.RecentWorkspace, value string) {
	k, t := a.kit, a.kit.T
	s := &a.newPage
	if tr.Clicked() {
		s.menuOpen = !s.menuOpen
		s.query, s.allRecent, s.highlight = "", false, -1
		if slices.IndexFunc(recent, func(w sessions.RecentWorkspace) bool { return w.Path == value }) >= recentShown {
			s.allRecent = true
		}
	}
	if !s.menuOpen {
		return
	}
	q := strings.ToLower(strings.TrimSpace(s.query))
	var shown []sessions.RecentWorkspace
	for _, w := range recent {
		if strings.Contains(strings.ToLower(w.Label), q) || strings.Contains(strings.ToLower(w.Path), q) {
			shown = append(shown, w)
		}
	}
	hidden := 0
	if q == "" && !s.allRecent && len(shown) > recentShown {
		shown, hidden = shown[:recentShown], len(shown)-recentShown
	}
	more, other := -1, len(shown)
	if hidden > 0 {
		more, other = len(shown), len(shown)+1
	}
	none := other + 1
	if a.following() {
		// The folder dialog browses this computer's disk.
		other = -2
		none = len(shown)
		if hidden > 0 {
			none++
		}
	}
	if s.highlight < 0 {
		s.highlight = none
		if value == "" {
			s.highlight = 0
		}
		for i, w := range shown {
			if w.Path == value {
				s.highlight = i
			}
		}
	}
	active := min(s.highlight, none)
	choose := func(v string) {
		s.menuOpen = false
		s.pick, s.picked = v, true
		s.err = ""
	}
	act := func(i int) {
		switch {
		case i < len(shown):
			choose(shown[i].Path)
		case i == more:
			s.allRecent = true
		case i == other:
			s.menuOpen = false
			go func() {
				paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Title: L("Choose a workspace"), Directory: true})
				if err == nil && len(paths) > 0 {
					a.cfg.Update(func() { choose(paths[0]) })
				}
			}()
		default:
			choose(scratchPick)
		}
	}
	row := func(i int, key, icon, text, detail, title string, checked bool) {
		padY := k.Px(6)
		if detail != "" {
			padY = k.Px(4)
		}
		b := ui.ButtonBase(c.Key(key)).Label(text).Gap(k.Px(8)).Padding(padY, k.Px(8)).Radius(k.Px(6)).
			Justify(ui.Start).Cursor(ui.CursorPointer)
		if title != "" {
			b.Tooltip(title)
		}
		if i < len(shown) || i == none {
			b.Role(ui.RoleMenuItemRadio).Checked(checked)
		}
		if b.Hovered() && i != active {
			s.highlight = i
		}
		color, muted := t.PopoverForeground, t.MutedForeground
		if i == active {
			b.Background(t.Accent)
			color = t.AccentForeground
			muted = color.Alpha(0.7)
		}
		b.Children(func() {
			k.Icon(c, icon, 14, t.MutedForeground)
			ui.Column(c).Grow(1).MinWidth(0).Children(func() {
				k.Text(c, text, 14, 20).TextColor(color).SingleLine()
				if detail != "" {
					k.Text(c, detail, 12, 16).TextColor(muted).SingleLine()
				}
			})
			ui.Box(c).Size(k.Px(16), k.Px(16)).Shrink(0).Center().Children(func() {
				if checked {
					k.Icon(c, "check", 14, color)
				}
			})
		})
		if b.Clicked() {
			act(i)
		}
	}
	ui.PopoverBase(c, tr, &s.menuOpen, func(p ui.Element) {
		p.Label(L("Choose a project")).Width(k.Px(288)).Margin(k.Px(6), 0, 0, 0).Radius(k.Px(12)).Border(1, t.Border).
			Background(t.Popover).TextColor(t.PopoverForeground).Clip().Shadow(0, k.Px(10), k.Px(15), -k.Px(3), ui.RGBA(0, 0, 0, 0.1))
		ui.Row(c).Gap(k.Px(8)).PaddingX(k.Px(12)).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
			k.Icon(c, "search", 14, t.MutedForeground)
			in := ui.TextInputBase(c, &s.query).Label(L("Search projects")).Placeholder(L("Search projects")).AutoFocus().
				Height(k.Px(40)).Grow(1).MinWidth(0).FontSize(k.Px(14)).TextColor(t.Foreground)
			if in.Changed() {
				s.highlight = 0
			}
			switch {
			case in.Shortcut(0, ui.KeyDown):
				s.highlight = min(active+1, none)
			case in.Shortcut(0, ui.KeyUp):
				s.highlight = max(active-1, 0)
			case in.Submitted():
				act(active)
			}
		})
		ui.Scroll(c).MaxHeight(k.Px(264)).Children(func() {
			ui.Column(c).Role(ui.RoleList).Label(L("Projects")).Padding(k.Px(4)).Children(func() {
				if len(shown) == 0 {
					msg := L("No projects yet.")
					if q != "" {
						msg = L("No projects match.")
					}
					k.Text(c, msg, 13, 20).TextColor(t.MutedForeground).Padding(k.Px(6), k.Px(8))
				}
				for i, w := range shown {
					row(i, w.Path, "folder", w.Label, a.shownPath(w.Path), w.Path, w.Path == value)
				}
				if hidden > 0 {
					row(more, "more", "chevron-down", L("Show %d more", hidden), "", "", false)
				}
			})
		})
		ui.Column(c).Padding(k.Px(4)).BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Children(func() {
			if !a.following() {
				row(other, "other", "plus", L("Other folder…"), "", L("Choose a folder on this computer"), false)
			}
			row(none, scratchPick, "x", L("Don't work in a project"), "", a.newFolderIn(), value == scratchPick)
		})
	})
}

// newSessionComposer is the InputBar of the start page: Enter starts the
// Session, sending what was typed as its first message.
func (a *App) newSessionComposer(c *ui.Context, workspace string, worktree worktreeChoice, creating bool, choices []models.Choice, modelID, effort, autonomy string, shownMode, requestedMode defaults.ToolMode, fromDaemon []slash.Item) {
	k, t := a.kit, a.kit.T
	s := &a.newPage
	enabled := workspace != "" && !creating
	interaction := s.interaction
	if dv := a.sessionDefaults(); interaction == "" && dv != nil {
		interaction = dv.InteractionMode
	}
	s.takeEdit(&s.text)
	picked, rest, hasPick := s.splitTag(s.text, s.menu.items(s.text, slash.Starters, fromDaemon))
	accept := func(item slash.Item) {
		s.text = "/" + item.Name + " "
		s.caretToEnd = true
	}
	start := func() {
		if !enabled {
			return
		}
		s.takeEdit(&s.text)
		text, images := pasted.Join(s.text, s.pasted), s.images
		// A `/loop` the new Session could not run would leave it empty.
		if m := loopCommand.FindStringSubmatch(strings.TrimSpace(text)); m != nil && len(images) == 0 {
			cmd, err := loops.ParseCommand(m[1])
			switch {
			case err != nil:
				s.err = loopErr(err)
				return
			case cmd.Stop:
				s.err = L("This session has no loops.")
				return
			}
		}
		p := protocol.InitializeSessionParams{
			ModelID:           modelID,
			ReasoningEffort:   protocol.ReasoningEffort(effort),
			InteractionMode:   protocol.DroidInteractionMode(interaction),
			AutonomyLevel:     protocol.AutonomyLevel(autonomy),
			ToolExecutionMode: protocol.ToolExecutionMode(requestedMode),
		}
		worktree.apply(&p, text)
		go a.startSession(workspace, p, text, images)
	}
	ui.Column(c).Role(ui.RoleGroup).Label(L("Message composer")).Gap(k.Px(6)).Children(func() {
		card := ui.Column(c).Radius(k.Px(18)).Border(1, t.Input).Background(t.Card)
		if drop, ok := ui.DropData(card, dropOptions); ok {
			d := readDrop(drop.Data)
			if err := attachFiles(&s.text, &s.images, d.files, a.following()); err != nil {
				s.err = err.Error()
			}
			if d.image != nil {
				if img, ok, _ := attachments.FromBytes(d.image, "Dropped image.png", uuid.NewString()); ok {
					s.images = append(s.images, img)
				}
			}
			if pasted.Long(d.text) {
				s.pasted = append(s.pasted, pasted.New(d.text))
			} else {
				s.text = appendWords(s.text, d.text)
			}
		}
		card.Children(func() {
			if len(s.images)+len(s.pasted) > 0 {
				if s.bitmaps == nil {
					s.bitmaps = map[string]*ui.Bitmap{}
				}
				a.attachmentList(c, &s.images, &s.pasted, s.bitmaps, nil, func(text string) {
					s.text = appendLines(s.text, text)
					s.caretToEnd = true
				})
			}
			// In a column the text area's Grow(1) would take its height, as
			// the composer's row gives it the width instead.
			var in ui.Element
			ui.Row(c).AlignItems(ui.Start).Children(func() {
				placeholder, left := L("Do anything…"), float32(16)
				if hasPick {
					a.slashTag(c, picked, func() { s.text = rest })
					left = 8
					if picked.ArgumentHint != "" {
						placeholder = l10n.T(picked.ArgumentHint)
					}
				}
				in = k.TextArea(c, &s.field, L("Message"), placeholder, kit.AreaStyle{Pad: [4]float32{16, 16, 8, left}, Size: k.Dense(13, 14), Line: k.Dense(22, 24),
					MinLines: 2, MaxLines: 8, Color: t.Foreground}).Disabled(!enabled).AutoFocus()
			})
			if s.caretToEnd {
				s.caretToEnd = false
				n := utf8.RuneCountInString(s.field)
				in.SetTextSelection(n, n)
			}
			paste := func() bool {
				if paths := a.clipboardFiles(); len(paths) > 0 {
					if err := attachFiles(&s.text, &s.images, paths, a.following()); err != nil {
						s.err = err.Error()
					}
					return true
				}
				if a.cfg.ReadImage == nil {
					return false
				}
				data := a.cfg.ReadImage()
				if len(data) == 0 {
					return false
				}
				img, ok, err := attachments.FromBytes(data, "Pasted image.png", uuid.NewString())
				switch {
				case err != nil:
					s.err = err.Error()
				case ok:
					s.images = append(s.images, img)
				}
				return ok || err != nil
			}
			in.OnPaste(func(text string) bool {
				if paste() {
					return true
				}
				if !pasted.Long(text) {
					return false
				}
				s.pasted = append(s.pasted, pasted.New(text))
				return true
			})
			in.OnTextKey(func(key ui.TextKey) bool {
				if s.menu.key(key, s.text, accept) {
					return true
				}
				if hasPick && key.Key == ui.KeyBackspace && key.Text() == "" {
					s.text = rest
					return true
				}
				if key.Key == ui.KeyEnter && key.Mods&ui.Shift == 0 {
					s.field = key.Text()
					s.takeEdit(&s.text)
					start()
					return true
				}
				return false
			})
			s.menu.view(c, a, card, accept)
			ui.Row(c).Gap(k.Px(4)).Padding(k.Px(4), k.Px(8), k.Px(8), k.Px(8)).Children(func() {
				add := k.Button(c, kit.Ghost, 32, true, L("Add image")).Radius(k.Px(16)).Disabled(!enabled)
				add.Children(func() { k.Icon(c, "plus", 16, t.MutedForeground) })
				if add.Clicked() {
					go func() {
						paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Title: L("Choose images"), Multiple: true,
							Filters: []mygo.FileFilter{{Name: L("Images"), Extensions: []string{"png", "jpg", "jpeg", "gif", "webp"}}}})
						if err != nil {
							return
						}
						a.cfg.Update(func() {
							for _, p := range paths {
								if img, ok, _ := attachments.ReadFile(p, p); ok {
									s.images = append(s.images, img)
								}
							}
						})
					}()
				}
				ui.Row(c).Grow(1).MinWidth(0).Wrap().Gap(k.Px(2)).Children(func() {
					a.modelPicker(c, &s.modelOpen, &s.picker, choices, modelID, effort, func(id string) {
						s.modelID = id
						for _, m := range choices {
							if m.ID == id && !contains(m.ReasoningEfforts, effort) {
								s.effort = ""
								if len(m.ReasoningEfforts) > 0 {
									s.effort = m.ReasoningEfforts[0]
								}
							}
						}
					}, func(e string) { s.effort = e }, &pickerToolMode{Value: shownMode, OnChange: func(m defaults.ToolMode) { s.toolMode = m }})
					if spec := interaction == string(protocol.DroidInteractionModeSpec); specToggle(c, k, spec) {
						s.interaction = string(protocol.DroidInteractionModeSpec)
						if spec {
							s.interaction = string(protocol.DroidInteractionModeAuto)
						}
					}
					opts := make([]kit.Option, len(autonomyLevels))
					for i, l := range autonomyLevels {
						opts[i] = kit.Option{Value: l, Label: l10n.T(models.AutonomyLabels[l])}
					}
					if next, ok := k.QuietSelect(c, &s.autoOpen, L("Autonomy"), "shield-check", autonomy, opts); ok {
						s.autonomy = next
					}
				})
				ui.Row(c).Padding(0, 0, 0, k.Px(4)).Shrink(0).Children(func() {
					send := k.Button(c, kit.Primary, 32, true, L("Start session")).Radius(k.Px(16)).Disabled(!enabled)
					send.Children(func() {
						if creating {
							k.Spinner(c, 16, t.PrimaryForeground)
						} else {
							k.Icon(c, "arrow-up", 16, t.PrimaryForeground)
						}
					})
					if send.Clicked() {
						start()
					}
				})
			})
		})
	})
}

// startSession validates the Workspace (or makes a Scratch one), asks the
// Daemon for a Session there and opens it, the first message waiting for
// it to load.
func (a *App) startSession(workspace string, p protocol.InitializeSessionParams, text string, images []attachments.Image) {
	s := &a.newPage
	fail := func(msg string) {
		s.mu.Lock()
		s.creating = false
		s.mu.Unlock()
		a.cfg.Update(func() { s.err = msg })
	}
	s.mu.Lock()
	s.creating = true
	s.mu.Unlock()
	a.redraw()
	cl, err := a.ctl.Client()
	if err != nil {
		fail(err.Error())
		return
	}
	id := a.takeDraft(workspace, p)
	if id == "" {
		var tags []protocol.SessionTag
		if workspace == scratchPick {
			var dir string
			err := gateway.ErrNotFound
			if a.cfg.Gateway != nil {
				dir, err = a.cfg.Gateway.CreateScratch(a.ctx)
			}
			if errors.Is(err, gateway.ErrNotFound) {
				fail(a.noScratchFolder())
				return
			}
			if err != nil {
				fail(err.Error())
				return
			}
			workspace = dir
			tags = []protocol.SessionTag{{Name: sessions.ScratchTag}}
		}
		if p.Cwd, err = a.validWorkspace(cl, workspace); err != nil {
			fail(err.Error())
			return
		}
		p.Tags = tags
		res, err := a.ctl.InitializeSession(a.ctx, p)
		if err != nil {
			fail(err.Error())
			return
		}
		id = res.SessionID
	}
	if strings.TrimSpace(text) != "" || len(images) > 0 {
		a.pending.Set(id, drafts.PendingPrompt{Text: text, Images: images})
	}
	go a.refreshList()
	// The page stays creating until it is left: a frame built between would
	// see no draft and open another.
	a.cfg.Update(func() {
		s.mu.Lock()
		s.creating = false
		s.mu.Unlock()
		s.text, s.images, s.pasted, s.bitmaps = "", nil, nil, nil
		a.Go(Route{Name: "session", SessionID: id})
	})
}

// validWorkspace is the directory the Daemon resolves workspace to, or why
// it cannot work there.
func (a *App) validWorkspace(cl *droid.Client, workspace string) (string, error) {
	check, err := cl.ValidateWorkingDirectory(a.ctx, protocol.ValidateWorkingDirectoryParams{WorkingDirectory: workspace})
	if err != nil {
		return "", err
	}
	if !check.IsValid {
		msg := check.Error
		if msg == "" {
			msg = L("%s is not a usable directory.", workspace)
		}
		return "", errors.New(msg)
	}
	if check.ResolvedPath != "" {
		return check.ResolvedPath, nil
	}
	return workspace, nil
}

// droiMark is the app's mark, drawn as the web Client's DroiMark.
func droiMark(c *ui.Context, k *kit.Kit, size float32) {
	if logo := droiLogo(); logo != nil {
		ui.Image(c, logo).Size(k.Px(size), k.Px(size)).Label("Droi").TextColor(k.T.Foreground)
	}
}

var droiLogo = sync.OnceValue(func() *ui.SVG {
	s, _ := ui.ParseSVG([]byte(droiMarkSVG))
	return s
})

// droiMarkSVG is resources/icon.svg's "D" with its green dot, without the
// tile; the D takes the text color.
const droiMarkSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="302 292 440 440"><rect x="346" y="292" width="112" height="440" rx="36" fill="currentColor"/><path d="M478 328Q478 292 514 294.97A220 220 0 0 1 514 729.03Q478 732 478 696Z" fill="currentColor"/><circle cx="571" cy="512" r="54" fill="#34c77b"/></svg>`
