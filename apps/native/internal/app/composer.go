package app

import (
	"encoding/json"
	"math"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/google/uuid"
	droid "github.com/kkkk2323/droi/packages/droid-sdk-go"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/protocol"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/session"

	"github.com/kkkk2323/droi/apps/native/internal/attachments"
	"github.com/kkkk2323/droi/apps/native/internal/kit"
	"github.com/kkkk2323/droi/apps/native/internal/sessions"
	"github.com/kkkk2323/droi/apps/native/internal/slash"
)

// compactCommand is `/compact [instructions]`, which the composer runs
// itself.
var compactCommand = regexp.MustCompile(`^/compact(?:\s+([\s\S]*))?$`)

// composerState is the InputBar's state for one Session: the text and
// images typed, and the slash suggestions' highlight.
type composerState struct {
	text      string
	images    []attachments.Image
	highlight int
	dismissed string // the text the suggestions were dismissed for
	err       string

	modelOpen bool
	picker    pickerState
	autoOpen  bool
	toolsOpen bool
	toolsTab  string

	// Guarded by mu: filled by goroutines.
	mu      sync.Mutex
	budget  *protocol.GetContextBreakdownResult
	budgetM string // the model the budget is for
	slash   []slash.Item
	slashAt time.Time
	skills  []protocol.SkillInfo
	servers []protocol.MCPServerStatusInfo
	toolsAt time.Time
}

// composerArea is everything under the transcript: the shelf, the
// composer or the Prompts standing in for it, and the footer row.
func (v *sessionView) composerArea(c *ui.Context, s *session.Session, workspace string, loaded bool) {
	a := v.a
	k := a.kit
	if s == nil {
		return
	}
	if loaded {
		v.takePending(s)
	}
	perms, asks := a.ctl.PendingPermissions(v.id), a.ctl.PendingAskUsers(v.id)
	running := s.WorkingState() != protocol.DroidWorkingStateIdle || v.compacting
	if running && len(perms)+len(asks) == 0 && c.Shortcut(0, ui.KeyEscape) {
		go func() { _ = a.ctl.InterruptSession(a.ctx, v.id) }()
	}
	v.column(c).Shrink(0).Padding(0, k.Px(24), k.Px(12), k.Px(24)).Children(func() {
		v.shelf(c, s)
		if len(perms)+len(asks) > 0 {
			v.promptArea(c, perms, asks)
		} else {
			v.inputBar(c, s, running, loaded)
		}
		v.footer(c, s, workspace, loaded)
	})
}

// takePending sends the message typed on the New session page once the
// Session can take it.
func (v *sessionView) takePending(s *session.Session) {
	if p, ok := v.a.pending.Take(v.id); ok {
		v.send(s, p.Text, p.Images, "")
	}
}

// send sends a message, or queues it with placement while a turn runs; an
// idle send shows at once as an optimistic message.
func (v *sessionView) send(s *session.Session, text string, images []attachments.Image, placement protocol.QueuePlacement) {
	a := v.a
	trimmed := strings.TrimSpace(text)
	if trimmed == "" && len(images) == 0 {
		return
	}
	if m := compactCommand.FindStringSubmatch(trimmed); m != nil && len(images) == 0 {
		go v.compact(m[1])
		return
	}
	v.composer.err = ""
	requestID, messageID := uuid.NewString(), uuid.NewString()
	var content []protocol.ContentBlock
	if trimmed != "" {
		b, _ := protocol.NewContentBlock(map[string]any{"type": "text", "text": trimmed})
		content = append(content, b)
	}
	sources := make([]protocol.Base64ImageSource, len(images))
	for i, img := range images {
		sources[i] = protocol.Base64ImageSource{Type: "base64", Data: img.Data, MediaType: protocol.Base64ImageSourceMediaType(img.MediaType)}
		b, _ := protocol.NewContentBlock(map[string]any{"type": "image", "source": sources[i]})
		content = append(content, b)
	}
	if placement == "" {
		now := float64(time.Now().UnixMilli())
		a.ctl.Store().RegisterOptimisticSubmit(session.OptimisticSubmit{
			SessionID:         v.id,
			ExternalKey:       requestID,
			UserMessage:       protocol.FactoryDroidMessage{ID: messageID, Role: protocol.MessageRoleUser, Content: content, CreatedAt: now, UpdatedAt: now},
			AssistantBubbleID: uuid.NewString(),
			OnError:           func(err error) { v.fail(err) },
		})
	}
	v.list.FollowEnd = true
	v.list.ScrollToEnd()
	p := protocol.AddUserMessageParams{SessionID: v.id, Text: trimmed, MessageID: messageID, QueuePlacement: placement}
	if len(sources) > 0 {
		p.Images = sources
	}
	go func() {
		if _, err := a.ctl.AddUserMessage(droid.WithRequestID(a.ctx, requestID), p); err != nil {
			if placement == "" {
				a.ctl.Store().CancelOptimisticSubmit(requestID)
			}
			v.fail(err)
		}
	}()
}

func (v *sessionView) fail(err error) {
	v.a.cfg.Update(func() { v.composer.err = err.Error() })
}

// compact runs `/compact`: in place on a newer Daemon, else as a handoff
// to a child Session, which this Client tags as the continuation and shows.
func (v *sessionView) compact(instructions string) {
	a := v.a
	v.a.cfg.Update(func() { v.compacting = true })
	a.compactions.Start(v.id)
	defer a.cfg.Update(func() { v.compacting = false })
	cl, err := a.ctl.Client()
	var res *protocol.CompactSessionResult
	if err == nil {
		res, err = cl.CompactSession(a.ctx, protocol.CompactSessionParams{SessionID: v.id, CustomInstructions: strings.TrimSpace(instructions)})
	}
	if err != nil {
		a.compactions.Fail(v.id)
		v.fail(err)
		return
	}
	if res.NewSessionID != v.id {
		_, _ = a.ctl.LoadSession(a.ctx, protocol.LoadSessionParams{SessionID: res.NewSessionID})
		var inherited []sessions.Tag
		if s := a.ctl.Store().Session(v.id); s != nil {
			for _, t := range s.Settings().Tags {
				inherited = append(inherited, sessions.Tag{Name: t.Name, Metadata: t.Metadata})
			}
		}
		tags := sessions.ContinuationTags(v.id, inherited)
		pt := make([]protocol.SessionTag, len(tags))
		for i, t := range tags {
			pt[i] = protocol.SessionTag{Name: t.Name, Metadata: t.Metadata}
		}
		if cl, err := a.ctl.Client(); err == nil {
			_, _ = cl.UpdateSessionSettings(a.ctx, protocol.UpdateSessionSettingsParams{SessionID: res.NewSessionID, Tags: pt})
		}
		a.refreshList()
	}
	a.compactions.Finish(v.id, res.NewSessionID, int(res.RemovedCount), time.Now().UnixMilli())
	if res.NewSessionID != v.id {
		a.cfg.Update(func() {
			if a.route.Name == "session" && a.route.SessionID == v.id {
				a.Go(Route{Name: "session", SessionID: res.NewSessionID})
			}
		})
	}
}

// slashItems are the builtins and the Session's commands and skills, read
// again a minute after they were.
func (v *sessionView) slashItems() []slash.Item {
	cs := &v.composer
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if time.Since(cs.slashAt) > time.Minute && v.a.ctl != nil {
		cs.slashAt = time.Now()
		go func() {
			cl, err := v.a.ctl.Client()
			if err != nil {
				return
			}
			cmds, err1 := cl.ListCommands(v.a.ctx, protocol.ListCommandsParams{SessionID: v.id})
			sk, err2 := cl.ListSkills(v.a.ctx, protocol.ListSkillsParams{SessionID: v.id})
			if err1 != nil || err2 != nil {
				return
			}
			cs.mu.Lock()
			cs.slash = slash.FromDaemon(cmds.Commands, sk.Skills)
			cs.skills = sk.Skills
			cs.mu.Unlock()
			v.a.redraw()
		}()
	}
	return slash.Merge(slash.Builtins, cs.slash)
}

// inputBar is the InputBar: attachments and text in a rounded card, the
// settings and the send button in its footer row. Enter sends,
// Shift+Enter breaks the line; while a turn runs Enter queues and Cmd+Enter
// hands the message to the running turn.
func (v *sessionView) inputBar(c *ui.Context, s *session.Session, running, loaded bool) {
	a := v.a
	k, t := a.kit, a.kit.T
	cs := &v.composer
	items := v.slashItems()
	picked, rest, hasPick := slash.Picked(cs.text, items)
	prefix := ""
	if hasPick {
		prefix = cs.text[:len(cs.text)-len(rest)]
	}
	var suggestions []slash.Item
	if q, ok := slash.Query(cs.text, len(cs.text)); ok && cs.dismissed != cs.text {
		suggestions = slash.Filter(items, q, 8)
	}
	active := min(cs.highlight, max(len(suggestions)-1, 0))
	canSend := loaded && (strings.TrimSpace(cs.text) != "" || len(cs.images) > 0)
	submit := func(placement protocol.QueuePlacement) {
		if !canSend {
			return
		}
		if running && placement == "" {
			placement = protocol.QueuePlacementEndOfLoop
		}
		if !running {
			placement = ""
		}
		v.send(s, cs.text, cs.images, placement)
		cs.text, cs.images = "", nil
		v.saveDraft()
	}
	accept := func(item slash.Item) {
		cs.text = "/" + item.Name + " "
		cs.highlight = 0
		v.saveDraft()
	}

	ui.Column(c).Role(ui.RoleGroup).Label("Message composer").Gap(k.Px(6)).Children(func() {
		if cs.err != "" {
			k.Text(c, cs.err, 12, 16).Role(ui.RoleStatus).TextColor(t.DestructiveForeground).PaddingX(k.Px(4))
		}
		card := ui.Column(c).Radius(k.Px(16)).Border(1, t.Border).Background(t.Background)
		if card.FileDragOver() {
			card.Border(1, t.Primary.Alpha(0.6)).Background(t.Primary.Alpha(0.05))
		}
		if files := card.DroppedFiles(); len(files) > 0 {
			v.addFiles(files)
		}
		card.Children(func() {
			if len(cs.images) > 0 {
				v.attachmentList(c)
			}
			ui.Row(c).AlignItems(ui.Start).Children(func() {
				if hasPick {
					v.slashTag(c, picked, rest)
				}
				value := cs.text[len(prefix):]
				placeholder := "Ask anything"
				switch {
				case running:
					placeholder = "Queue a message… (⌘↩ inserts it now)"
				case hasPick && picked.ArgumentHint != "":
					placeholder = picked.ArgumentHint
				}
				left := k.Px(16)
				if hasPick {
					left = k.Px(8)
				}
				in := k.TextArea(c, &value, "Message", placeholder, kit.AreaStyle{Pad: [4]float32{14, 16, 4, left / k.Px(1)}, Size: 14, Line: 24,
					MinLines: 1, MaxHeight: 224, Color: t.Foreground, Muted: t.MutedForeground}).Disabled(!loaded)
				if !v.focused {
					in.AutoFocus()
					v.focused = true
				}
				in.HandleInput(func(ev ui.InputEvent) bool {
					if isPaste(ev) {
						return v.pasteImage()
					}
					if ev.Kind != ui.InputKeyDown {
						return false
					}
					if len(suggestions) > 0 {
						switch {
						case ev.Key == ui.KeyDown:
							cs.highlight = (active + 1) % len(suggestions)
							return true
						case ev.Key == ui.KeyUp:
							cs.highlight = (active - 1 + len(suggestions)) % len(suggestions)
							return true
						case ev.Key == ui.KeyTab || ev.Key == ui.KeyEnter && ev.Mods&ui.Shift == 0:
							accept(suggestions[active])
							return true
						case ev.Key == ui.KeyEscape:
							cs.dismissed = cs.text
							return true
						}
					}
					if hasPick && ev.Key == ui.KeyBackspace && value == "" {
						cs.text = rest
						return true
					}
					if ev.Key == ui.KeyEnter && ev.Mods&ui.Shift == 0 && !in.Composing() {
						if ev.Mods&ui.Cmd != 0 {
							submit(protocol.QueuePlacementEndOfTurn)
						} else {
							submit("")
						}
						return true
					}
					return false
				})
				if in.Changed() {
					cs.text = prefix + value
					cs.highlight = 0
					v.saveDraft()
				}
			})
			if len(suggestions) > 0 {
				v.suggestionList(c, card, suggestions, active, accept)
			}
			ui.Row(c).Gap(k.Px(4)).Padding(k.Px(4), k.Px(8), k.Px(8), k.Px(8)).Children(func() {
				add := k.Button(c, kit.Ghost, 32, true, "Add image").Radius(k.Px(16)).Disabled(!loaded)
				add.Children(func() { k.Icon(c, "plus", 16, t.MutedForeground) })
				if add.Clicked() {
					go v.chooseImages()
				}
				ui.Row(c).Grow(1).MinWidth(0).Wrap().Gap(k.Px(2)).Children(func() {
					v.settingsBar(c, s)
				})
				v.toolsButton(c)
				ui.Row(c).Gap(k.Px(4)).Padding(0, 0, 0, k.Px(4)).Shrink(0).Children(func() {
					if running {
						stop := k.Button(c, kit.Secondary, 32, true, "Cancel").Radius(k.Px(16))
						stop.Children(func() {
							ui.Box(c).Size(k.Px(10), k.Px(10)).Radius(k.Px(2)).Background(t.SecondaryForeground)
						})
						if stop.Clicked() {
							go func() { _ = a.ctl.InterruptSession(a.ctx, v.id) }()
						}
						if canSend {
							q := k.Button(c, kit.Primary, 32, true, "Queue").Radius(k.Px(16)).Tooltip("Send after this turn (↩); ⌘↩ hands it to the running turn")
							q.Children(func() { k.Icon(c, "arrow-up", 16, t.PrimaryForeground) })
							if q.Clicked() {
								submit("")
							}
						}
						return
					}
					send := k.Button(c, kit.Primary, 32, true, "Send").Radius(k.Px(16)).Disabled(!canSend)
					send.Children(func() { k.Icon(c, "arrow-up", 16, t.PrimaryForeground) })
					if send.Clicked() {
						submit("")
					}
				})
			})
		})
	})
}

func (v *sessionView) saveDraft() {
	v.a.drafts.Save(v.id, draftOf(v.composer.text, v.composer.images))
}

// addFiles attaches the images of files; other files go in as their paths.
// isPaste is ⌘V, which a text area takes as a key, or Paste of the Edit
// menu, which comes as a command.
func isPaste(ev ui.InputEvent) bool {
	return ev.Kind == ui.InputCommand && ev.Text == "paste" || ev.Kind == ui.InputKeyDown && ev.Key == ui.KeyV && ev.Mods == ui.Cmd
}

// pasteImage attaches the clipboard's image, as the web Client takes one
// pasted; with none it lets the text area paste the text.
func (v *sessionView) pasteImage() bool {
	if v.a.cfg.ReadImage == nil {
		return false
	}
	data := v.a.cfg.ReadImage()
	if len(data) == 0 {
		return false
	}
	img, ok, err := attachments.FromBytes(data, "Pasted image.png", uuid.NewString())
	switch {
	case err != nil:
		v.composer.err = err.Error()
	case ok:
		v.composer.images = append(v.composer.images, img)
	}
	return ok || err != nil
}

func (v *sessionView) addFiles(paths []string) {
	cs := &v.composer
	var others []string
	for _, p := range paths {
		img, ok, err := attachments.ReadFile(p, uuid.NewString())
		switch {
		case err != nil:
			cs.err = err.Error()
		case ok:
			cs.images = append(cs.images, img)
		default:
			others = append(others, p)
		}
	}
	if len(others) > 0 {
		quoted := make([]string, len(others))
		for i, p := range others {
			quoted[i] = p
			if strings.ContainsAny(p, " \t") {
				quoted[i] = strconv.Quote(p)
			}
		}
		insert := strings.Join(quoted, " ")
		if cs.text != "" && !strings.HasSuffix(cs.text, " ") && !strings.HasSuffix(cs.text, "\n") {
			insert = " " + insert
		}
		cs.text += insert + " "
	}
	v.saveDraft()
}

func (v *sessionView) chooseImages() {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Title:    "Choose images",
		Multiple: true,
		Filters:  []mygo.FileFilter{{Name: "Images", Extensions: []string{"png", "jpg", "jpeg", "gif", "webp"}}},
	})
	if err != nil || len(paths) == 0 {
		return
	}
	v.a.cfg.Update(func() { v.addFiles(paths) })
}

func (v *sessionView) attachmentList(c *ui.Context) {
	k, t := v.a.kit, v.a.kit.T
	cs := &v.composer
	ui.Row(c).Role(ui.RoleList).Label("Attachments").Wrap().Gap(k.Px(8)).Padding(k.Px(12), k.Px(12), 0, k.Px(12)).Children(func() {
		for _, img := range cs.images {
			ui.Box(c).Key(img.ID).Size(k.Px(64), k.Px(64)).Children(func() {
				if bm := v.image("att:"+img.ID, attachmentImage(img)); bm != nil {
					ui.Image(c, bm).Label(img.Name).Fill().Radius(k.Px(8)).Border(1, t.Border).Fit(ui.Cover)
				}
				x := ui.ButtonBase(c).Label("Remove "+img.Name).Size(k.Px(20), k.Px(20)).Radius(k.Px(10)).Border(1, t.Border).
					Background(t.Background).Absolute().Top(-k.Px(6)).Right(-k.Px(6)).Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, 0.05)).Cursor(ui.CursorPointer)
				color := t.MutedForeground
				if x.Hovered() {
					color = t.Foreground
				}
				x.Children(func() { k.Icon(c, "x", 12, color) })
				if x.Clicked() {
					id := img.ID
					out := cs.images[:0:0]
					for _, i := range cs.images {
						if i.ID != id {
							out = append(out, i)
						}
					}
					cs.images = out
					v.saveDraft()
				}
			})
		}
	})
}

// slashTag is the command or skill the message will run, before its text.
func (v *sessionView) slashTag(c *ui.Context, item slash.Item, rest string) {
	k, t := v.a.kit, v.a.kit.T
	kind, icon := "Command", "square-slash"
	if item.Kind == slash.Skill {
		kind, icon = "Skill", "sparkles"
	}
	ui.Row(c).Role(ui.RoleGroup).Label(kind+" "+item.Name).Tooltip(item.Description).Height(k.Px(24)).Gap(k.Px(4)).
		Margin(k.Px(14), 0, 0, k.Px(12)).Padding(0, k.Px(2), 0, k.Px(6)).Radius(k.Px(6)).Background(t.Info.Alpha(0.1)).Shrink(0).Children(func() {
		k.Icon(c, icon, 14, t.InfoForeground)
		k.Text(c, item.Name, 13, 19.5).FontWeight(500).TextColor(t.InfoForeground).SingleLine()
		x := ui.ButtonBase(c).Label("Remove "+strings.ToLower(kind)+" "+item.Name).Size(k.Px(20), k.Px(20)).Radius(k.Px(4)).Cursor(ui.CursorPointer)
		op := float32(0.6)
		if x.Hovered() {
			op = 1
		}
		x.Children(func() { k.Icon(c, "x", 12, t.InfoForeground).Opacity(op) })
		if x.Clicked() {
			v.composer.text = rest
		}
	})
}

func (v *sessionView) suggestionList(c *ui.Context, card *ui.Element, items []slash.Item, active int, accept func(slash.Item)) {
	k, t := v.a.kit, v.a.kit.T
	ui.Overlay(c, func() {
		list := ui.Column(c).Role(ui.RoleList).Label("Commands and skills").AttachTo(card, ui.AnchorTopLeft, ui.AnchorBottomLeft).
			Width(card.Bounds().W-k.Px(16)).Margin(0, 0, k.Px(8), k.Px(8)).MaxHeight(k.Px(288)).Padding(k.Px(4)).Radius(k.Px(12)).
			Border(1, t.Border).Background(t.Popover).Shadow(0, k.Px(10), k.Px(15), -k.Px(3), ui.RGBA(0, 0, 0, 0.1))
		list.Children(func() {
			for i, item := range items {
				row := ui.Row(c).Key(item.Name).Role(ui.RoleListItem).Label("/"+item.Name).Gap(k.Px(10)).Padding(k.Px(6), k.Px(10)).
					Radius(k.Px(8)).Cursor(ui.CursorPointer)
				if _, _, over := row.PointerPosition(); over && i != active {
					v.composer.highlight = i
				}
				fg := t.PopoverForeground
				if i == active {
					kit.Selected(row, true).Background(t.Accent)
					fg = t.AccentForeground
				}
				icon, kind := "square-slash", "Command"
				if item.Kind == slash.Skill {
					icon, kind = "sparkles", "Skill"
				}
				row.Children(func() {
					k.Icon(c, icon, 14, t.MutedForeground)
					k.Text(c, "/"+item.Name, 13, 19.5).FontWeight(500).TextColor(fg).Shrink(0)
					if item.ArgumentHint != "" {
						k.Text(c, item.ArgumentHint, 11, 16.5).Font(k.Mono).TextColor(t.MutedForeground).Shrink(0)
					}
					k.Text(c, item.Description, 13, 19.5).TextColor(t.MutedForeground).SingleLine().Grow(1).Shrink(1).MinWidth(0)
					k.Text(c, kind, 11, 16.5).TextColor(t.MutedForeground).Shrink(0)
				})
				if row.Clicked() {
					accept(item)
				}
			}
		})
	})
}

// footer is the row under the composer: the Workspace, a finished
// `/compact`, and the context meter.
func (v *sessionView) footer(c *ui.Context, s *session.Session, workspace string, loaded bool) {
	a := v.a
	k, t := a.kit, a.kit.T
	ui.Row(c).Height(k.Px(28)).Gap(k.Px(12)).PaddingX(k.Px(8)).Children(func() {
		if workspace != "" {
			ui.Row(c).Gap(k.Px(6)).MinWidth(0).Tooltip(workspace).Children(func() {
				k.Icon(c, "folder", 14, t.MutedForeground)
				k.Text(c, workspaceName(workspace), 12, 16).TextColor(t.MutedForeground).SingleLine()
			})
		}
		if done, ok := a.compactions.Snapshot().Finished[v.id]; ok && done.NoticeShown(time.Now().UnixMilli()) {
			c.After(time.Second)
			ui.Row(c).Role(ui.RoleStatus).Gap(k.Px(4)).MinWidth(0).Children(func() {
				k.Icon(c, "check", 12, t.Success)
				k.Text(c, "Compacted, "+strconv.Itoa(done.RemovedCount)+" "+plural(done.RemovedCount, "message", "messages")[len(strconv.Itoa(done.RemovedCount))+1:]+" summarised", 12, 16).
					TextColor(t.Success).SingleLine()
			})
		}
		ui.Spacer(c)
		if loaded {
			v.contextMeter(c, s)
		}
	})
}

// contextMeter is the context window's fill as a ring and figures: what
// the last model call sent, over the budget of the model's breakdown.
func (v *sessionView) contextMeter(c *ui.Context, s *session.Session) {
	a := v.a
	k, t := a.kit, a.kit.T
	cs := &v.composer
	model := s.Settings().ModelID
	cs.mu.Lock()
	budget, forModel := cs.budget, cs.budgetM
	if forModel != model || (budget == nil && forModel == "") {
		cs.budgetM = model
		go func() {
			cl, err := a.ctl.Client()
			if err != nil {
				return
			}
			res, err := cl.GetContextBreakdown(a.ctx, protocol.GetContextBreakdownParams{SessionID: v.id})
			if err != nil {
				return
			}
			cs.mu.Lock()
			cs.budget = res
			cs.mu.Unlock()
			a.redraw()
		}()
	}
	cs.mu.Unlock()
	if budget == nil {
		return
	}
	used := budget.UsedTokens
	if last := s.Usage().LastCall; last != nil {
		used = last.InputTokens + last.CacheReadTokens
	}
	ratio := 0.0
	if budget.ContextBudget > 0 {
		ratio = math.Min(1, used/budget.ContextBudget)
	}
	percent := int(math.Round(ratio * 100))
	color := t.Foreground.Alpha(0.6)
	switch {
	case ratio > 0.9:
		color = t.Destructive
	case ratio > 0.7:
		color = t.Attention
	}
	label := formatTokens(used) + " / " + formatTokens(budget.ContextBudget) + " · " + strconv.Itoa(percent) + "%"
	ui.Row(c).Role(ui.RoleMeter).Label("Context used").Tooltip(groupDigits(used) + " / " + groupDigits(budget.ContextBudget) + " tokens").
		Gap(k.Px(6)).Shrink(0).Children(func() {
		ring := ui.Box(c).Size(k.Px(14), k.Px(14))
		ring.Draw(func(p *ui.Painter, r ui.Rect) {
			scale := r.W / 14
			cx, cy, rad := r.X+7*scale, r.Y+7*scale, 5.5*scale
			p.StrokePath(new(ui.Path).Circle(cx, cy, rad), 2*scale, t.Border)
			if ratio <= 0 {
				return
			}
			arc := new(ui.Path)
			steps := max(2, int(48*ratio))
			for i := 0; i <= steps; i++ {
				a := -math.Pi/2 + 2*math.Pi*ratio*float64(i)/float64(steps)
				x, y := cx+rad*float32(math.Cos(a)), cy+rad*float32(math.Sin(a))
				if i == 0 {
					arc.MoveTo(x, y)
				} else {
					arc.LineTo(x, y)
				}
			}
			p.StrokePath(arc, 2*scale, color)
		})
		k.Text(c, label, 12, 16).TextColor(t.MutedForeground).FontFeatures("tnum")
	})
}

// groupDigits is n with thousands separators, as toLocaleString in en-US.
func groupDigits(f float64) string {
	s := strconv.FormatInt(int64(math.Round(f)), 10)
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// shelf is the ComposerShelf: the task list under way and the messages
// the Daemon holds, tucked against the composer's top edge.
func (v *sessionView) shelf(c *ui.Context, s *session.Session) {
	a := v.a
	k, t := a.kit, a.kit.T
	todos := s.Todos()
	done := 0
	for _, td := range todos {
		if td.Status == session.TodoCompleted {
			done++
		}
	}
	showTodos := len(todos) > 0 && done < len(todos)
	queued := s.QueuedMessages()
	if !showTodos && len(queued) == 0 {
		return
	}
	ui.Box(c).PaddingX(k.Px(14)).Children(func() {
		ui.Column(c).Radius(k.Px(12), k.Px(12), 0, 0).BorderWidth(1, 1, 0, 1).BorderColor(t.Border).Background(t.Background).Clip().Children(func() {
			if showTodos {
				v.todoPanel(c, todos, done)
			}
			if showTodos && len(queued) > 0 {
				ui.Box(c).Height(1).Background(t.Border)
			}
			if len(queued) > 0 {
				v.queuedList(c, s, queued)
			}
		})
	})
}

func (v *sessionView) todoIcon(c *ui.Context, status session.TodoStatus) {
	k, t := v.a.kit, v.a.kit.T
	switch status {
	case session.TodoCompleted:
		k.Icon(c, "circle-check", 12, t.Success).Label("Done")
	case session.TodoInProgress:
		k.Spinner(c, 12, t.Info).Label("In progress")
	default:
		k.Icon(c, "circle", 12, t.MutedForeground.Alpha(0.5)).Label("Pending")
	}
}

func (v *sessionView) todoPanel(c *ui.Context, todos []session.TodoItem, done int) {
	k, t := v.a.kit, v.a.kit.T
	open := v.flag("todos", false)
	var current *session.TodoItem
	for _, st := range []session.TodoStatus{session.TodoInProgress, session.TodoPending} {
		for i := range todos {
			if current == nil && todos[i].Status == st {
				current = &todos[i]
			}
		}
	}
	ui.Column(c).PaddingY(k.Px(4)).Children(func() {
		p := ui.CollapsibleBase(c, open)
		tr := p.Trigger.Label("Tasks, "+strconv.Itoa(done)+" of "+strconv.Itoa(len(todos))+" done").FillWidth().Height(k.Px(30)).
			Gap(k.Px(8)).Padding(0, k.Px(6), 0, k.Px(12)).Cursor(ui.CursorPointer)
		tr.Children(func() {
			if *open {
				k.Icon(c, "list-checks", 12, t.MutedForeground)
				k.Text(c, "Tasks", 12.5, 18.75).TextColor(t.MutedForeground).Grow(1)
			} else if current != nil {
				v.todoIcon(c, current.Status)
				k.Text(c, current.Content, 12.5, 18.75).TextColor(t.Foreground).SingleLine().Grow(1).Shrink(1).MinWidth(0)
			}
			k.Text(c, strconv.Itoa(done)+"/"+strconv.Itoa(len(todos)), 11, 16.5).TextColor(t.MutedForeground).FontFeatures("tnum").Shrink(0)
			ui.Box(c).Size(k.Px(24), k.Px(24)).Center().Shrink(0).Children(func() {
				k.Icon(c, "chevron-down", 12, t.MutedForeground).Rotate(180 * p.Progress())
			})
		})
		p.Panel(func() {
			ui.Column(c).Role(ui.RoleList).Label("Tasks").Padding(0, 0, k.Px(4), 0).Children(func() {
				for _, td := range todos {
					color := t.Foreground
					if td.Status == session.TodoCompleted {
						color = t.MutedForeground
					}
					ui.Row(c).Key(td.ID).AlignItems(ui.Start).MinHeight(k.Px(26)).Gap(k.Px(8)).Padding(k.Px(3), k.Px(12)).Children(func() {
						ui.Row(c).Height(k.Px(20)).Children(func() { v.todoIcon(c, td.Status) })
						k.Text(c, td.Content, 12.5, 20).TextColor(color).Grow(1).MinWidth(0)
					})
				}
			})
		})
	})
}

func queuedText(q session.QueuedMessage) string {
	var parts []string
	for _, b := range q.Content {
		switch b.Type {
		case protocol.ContentBlockTypeText:
			var tb struct {
				Text string `json:"text"`
			}
			_ = json.Unmarshal(b.Raw, &tb)
			if tb.Text != "" {
				parts = append(parts, tb.Text)
			}
		case protocol.ContentBlockTypeImage:
			parts = append(parts, "[image]")
		}
	}
	return strings.Join(parts, " ")
}

func isPaused(q session.QueuedMessage) bool { return q.Kind == session.KindLocalPausedAfterEsc }

func isSteering(q session.QueuedMessage) bool {
	return q.Kind == session.KindDaemonQueuedDiscardable || q.Kind == session.KindLocalDeferredAfterEsc
}

// queuedList is the messages waiting for the running turn: up to two as
// a list, more folded behind the one that goes out first.
func (v *sessionView) queuedList(c *ui.Context, s *session.Session, queued []session.QueuedMessage) {
	a := v.a
	k, t := a.kit, a.kit.T
	icon := func(q session.QueuedMessage) {
		switch {
		case isPaused(q):
			k.Icon(c, "pause", 12, t.MutedForeground)
		case isSteering(q):
			k.Icon(c, "corner-down-left", 12, t.MutedForeground)
		default:
			k.Icon(c, "clock", 12, t.MutedForeground)
		}
	}
	remove := func(q session.QueuedMessage) {
		if !q.Kind.DaemonBacked() {
			s.ClearQueuedMessage(q.RequestID)
			return
		}
		go func() {
			cl, err := a.ctl.Client()
			if err == nil {
				raw, _ := json.Marshal(map[string]any{"sessionId": v.id, "requestId": q.RequestID, "action": "delete"})
				_, err = cl.ResolveQueuedUserMessage(a.ctx, raw)
			}
			if err != nil {
				v.fail(err)
			}
		}()
	}
	list := func() {
		ui.Column(c).Role(ui.RoleList).Label("Queued messages").Children(func() {
			for _, q := range queued {
				ui.Row(c).Key(q.RequestID).Height(k.Px(30)).Gap(k.Px(8)).Padding(0, k.Px(6), 0, k.Px(12)).Children(func() {
					icon(q)
					k.Text(c, queuedText(q), 12.5, 18.75).TextColor(t.Foreground).SingleLine().Grow(1).Shrink(1).MinWidth(0)
					state := "Queued"
					switch {
					case isPaused(q):
						state = "Paused"
					case isSteering(q):
						state = "Next"
					}
					k.Text(c, state, 11, 16.5).TextColor(t.MutedForeground).Shrink(0)
					if isPaused(q) && k.IconButton(c, "pencil", "Edit queued message", 24).Clicked() {
						text := queuedText(q)
						remove(q)
						cs := &v.composer
						if strings.TrimSpace(cs.text) != "" {
							text += "\n" + cs.text
						}
						cs.text = text
						v.saveDraft()
					}
					if k.IconButton(c, "x", "Remove queued message", 24).Clicked() {
						remove(q)
					}
				})
			}
		})
	}
	ui.Column(c).PaddingY(k.Px(4)).Children(func() {
		if len(queued) <= 2 {
			list()
			return
		}
		next := queued[0]
		for _, q := range queued {
			if isSteering(q) {
				next = q
				break
			}
		}
		open := v.flag("queued", false)
		p := ui.CollapsibleBase(c, open)
		p.Trigger.Label("Queued messages, "+strconv.Itoa(len(queued))).FillWidth().Height(k.Px(30)).Gap(k.Px(8)).
			Padding(0, k.Px(6), 0, k.Px(12)).Cursor(ui.CursorPointer).Children(func() {
			if *open {
				k.Icon(c, "clock", 12, t.MutedForeground)
				k.Text(c, "Queued messages", 12.5, 18.75).TextColor(t.MutedForeground).Grow(1)
			} else {
				icon(next)
				k.Text(c, queuedText(next), 12.5, 18.75).TextColor(t.Foreground).SingleLine().Grow(1).Shrink(1).MinWidth(0)
			}
			k.Text(c, strconv.Itoa(len(queued))+" queued", 11, 16.5).TextColor(t.MutedForeground).FontFeatures("tnum").Shrink(0)
			ui.Box(c).Size(k.Px(24), k.Px(24)).Center().Shrink(0).Children(func() {
				k.Icon(c, "chevron-down", 12, t.MutedForeground).Rotate(180 * p.Progress())
			})
		})
		p.Panel(list)
	})
}
