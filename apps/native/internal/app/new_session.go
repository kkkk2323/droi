package app

import (
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/google/uuid"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/controller"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/internal/attachments"
	"github.com/kkkk2323/droi/apps/native/internal/defaults"
	"github.com/kkkk2323/droi/apps/native/internal/drafts"
	"github.com/kkkk2323/droi/apps/native/internal/kit"
	"github.com/kkkk2323/droi/apps/native/internal/models"
	"github.com/kkkk2323/droi/apps/native/internal/sessions"
)

// newSessionState is the New session page's: the Workspace picked, the
// settings changed on the page, and the composer.
type newSessionState struct {
	// pick is "" until the user picks (the most recent Workspace is the
	// target), "scratch" for None, or a path.
	pick     string
	picked   bool
	menuOpen bool

	modelID, effort, autonomy string
	toolMode                  defaults.ToolMode

	text    string
	images  []attachments.Image
	bitmaps map[string]*ui.Bitmap
	err     string

	modelOpen bool
	picker    pickerState
	autoOpen  bool

	mu       sync.Mutex
	creating bool
	defaults *defaults.View
	readAt   time.Time
}

const scratchPick = "scratch"

// reset starts the page afresh: from a sidebar group, with its Workspace.
func (s *newSessionState) reset(r Route) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pick, s.picked = "", false
	s.modelID, s.effort, s.autonomy, s.toolMode = "", "", "", ""
	s.err = ""
	switch {
	case r.Scratch:
		s.pick, s.picked = scratchPick, true
	case r.Workspace != "":
		s.pick, s.picked = r.Workspace, true
	}
}

// sessionDefaults are the Daemon's defaults for a new Session, read again
// a minute after they were.
func (a *App) sessionDefaults() *defaults.View {
	s := &a.newPage
	s.mu.Lock()
	defer s.mu.Unlock()
	// Before the connection is up the Daemon would refuse the request.
	if time.Since(s.readAt) > time.Minute && a.ctl != nil && a.ctl.Status().Connected {
		s.readAt = time.Now()
		failed := func() {
			s.mu.Lock()
			s.readAt = time.Now().Add(5*time.Second - time.Minute)
			s.mu.Unlock()
		}
		go func() {
			cl, err := a.ctl.Client()
			if err != nil {
				failed()
				return
			}
			res, err := cl.GetDefaultSettings(a.ctx)
			if err != nil {
				failed()
				return
			}
			var raw map[string]any
			b, _ := json.Marshal(res)
			_ = json.Unmarshal(b, &raw)
			view := defaults.ToSessionDefaults(raw)
			s.mu.Lock()
			s.defaults = &view
			s.mu.Unlock()
			a.redraw()
		}()
	}
	return s.defaults
}

// newSessionPage is the Waku-style start page: one question with the
// Workspace as a menu in it, and the composer under it. The first message
// rides along into the Session.
func (a *App) newSessionPage(c *ui.Context, listed []sessions.Summary, status controller.Status) {
	k, t := a.kit, a.kit.T
	s := &a.newPage
	recent := sessions.RecentWorkspaces(listed)
	workspace := s.pick
	if !s.picked && len(recent) > 0 {
		workspace = recent[0].Path
	}
	scratch := workspace == scratchPick
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

	ui.Column(c).Role(ui.RoleGroup).Label("New session").Fill().Children(func() {
		ui.Row(c).Height(k.Px(44)).Shrink(0).PaddingX(k.Px(8)).AlignItems(ui.Center).DragWindow().Children(func() {
			if a.narrow {
				a.openSessionsButton(c)
			}
		})
		// Not a Scroll: one lays its children out in an unbounded height, so
		// they could not be centred in it.
		ui.Column(c).Grow(1).MinHeight(0).ClipY().Children(func() {
			ui.Column(c).Fill().Center().Gap(k.Px(20)).PaddingX(k.Px(24)).Children(func() {
				droiMark(c, k, 36)
				ui.Row(c).Role(ui.RoleHeading).FillWidth().Wrap().Justify(ui.Center).GapX(k.Px(6)).Children(func() {
					h := func(s string) { k.Text(c, s, 20, 28).FontWeight(500).LetterSpacing(-k.Px(0.5)) }
					switch {
					case workspace != "" && !scratch:
						h("What do you want to build in")
						ui.Row(c).Children(func() {
							a.workspaceMenu(c, recent, workspace, labelOf(recent, workspace), 20)
							h("?")
						})
					case scratch:
						h("What’s on your mind?")
						ui.Row(c).FillWidth().Justify(ui.Center).Children(func() {
							a.workspaceMenu(c, recent, scratchPick, "Workspace: None", 14)
						})
					default:
						h("Where should Droid work?")
						if len(listed) > 0 || status.Connected {
							a.workspaceMenu(c, recent, "", "Choose a workspace", 20)
						}
					}
				})
				if s.err != "" {
					k.Text(c, s.err, 14, 20).Role(ui.RoleStatus).TextColor(t.DestructiveForeground)
				}
			})
		})
		col := ui.Column(c).FillWidth().MaxWidth(k.Px(columnWidth)).AlignSelf(ui.Center).Padding(0, k.Px(24), k.Px(12), k.Px(24)).Shrink(0)
		col.Children(func() {
			a.newSessionComposer(c, workspace, creating, choices, modelID, effort, autonomy, shownMode, requestedMode)
			ui.Row(c).Height(k.Px(28)).Gap(k.Px(12)).PaddingX(k.Px(8)).Children(func() {
				if workspace == "" {
					return
				}
				path, icon := workspace, "folder"
				if scratch {
					path, icon = a.scratchRoot(), "message-square-dashed"
				}
				ui.Row(c).Gap(k.Px(6)).MinWidth(0).Tooltip(path).Children(func() {
					k.Icon(c, icon, 14, t.MutedForeground)
					k.Text(c, path, 12, 16).TextColor(t.MutedForeground).SingleLine()
				})
			})
		})
	})
}

func (a *App) scratchRoot() string {
	if a.cfg.Scratch != nil {
		return a.cfg.Scratch.Root()
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

// workspaceMenu is the question's Workspace, underlined with dashes, as a
// menu of None, the recent Workspaces and another folder.
func (a *App) workspaceMenu(c *ui.Context, recent []sessions.RecentWorkspace, value, label string, size float32) {
	k, t := a.kit, a.kit.T
	s := &a.newPage
	lh := size * 1.4
	tr := ui.ButtonBase(c).Label("Workspace").Expanded(s.menuOpen).Gap(k.Px(2)).BorderWidth(0, 0, 1, 0).Cursor(ui.CursorPointer)
	line := t.MutedForeground.Alpha(0.5)
	if tr.Hovered() || s.menuOpen {
		line = t.Foreground
	}
	tr.BorderColor(line).BorderStyle(ui.BorderDashed)
	tr.Children(func() {
		color := t.Foreground
		weight := 500
		if size < 20 {
			color, weight = t.MutedForeground, 400
		}
		k.Text(c, label, size, lh).FontWeight(weight).TextColor(color).LetterSpacing(-k.Px(size * 0.025))
		k.Icon(c, "chevron-down", 14, color).Opacity(0.5)
	})
	if tr.Clicked() {
		s.menuOpen = !s.menuOpen
	}
	choose := func(v string) {
		s.pick, s.picked = v, true
		s.err = ""
	}
	k.MenuPopup(c, tr, &s.menuOpen, false, "Workspace", func() {
		item := func(key, text, title string) {
			b := ui.ButtonBase(c).Key(key).Role(ui.RoleMenuItemRadio).Checked(value == key).Label(text).Tooltip(title).
				MinWidth(k.Px(192)).Gap(k.Px(12)).Padding(k.Px(6), k.Px(8), k.Px(6), k.Px(10)).Radius(k.Px(6)).Justify(ui.Start).Cursor(ui.CursorPointer)
			color := t.PopoverForeground
			if b.Hovered() {
				b.Background(t.Accent)
				color = t.AccentForeground
			}
			b.Children(func() {
				k.Text(c, text, 14, 20).TextColor(color).SingleLine().Grow(1).MaxWidth(k.Px(320))
				ui.Box(c).Size(k.Px(16), k.Px(16)).Center().Children(func() {
					if value == key {
						k.Icon(c, "check", 14, color)
					}
				})
			})
			if b.Clicked() {
				s.menuOpen = false
				choose(key)
			}
		}
		sep := func() { ui.Box(c).Height(1).Margin(k.Px(4), 0).Background(t.Border) }
		item(scratchPick, "None", "A new folder of its own, not in any project")
		if len(recent) > 0 {
			sep()
		}
		for _, w := range recent {
			item(w.Path, w.Label, w.Path)
		}
		sep()
		if k.MenuItem(c, &s.menuOpen, "folder-plus", "Other folder…").Clicked() {
			go func() {
				paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Title: "Choose a workspace", Directory: true})
				if err == nil && len(paths) > 0 {
					a.cfg.Update(func() { choose(paths[0]) })
				}
			}()
		}
	})
}

// newSessionComposer is the InputBar of the start page: Enter starts the
// Session, sending what was typed as its first message.
func (a *App) newSessionComposer(c *ui.Context, workspace string, creating bool, choices []models.Choice, modelID, effort, autonomy string, shownMode, requestedMode defaults.ToolMode) {
	k, t := a.kit, a.kit.T
	s := &a.newPage
	enabled := workspace != "" && !creating
	start := func() {
		if !enabled {
			return
		}
		text, images := s.text, s.images
		go a.startSession(workspace, protocol.InitializeSessionParams{
			ModelID:           modelID,
			ReasoningEffort:   protocol.ReasoningEffort(effort),
			AutonomyLevel:     protocol.AutonomyLevel(autonomy),
			ToolExecutionMode: protocol.ToolExecutionMode(requestedMode),
		}, text, images)
	}
	ui.Column(c).Role(ui.RoleGroup).Label("Message composer").Gap(k.Px(6)).Children(func() {
		card := ui.Column(c).Radius(k.Px(16)).Border(1, t.Border).Background(t.Background)
		if files := card.DroppedFiles(); len(files) > 0 {
			for _, p := range files {
				if img, ok, _ := attachments.ReadFile(p, p); ok {
					s.images = append(s.images, img)
				}
			}
		}
		card.Children(func() {
			if len(s.images) > 0 {
				if s.bitmaps == nil {
					s.bitmaps = map[string]*ui.Bitmap{}
				}
				a.attachmentList(c, &s.images, s.bitmaps, nil)
			}
			// In a column the text area's Grow(1) would take its height, as
			// the composer's row gives it the width instead.
			var in *ui.Element
			ui.Row(c).Children(func() {
				in = k.TextArea(c, &s.text, "Message", "Do anything…", kit.AreaStyle{Pad: [4]float32{14, 16, 4, 16}, Size: 14, Line: 24,
					MinLines: 1, MaxHeight: 224, Color: t.Foreground, Muted: t.MutedForeground}).Disabled(!enabled).AutoFocus()
			})
			paste := func() bool {
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
			if in.Focused() {
				a.pasteTo = paste
			}
			in.HandleInput(func(ev ui.InputEvent) bool {
				if isPaste(ev) && paste() {
					return true
				}
				if ev.Kind == ui.InputKeyDown && ev.Key == ui.KeyEnter && ev.Mods&ui.Shift == 0 && !in.Composing() {
					start()
					return true
				}
				return false
			})
			ui.Row(c).Gap(k.Px(4)).Padding(k.Px(4), k.Px(8), k.Px(8), k.Px(8)).Children(func() {
				add := k.Button(c, kit.Ghost, 32, true, "Add image").Radius(k.Px(16)).Disabled(!enabled)
				add.Children(func() { k.Icon(c, "plus", 16, t.MutedForeground) })
				if add.Clicked() {
					go func() {
						paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Title: "Choose images", Multiple: true,
							Filters: []mygo.FileFilter{{Name: "Images", Extensions: []string{"png", "jpg", "jpeg", "gif", "webp"}}}})
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
					}, func(e string) { s.effort = e }, &pickerToolMode{value: shownMode, onChange: func(m defaults.ToolMode) { s.toolMode = m }})
					opts := make([]kit.Option, len(autonomyLevels))
					for i, l := range autonomyLevels {
						opts[i] = kit.Option{Value: l, Label: models.AutonomyLabels[l]}
					}
					if next, ok := k.QuietSelect(c, &s.autoOpen, "Autonomy", "shield-check", autonomy, opts); ok {
						s.autonomy = next
					}
				})
				ui.Row(c).Padding(0, 0, 0, k.Px(4)).Shrink(0).Children(func() {
					send := k.Button(c, kit.Primary, 32, true, "Start session").Radius(k.Px(16)).Disabled(!enabled)
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
	var tags []protocol.SessionTag
	if workspace == scratchPick {
		if a.cfg.Scratch == nil {
			fail("This computer has no Scratch folder.")
			return
		}
		dir, err := a.cfg.Scratch.Create()
		if err != nil {
			fail(err.Error())
			return
		}
		workspace = dir
		tags = []protocol.SessionTag{{Name: sessions.ScratchTag}}
	}
	cl, err := a.ctl.Client()
	if err != nil {
		fail(err.Error())
		return
	}
	check, err := cl.ValidateWorkingDirectory(a.ctx, protocol.ValidateWorkingDirectoryParams{WorkingDirectory: workspace})
	if err != nil {
		fail(err.Error())
		return
	}
	if !check.IsValid {
		msg := check.Error
		if msg == "" {
			msg = workspace + " is not a usable directory."
		}
		fail(msg)
		return
	}
	p.Cwd = workspace
	if check.ResolvedPath != "" {
		p.Cwd = check.ResolvedPath
	}
	p.Tags = tags
	if a.cfg.SystemPrompt != nil {
		p.SystemPrompt = a.cfg.SystemPrompt()
	}
	res, err := a.ctl.InitializeSession(a.ctx, p)
	if err != nil {
		fail(err.Error())
		return
	}
	if strings.TrimSpace(text) != "" || len(images) > 0 {
		a.pending.Set(res.SessionID, drafts.PendingPrompt{Text: text, Images: images})
	}
	s.mu.Lock()
	s.creating = false
	s.mu.Unlock()
	go a.refreshList()
	a.cfg.Update(func() {
		s.text, s.images, s.bitmaps = "", nil, nil
		a.Go(Route{Name: "session", SessionID: res.SessionID})
	})
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
