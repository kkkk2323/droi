package app

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/internal/automations"
	"github.com/kkkk2323/droi/apps/native/internal/kit"
	"github.com/kkkk2323/droi/apps/native/internal/l10n"
	"github.com/kkkk2323/droi/apps/native/internal/models"
)

// The Daemon reports no change to its Automations; the page reads them
// again now and then, and the runs of the one shown more often.
const (
	automationsRefresh = 30 * time.Second
	runsRefresh        = 15 * time.Second
	runningRefresh     = 3 * time.Second
	factoryAppRefresh  = 30 * time.Second
)

// The Route.Tab of the page that creates one, and of the one that edits
// Route.Automation.
const (
	newAutomationTab  = "new"
	editAutomationTab = "edit"
)

// noticeFor is how long the page says an action went well.
const noticeFor = 4 * time.Second

// rowMotion slides the rows of the page's lists to their places as one
// comes or goes, and fades a new one in from just above.
var rowMotion = ui.ElementTransition{Duration: 180 * time.Millisecond, Position: true, Colors: true, Enter: &ui.Motion{Y: -4}}

// automationsState is the Automations page's, kept between frames.
type automationsState struct {
	list    []protocol.AutomationEntry
	listErr string
	listAt  time.Time
	loading bool

	// workdirs are the Workspaces of the Automations, read from their
	// folders with the list.
	workdirs map[string]string

	runs     map[string][]protocol.AutomationRunRecord
	runsErr  map[string]string
	runsAt   map[string]time.Time
	runsBusy map[string]bool

	form automationForm
	// formFor is the Automation the form was filled from, newAutomationTab
	// for a new one.
	formFor string
	// acting is the action under way, on the Automation actingOn; notice
	// is what the last one said, an error when noticeErr.
	acting, actingOn, notice string
	noticeErr                bool
	noticeAt                 time.Time
	menuOpen, confirmDelete  bool

	factoryApp       bool
	factoryCheckedAt time.Time
	factoryChecking  bool
}

// automationForm is what the editor's fields hold.
type automationForm struct {
	name, prompt, clock, minute, custom, workdir string
	repeat                                       automations.Repeat
	weekday                                      time.Weekday
	// model is "" for the Daemon's default.
	model     string
	modelOpen bool
	picker    pickerState
	// err is what is wrong with the field errOn.
	err, errOn string
}

func (a *App) offset() int {
	_, off := a.cfg.Now().Zone()
	return off
}

// enterAutomations starts a page of Automations afresh.
func (a *App) enterAutomations(r Route) {
	s := &a.automations
	s.confirmDelete, s.menuOpen, s.notice = false, false, ""
	if r.Tab != "" {
		s.formFor = ""
	}
}

func (a *App) loadAutomations(force bool) {
	s := &a.automations
	if s.loading || a.ctl == nil || !force && !s.listAt.IsZero() && a.cfg.Now().Sub(s.listAt) < automationsRefresh {
		return
	}
	s.loading = true
	go func() {
		var res *protocol.ListAutomationsResult
		cl, err := a.ctl.Client()
		if err == nil {
			res, err = cl.ListAutomations(a.ctx, protocol.ListAutomationsParams{})
		}
		workdirs := map[string]string{}
		if err == nil {
			for _, e := range res.Automations {
				workdirs[e.ID] = automations.WorkingDirectory(e.Path)
			}
		}
		a.cfg.Update(func() {
			s.loading = false
			s.listAt = a.cfg.Now()
			if err != nil {
				s.listErr = L("Automations did not load: %s", err)
				// Soon again: the Daemon may only be starting.
				s.listAt = s.listAt.Add(5*time.Second - automationsRefresh)
				return
			}
			s.listErr, s.list, s.workdirs = "", res.Automations, workdirs
		})
	}()
}

func (a *App) loadRuns(id string, force bool) {
	s := &a.automations
	if s.runs == nil {
		s.runs, s.runsErr, s.runsAt, s.runsBusy = map[string][]protocol.AutomationRunRecord{}, map[string]string{}, map[string]time.Time{}, map[string]bool{}
	}
	every := runsRefresh
	for _, r := range s.runs[id] {
		if r.Status == "in_progress" {
			every = runningRefresh
		}
	}
	if s.runsBusy[id] || a.ctl == nil || !force && !s.runsAt[id].IsZero() && a.cfg.Now().Sub(s.runsAt[id]) < every {
		return
	}
	s.runsBusy[id] = true
	go func() {
		var res *protocol.GetAutomationHistoryResult
		limit := float64(20)
		cl, err := a.ctl.Client()
		if err == nil {
			res, err = cl.GetAutomationHistory(a.ctx, protocol.GetAutomationHistoryParams{AutomationID: id, Limit: &limit})
		}
		a.cfg.Update(func() {
			s.runsBusy[id] = false
			s.runsAt[id] = a.cfg.Now()
			if err != nil {
				s.runsErr[id] = err.Error()
				return
			}
			s.runsErr[id], s.runs[id] = "", res.Runs
		})
	}()
}

// checkFactoryApp looks now and then for the Factory App's Daemon, off the frame.
func (a *App) checkFactoryApp() {
	s := &a.automations
	if a.cfg.FactoryAppRunning == nil || s.factoryChecking || !s.factoryCheckedAt.IsZero() && a.cfg.Now().Sub(s.factoryCheckedAt) < factoryAppRefresh {
		return
	}
	s.factoryChecking = true
	go func() {
		running := a.cfg.FactoryAppRunning()
		a.cfg.Update(func() {
			s.factoryChecking, s.factoryCheckedAt, s.factoryApp = false, a.cfg.Now(), running
		})
	}()
}

// refused is a change the Daemon answered with success false.
func refused(ok bool, msg string, err error) error {
	if err != nil || ok {
		return err
	}
	if msg == "" {
		msg = L("the Daemon refused the change")
	}
	return errors.New(msg)
}

// dispatchRun starts a run now: daemon.dispatch_automation_run, newer
// than the SDK's schema. Its error is an object, not a string.
func (a *App) dispatchRun(id string) (string, error) {
	cl, err := a.ctl.Client()
	if err != nil {
		return "", err
	}
	var out struct {
		Success   bool            `json:"success"`
		SessionID string          `json:"sessionId"`
		Error     json.RawMessage `json:"error"`
	}
	if err := cl.Call(a.ctx, "daemon.dispatch_automation_run", map[string]any{"automationId": id}, &out); err != nil {
		return "", err
	}
	if out.Success {
		return out.SessionID, nil
	}
	var msg string
	if json.Unmarshal(out.Error, &msg) != nil {
		var e struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(out.Error, &e)
		msg = e.Message
	}
	return "", refused(false, msg, nil)
}

// act runs one action on an Automation off the frame, then reads the list
// and the runs again and says how it went.
func (a *App) act(id, action, done string, fn func() error, then func()) {
	s := &a.automations
	if s.acting != "" {
		return
	}
	s.acting, s.actingOn, s.notice = action, id, ""
	go func() {
		err := fn()
		a.cfg.Update(func() {
			s.acting, s.actingOn = "", ""
			if err == nil && then != nil {
				// then may go to another page, which drops the notice.
				then()
			}
			s.noticeAt, s.noticeErr, s.notice = a.cfg.Now(), err != nil, done
			if err != nil {
				s.notice = L("Could not %s: %s", l10n.T(action), err)
			}
			a.loadAutomations(true)
			if id != "" {
				a.loadRuns(id, true)
			}
		})
	}()
}

// automationsPage lists the Automations, shows one, or edits one.
func (a *App) automationsPage(c *ui.Context) {
	k := a.kit
	s := &a.automations
	a.loadAutomations(false)
	a.checkFactoryApp()
	ui.Column(c).Role(ui.RoleGroup).Label(L("Automations")).Fill().Children(func() {
		ui.Row(c).Height(k.Px(44)).Shrink(0).PaddingX(k.Px(8)).AlignItems(ui.Center).DragWindow().Children(func() {
			if a.narrow {
				a.openSessionsButton(c)
			}
		})
		ui.Scroll(c).Grow(1).MinWidth(0).Children(func() {
			ui.Column(c).FillWidth().MaxWidth(k.Px(720)).AlignSelf(ui.Center).Gap(k.Px(24)).Padding(k.Px(4), k.Px(32), k.Px(48), k.Px(32)).Children(func() {
				id := a.route.Automation
				switch {
				case a.route.Tab == newAutomationTab:
					a.automationEditor(c, nil)
				case id == "":
					a.automationList(c)
				case a.findAutomation(id) == nil:
					a.missingAutomation(c)
				case a.route.Tab == editAutomationTab:
					a.automationEditor(c, a.findAutomation(id))
				default:
					a.automationView(c, a.findAutomation(id))
				}
			})
		})
		if e := a.findAutomation(a.route.Automation); e != nil && s.confirmDelete {
			a.deleteDialog(c, e)
		}
	})
}

// crumb is a step of the page's breadcrumb; the last is the page itself.
type crumb struct {
	label, name string
	to          Route
}

// automationHeader is the page's breadcrumb, then its title with what
// goes beside it and its actions on the right.
func (a *App) automationHeader(c *ui.Context, crumbs []crumb, title string, beside, actions func()) {
	k, t := a.kit, a.kit.T
	ui.Column(c).Gap(k.Px(6)).Children(func() {
		if len(crumbs) > 0 {
			ui.Row(c).Role(ui.RoleGroup).Label(L("Breadcrumb")).Gap(k.Px(2)).AlignItems(ui.Center).Margin(0, 0, 0, -k.Px(6)).Children(func() {
				for i, cr := range crumbs {
					if i > 0 {
						k.Icon(c, "chevron-right", 12, t.MutedForeground.Alpha(0.6))
					}
					if i == len(crumbs)-1 {
						k.Text(c, cr.label, 13, 20).TextColor(t.MutedForeground).SingleLine().Shrink(1).MinWidth(0).PaddingX(k.Px(6))
						continue
					}
					b := ui.ButtonBase(c).Label(cr.name).Height(k.Px(24)).PaddingX(k.Px(6)).Radius(k.Px(6)).Shrink(1).MinWidth(0).Cursor(ui.CursorPointer)
					color := t.MutedForeground
					if b.Hovered() {
						b.Background(t.Muted)
						color = t.Foreground
					}
					b.Children(func() { k.Text(c, cr.label, 13, 20).TextColor(color).SingleLine() })
					if b.Clicked() {
						a.Go(cr.to)
					}
				}
			})
		}
		ui.Row(c).Gap(k.Px(10)).AlignItems(ui.Center).MinHeight(k.Px(32)).Children(func() {
			k.Text(c, title, 20, 28).Role(ui.RoleHeading).FontWeight(600).LetterSpacing(-k.Px(0.5)).SingleLine().Shrink(1).MinWidth(0)
			if beside != nil {
				beside()
			}
			ui.Spacer(c)
			if actions != nil {
				ui.Row(c).Gap(k.Px(8)).AlignItems(ui.Center).Shrink(0).Children(actions)
			}
		})
	})
}

func allAutomations() crumb {
	return crumb{label: L("Automations"), name: L("All automations"), to: Route{Name: "automations"}}
}

// factoryAppNotice says the Factory App runs the same Automations.
func (a *App) factoryAppNotice(c *ui.Context) {
	if !a.automations.factoryApp {
		return
	}
	k, t := a.kit, a.kit.T
	ui.Row(c).Role(ui.RoleStatus).Label(L("Factory App is running")).AlignItems(ui.Start).Gap(k.Px(10)).Padding(k.Px(8), k.Px(12)).
		Radius(k.Px(8)).Background(t.Attention.Alpha(0.08)).Children(func() {
		k.Icon(c, "triangle-alert", 14, t.AttentionForeground).Margin(k.Px(3), 0, 0, 0)
		k.Text(c, L("The Factory App is running. Its own Daemon runs these automations too, so a run can start twice. Quit the Factory App to avoid that."), 13, 20).
			TextColor(t.Foreground.Alpha(0.85)).Grow(1).MinWidth(0)
	})
}

// automationNotice is what the last action said: an error until the next
// action, a success for a few seconds.
func (a *App) automationNotice(c *ui.Context) {
	k, t := a.kit, a.kit.T
	s := &a.automations
	if s.notice == "" {
		return
	}
	icon, color := "circle-check", t.Success
	if s.noticeErr {
		icon, color = "circle-alert", t.DestructiveForeground
	} else {
		left := noticeFor - a.cfg.Now().Sub(s.noticeAt)
		if left <= 0 {
			return
		}
		c.After(left)
	}
	ui.Row(c).Key("notice").Role(ui.RoleStatus).Label(s.notice).Gap(k.Px(8)).AlignItems(ui.Start).
		Transition(ui.ElementTransition{Duration: 150 * time.Millisecond, Enter: &ui.Motion{Y: -4}, Exit: &ui.Motion{}}).Children(func() {
		k.Icon(c, icon, 14, color).Margin(k.Px(3), 0, 0, 0)
		k.Text(c, s.notice, 13, 20).TextColor(t.Foreground).Grow(1).MinWidth(0)
	})
}

func (a *App) automationList(c *ui.Context) {
	k, t := a.kit, a.kit.T
	s := &a.automations
	count := ""
	if len(s.list) > 0 {
		count = strconv.Itoa(len(s.list))
	}
	ui.Column(c).Gap(k.Px(4)).Children(func() {
		a.automationHeader(c, nil, L("Automations"), func() {
			if count != "" {
				k.Text(c, count, 13, 20).TextColor(t.MutedForeground).FontFeatures("tnum")
			}
		}, func() {
			if len(s.list) > 0 && a.smallButton(c, kit.Primary, L("New automation"), "plus", false).Clicked() {
				a.Go(Route{Name: "automations", Tab: newAutomationTab})
			}
		})
		k.Text(c, L("Prompts Droid runs on a schedule, on this computer, while Droi is open."), 13, 20).TextColor(t.MutedForeground)
	})
	a.factoryAppNotice(c)
	a.automationNotice(c)
	switch {
	case s.list == nil && s.listErr != "":
		a.loadFailed(c, s.listErr, func() { a.loadAutomations(true) })
	case s.list == nil:
		a.skeletonRows(c, L("Loading automations"), 3, 56)
	case len(s.list) == 0:
		a.noAutomations(c)
	default:
		if s.listErr != "" {
			k.Text(c, s.listErr, 13, 20).Role(ui.RoleStatus).TextColor(t.DestructiveForeground)
		}
		ui.Column(c).Role(ui.RoleList).Label(L("Automations")).Children(func() {
			a.listHeading(c, L("Automation"), L("Next run"))
			for _, e := range s.list {
				a.automationRow(c, e)
			}
		})
	}
}

// listHeading names the columns of a list: the item, and the time on its
// right.
func (a *App) listHeading(c *ui.Context, left, right string) {
	k, t := a.kit, a.kit.T
	ui.Row(c).Height(k.Px(28)).PaddingX(k.Px(12)).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
		k.Text(c, left, 12, 16).FontWeight(500).TextColor(t.MutedForeground).Grow(1)
		k.Text(c, right, 12, 16).FontWeight(500).TextColor(t.MutedForeground).Margin(0, k.Px(36), 0, 0)
	})
}

// skeletonRows stand in for the rows of a list until it loads, pulsing
// softly unless the desktop asks for less motion.
func (a *App) skeletonRows(c *ui.Context, label string, n int, height float32) {
	k, t := a.kit, a.kit.T
	col := ui.Column(c).Role(ui.RoleStatus).Label(label)
	bar := t.Muted
	if !c.Preferences().ReduceMotion {
		bar = t.Muted.Alpha(0.6 + 0.4*col.Loop("pulse", 1600*time.Millisecond, ui.Bounce(ui.EaseInOut)))
	}
	col.Children(func() {
		for i := range n {
			ui.Row(c).Height(k.Px(height)).PaddingX(k.Px(12)).Gap(k.Px(12)).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(t.Border.Alpha(0.6)).Children(func() {
				kit.Dot(c, k.Px(8), bar)
				ui.Column(c).Grow(1).Gap(k.Px(6)).Children(func() {
					ui.Box(c).Width(k.Px(float32(180 - i*28))).Height(k.Px(10)).Radius(k.Px(4)).Background(bar)
					if height > 44 {
						ui.Box(c).Width(k.Px(float32(260 - i*36))).Height(k.Px(8)).Radius(k.Px(4)).Background(bar.Alpha(0.6))
					}
				})
				ui.Box(c).Width(k.Px(72)).Height(k.Px(8)).Radius(k.Px(4)).Background(bar).Margin(0, k.Px(36), 0, 0)
			})
		}
	})
}

// loadFailed says a list did not load, with a way to try again.
func (a *App) loadFailed(c *ui.Context, msg string, retry func()) {
	k, t := a.kit, a.kit.T
	ui.Row(c).Role(ui.RoleStatus).Label(msg).Gap(k.Px(10)).AlignItems(ui.Center).Padding(k.Px(10), k.Px(12)).Radius(k.Px(8)).
		Background(t.Destructive.Alpha(0.06)).Children(func() {
		k.Icon(c, "circle-alert", 14, t.DestructiveForeground)
		k.Text(c, msg, 13, 20).TextColor(t.Foreground).Grow(1).MinWidth(0)
		if a.smallButton(c, kit.Outline, L("Try again"), "refresh-cw", false).Clicked() {
			retry()
		}
	})
}

// noAutomations is the list before the first Automation: what one is for,
// and the way to make one.
func (a *App) noAutomations(c *ui.Context) {
	k, t := a.kit, a.kit.T
	ui.Column(c).Role(ui.RoleGroup).Label(L("No automations yet")).AlignItems(ui.Center).Gap(k.Px(6)).Padding(k.Px(40), k.Px(24)).
		Radius(k.Px(12)).Border(1, t.Border).Children(func() {
		ui.Box(c).Size(k.Px(36), k.Px(36)).Radius(k.Px(10)).Background(t.Muted).Center().Margin(0, 0, k.Px(6), 0).Children(func() {
			k.Icon(c, "calendar-clock", 18, t.MutedForeground)
		})
		k.Text(c, L("No automations yet"), 14, 20).FontWeight(500)
		k.Text(c, L("Run a prompt on a schedule, for example a summary of yesterday’s commits every morning."), 13, 20).
			TextColor(t.MutedForeground).MaxWidth(k.Px(360)).TextAlign(ui.Center)
		if a.smallButton(c, kit.Primary, L("New automation"), "plus", false).Margin(k.Px(10), 0, 0, 0).Clicked() {
			a.Go(Route{Name: "automations", Tab: newAutomationTab})
		}
	})
}

// summary is an Automation's schedule and its last run, in words.
func (a *App) summary(e protocol.AutomationEntry) string {
	parts := []string{automations.Describe(e.Schedule, a.offset())}
	if w := automations.When(e.LastRunAt, a.cfg.Now()); w != "" {
		last := L("last run %s", w)
		if e.LastRunStatus != "" {
			last += ", " + strings.ToLower(l10n.T(automations.RunLabel(e.LastRunStatus)))
		}
		parts = append(parts, last)
	}
	return strings.Join(parts, " · ")
}

// nextRun is when an Automation runs next, or why it does not.
func (a *App) nextRun(e protocol.AutomationEntry) string {
	if e.Status != "active" {
		return automations.StatusLabel(e.Status)
	}
	if w := automations.When(e.NextRunAt, a.cfg.Now()); w != "" {
		return w
	}
	return L("Not scheduled")
}

// automationMark is an Automation's state at a glance: running, active,
// paused, or with a failed last run.
func (a *App) automationMark(c *ui.Context, e protocol.AutomationEntry) {
	k, t := a.kit, a.kit.T
	ui.Box(c).Size(k.Px(16), k.Px(16)).Center().Shrink(0).Children(func() {
		switch {
		case automations.RunLabel(e.LastRunStatus) == "Running":
			k.Spinner(c, 14, t.Info)
		case e.Status == "paused":
			k.Icon(c, "circle-pause", 14, t.MutedForeground)
		case e.Status != "active":
			k.Icon(c, "circle-alert", 14, t.DestructiveForeground)
		case automations.RunLabel(e.LastRunStatus) == "Failed":
			kit.Dot(c, k.Px(8), t.Destructive)
		default:
			kit.Dot(c, k.Px(8), t.Success)
		}
	})
}

// runNow starts a run of the Automation id at once.
func (a *App) runNow(id string) {
	a.act(id, l10n.N("run"), L("Started a run."), func() error { _, err := a.dispatchRun(id); return err }, nil)
}

// automationRow opens the Automation, and shows a Run now button while the
// pointer is over it.
func (a *App) automationRow(c *ui.Context, e protocol.AutomationEntry) {
	k, t := a.kit, a.kit.T
	s := &a.automations
	name := trimOr(e.Name, e.ID)
	row := ui.Row(c).Key(e.ID).Transition(rowMotion).Height(k.Px(56)).PaddingX(k.Px(12)).Gap(k.Px(8)).AlignItems(ui.Center).
		BorderWidth(0, 0, 1, 0).BorderColor(t.Border.Alpha(0.6))
	hover := row.Hovered()
	if hover {
		row.Background(t.Muted.Alpha(0.5))
	}
	row.Children(func() {
		open := ui.ButtonBase(c).Role(ui.RoleListItem).Label(name).Grow(1).MinWidth(0).AlignSelf(ui.Stretch).Gap(k.Px(12)).Justify(ui.Start).Cursor(ui.CursorPointer)
		open.Children(func() {
			a.automationMark(c, e)
			ui.Column(c).Grow(1).MinWidth(0).Gap(k.Px(2)).Children(func() {
				k.Text(c, name, 14, 20).FontWeight(500).SingleLine()
				k.Text(c, a.summary(e), 12, 16).TextColor(t.MutedForeground).SingleLine()
			})
			k.Text(c, a.nextRun(e), 12, 16).TextColor(t.MutedForeground).FontFeatures("tnum").SingleLine().Shrink(0)
		})
		if open.Clicked() {
			a.Go(Route{Name: "automations", Automation: e.ID})
		}
		ui.Box(c).Size(k.Px(28), k.Px(28)).Center().Shrink(0).Children(func() {
			if s.acting == "run" && s.actingOn == e.ID {
				k.Spinner(c, 14, t.MutedForeground)
				return
			}
			run := k.IconButton(c, "play", L("Run %s now", name), 28).Disabled(s.acting != "")
			if !hover && !run.Focused() {
				run.Opacity(0)
			}
			if run.Clicked() {
				a.runNow(e.ID)
			}
		})
	})
}

func (a *App) findAutomation(id string) *protocol.AutomationEntry {
	for i := range a.automations.list {
		if a.automations.list[i].ID == id {
			return &a.automations.list[i]
		}
	}
	return nil
}

// missingAutomation is the page of an Automation the list does not have:
// not loaded yet, or gone.
func (a *App) missingAutomation(c *ui.Context) {
	k, t := a.kit, a.kit.T
	s := &a.automations
	if s.list == nil && s.listErr == "" {
		a.automationHeader(c, []crumb{allAutomations(), {label: "…"}}, "", nil, nil)
		a.skeletonRows(c, L("Loading automation"), 2, 40)
		return
	}
	a.automationHeader(c, []crumb{allAutomations(), {label: L("Not found")}}, L("Automation not found"), nil, nil)
	k.Text(c, L("It may have been deleted, here or in the Factory App."), 13, 20).TextColor(t.MutedForeground).Margin(-k.Px(16), 0, 0, 0)
}

// fact is a line of an Automation's facts: a label and its value.
func (a *App) fact(c *ui.Context, label string, value func()) {
	k, t := a.kit, a.kit.T
	ui.Row(c).Gap(k.Px(16)).AlignItems(ui.Start).MinHeight(k.Px(20)).Children(func() {
		k.Text(c, label, 13, 20).TextColor(t.MutedForeground).Width(k.Px(96)).Shrink(0).SingleLine()
		ui.Row(c).Grow(1).MinWidth(0).Gap(k.Px(8)).AlignItems(ui.Center).Children(value)
	})
}

// sectionTitle heads a part of a page, with a count beside it.
func (a *App) sectionTitle(c *ui.Context, title, count string) {
	k, t := a.kit, a.kit.T
	ui.Row(c).Gap(k.Px(6)).AlignItems(ui.Center).Children(func() {
		k.Text(c, title, 13, 20).Role(ui.RoleHeading).FontWeight(600)
		if count != "" {
			k.Text(c, count, 13, 20).TextColor(t.MutedForeground).FontFeatures("tnum")
		}
	})
}

// modelName is the name of the model an Automation's runs use.
func (a *App) modelName(id string) string {
	if id == "" {
		return L("Your default model")
	}
	if dv := a.sessionDefaults(); dv != nil {
		for _, m := range dv.Models {
			if m.ID == id {
				return m.Label
			}
		}
	}
	return id
}

func (a *App) automationView(c *ui.Context, e *protocol.AutomationEntry) {
	k, t := a.kit, a.kit.T
	s := &a.automations
	id, name := e.ID, trimOr(e.Name, e.ID)
	a.loadRuns(id, false)
	busy := s.acting != ""
	a.automationHeader(c, []crumb{allAutomations(), {label: name}}, name, func() {
		a.statusPill(c, e.Status == "active", automations.StatusLabel(e.Status))
	}, func() {
		runLabel := L("Run now")
		if s.acting == "run" {
			runLabel = L("Starting…")
		}
		if a.smallButton(c, kit.Primary, runLabel, "play", busy).Label(L("Run now")).Clicked() {
			a.runNow(id)
		}
		if a.smallButton(c, kit.Outline, L("Edit"), "pencil", busy).Label(L("Edit automation")).Clicked() {
			a.Go(Route{Name: "automations", Automation: id, Tab: editAutomationTab})
		}
		more := k.IconButton(c, "ellipsis", L("More actions"), 32).Expanded(s.menuOpen).Disabled(busy)
		if more.Clicked() {
			s.menuOpen = !s.menuOpen
		}
		k.MenuPopup(c, more, &s.menuOpen, true, L("More actions"), func() {
			if e.Status == "paused" {
				if k.MenuItem(c, &s.menuOpen, "play", L("Resume")).Clicked() {
					a.setPaused(id, false)
				}
			} else if k.MenuItem(c, &s.menuOpen, "pause", L("Pause")).Clicked() {
				a.setPaused(id, true)
			}
			if k.MenuItem(c, &s.menuOpen, "trash", L("Delete…")).Clicked() {
				s.confirmDelete = true
			}
		})
	})
	a.factoryAppNotice(c)
	a.automationNotice(c)
	ui.Column(c).Role(ui.RoleGroup).Label(L("Details")).Gap(k.Px(8)).Children(func() {
		off := a.offset()
		a.fact(c, L("Schedule"), func() {
			k.Text(c, automations.Describe(e.Schedule, off), 13, 20).SingleLine().Shrink(0)
			k.Text(c, e.Schedule+" UTC", 12, 20).Font(k.Mono).TextColor(t.MutedForeground).SingleLine().Shrink(1).MinWidth(0)
		})
		a.fact(c, L("Next run"), func() { k.Text(c, a.nextRun(*e), 13, 20).FontFeatures("tnum").SingleLine() })
		if w := automations.When(e.LastRunAt, a.cfg.Now()); w != "" {
			a.fact(c, L("Last run"), func() {
				k.Text(c, w, 13, 20).FontFeatures("tnum").SingleLine()
				if e.LastRunStatus != "" {
					k.Text(c, l10n.T(automations.RunLabel(e.LastRunStatus)), 13, 20).TextColor(t.MutedForeground).SingleLine()
				}
			})
		}
		a.fact(c, L("Model"), func() { k.Text(c, a.modelName(e.Model), 13, 20).SingleLine() })
		a.fact(c, L("Workspace"), func() {
			if w := s.workdirs[id]; w != "" {
				k.Text(c, w, 12, 20).Font(k.Mono).SingleLine().Shrink(1).MinWidth(0)
			} else {
				k.Text(c, L("Its own folder in ~/.factory/automations"), 13, 20).TextColor(t.MutedForeground).SingleLine().Shrink(1).MinWidth(0)
			}
		})
	})
	ui.Column(c).Gap(k.Px(8)).Children(func() {
		a.sectionTitle(c, L("Prompt"), "")
		ui.Column(c).Padding(k.Px(10), k.Px(12)).Radius(k.Px(8)).Background(t.Muted.Alpha(0.5)).Children(func() {
			k.Text(c, e.Prompt, 13, 20).TextColor(t.Foreground)
		})
	})
	a.runsList(c, id)
}

// setPaused pauses the Automation id, or resumes it.
func (a *App) setPaused(id string, pause bool) {
	action, done := l10n.N("resume"), L("Resumed. It runs on its schedule again.")
	if pause {
		action, done = l10n.N("pause"), L("Paused. Run now still starts a run.")
	}
	a.act(id, action, done, func() error {
		cl, err := a.ctl.Client()
		if err != nil {
			return err
		}
		if pause {
			res, err := cl.PauseAutomation(a.ctx, protocol.PauseAutomationParams{AutomationID: id})
			if err != nil {
				return err
			}
			return refused(res.Success, res.Error, nil)
		}
		res, err := cl.ResumeAutomation(a.ctx, protocol.ResumeAutomationParams{AutomationID: id})
		if err != nil {
			return err
		}
		return refused(res.Success, res.Error, nil)
	}, nil)
}

// deleteDialog asks before the Automation e goes.
func (a *App) deleteDialog(c *ui.Context, e *protocol.AutomationEntry) {
	k, t := a.kit, a.kit.T
	s := &a.automations
	w, _ := c.Size()
	id, name := e.ID, trimOr(e.Name, e.ID)
	ui.DialogBase(c, &s.confirmDelete, func(backdrop, panel *ui.Element) {
		backdrop.Background(ui.RGBA(0, 0, 0, 0.3))
		panel.Role(ui.RoleAlertDialog).Label(L("Delete automation")).Width(min(k.Px(420), w-k.Px(32))).Padding(k.Px(20)).Radius(k.Px(12)).Border(1, t.Border).
			Background(t.Popover).TextColor(t.PopoverForeground).Shadow(0, k.Px(20), k.Px(25), -k.Px(5), ui.RGBA(0, 0, 0, 0.1))
		ui.Column(c).FillWidth().Gap(k.Px(6)).Children(func() {
			k.Text(c, L("Delete “%s”?", name), 15, 22).Role(ui.RoleHeading).FontWeight(600)
			k.Text(c, L("It stops running and its folder in ~/.factory/automations is removed. The sessions of its past runs stay."), 13, 20).
				TextColor(t.MutedForeground)
			ui.Row(c).Gap(k.Px(8)).Justify(ui.End).Margin(k.Px(14), 0, 0, 0).Children(func() {
				if a.smallButton(c, kit.Outline, L("Cancel"), "", false).Clicked() {
					s.confirmDelete = false
				}
				if a.smallButton(c, kit.Destructive, L("Delete"), "", s.acting != "").Clicked() {
					s.confirmDelete = false
					a.act("", l10n.N("delete"), L("Deleted “%s”.", name), func() error {
						cl, err := a.ctl.Client()
						if err != nil {
							return err
						}
						res, err := cl.DeleteAutomation(a.ctx, protocol.DeleteAutomationParams{AutomationID: id})
						if err != nil {
							return err
						}
						return refused(res.Success, res.Error, nil)
					}, func() { a.Go(Route{Name: "automations"}) })
				}
			})
		})
	})
}

func (a *App) runsList(c *ui.Context, id string) {
	k, t := a.kit, a.kit.T
	s := &a.automations
	runs, loaded := s.runs[id]
	count := ""
	if len(runs) > 0 {
		count = strconv.Itoa(len(runs))
	}
	ui.Column(c).Gap(k.Px(4)).Children(func() {
		a.sectionTitle(c, L("Runs"), count)
		switch {
		case s.runsErr[id] != "" && !loaded:
			a.loadFailed(c, L("Runs did not load: %s", s.runsErr[id]), func() { a.loadRuns(id, true) })
		case !loaded:
			a.skeletonRows(c, L("Loading runs"), 2, 40)
		case len(runs) == 0:
			k.Text(c, L("No runs yet. Run it now, or wait for its schedule."), 13, 20).TextColor(t.MutedForeground).Padding(k.Px(8), 0)
		default:
			ui.Column(c).Role(ui.RoleList).Label(L("Runs")).Children(func() {
				for _, r := range runs {
					a.runRow(c, r)
				}
			})
		}
	})
}

func (a *App) runRow(c *ui.Context, r protocol.AutomationRunRecord) {
	k, t := a.kit, a.kit.T
	started := automations.When(r.StartedAt, a.cfg.Now())
	label := l10n.T(automations.RunLabel(r.Status))
	if r.Type == protocol.AutomationRunType("create") {
		label = L("Setup, %s", strings.ToLower(label))
	}
	b := ui.ButtonBase(c).Key(r.RunID).Transition(rowMotion).Role(ui.RoleListItem).Label(L("Open run from %s", started)).Height(k.Px(40)).Gap(k.Px(10)).
		PaddingX(k.Px(12)).Justify(ui.Start).BorderWidth(0, 0, 1, 0).BorderColor(t.Border.Alpha(0.6)).Cursor(ui.CursorPointer)
	if r.SessionID == "" {
		b.Disabled(true)
	}
	hover := b.Hovered()
	if hover {
		b.Background(t.Muted.Alpha(0.5))
	}
	b.Children(func() {
		ui.Box(c).Size(k.Px(16), k.Px(16)).Center().Shrink(0).Children(func() {
			switch automations.RunLabel(r.Status) {
			case "Running":
				k.Spinner(c, 14, t.Info)
			case "Succeeded":
				k.Icon(c, "circle-check", 14, t.Success)
			case "Failed":
				k.Icon(c, "circle-x", 14, t.DestructiveForeground)
			default:
				k.Icon(c, "circle-dashed", 14, t.MutedForeground)
			}
		})
		k.Text(c, label, 13, 20).SingleLine().Shrink(0)
		if r.ErrorMessage != "" {
			k.Text(c, r.ErrorMessage, 13, 20).TextColor(t.MutedForeground).SingleLine().Shrink(1).MinWidth(0)
		}
		ui.Spacer(c)
		k.Text(c, started, 12, 16).TextColor(t.MutedForeground).FontFeatures("tnum").Shrink(0)
		chevron := k.Icon(c, "chevron-right", 14, t.MutedForeground)
		if !hover {
			chevron.Opacity(0)
		}
	})
	if b.Clicked() && r.SessionID != "" {
		a.Go(Route{Name: "session", SessionID: r.SessionID})
	}
}

// fillForm sets the form from an Automation, or to a new one's defaults.
func (a *App) fillForm(e *protocol.AutomationEntry) {
	f := automationForm{repeat: automations.Daily, clock: "09:00", minute: "00"}
	if e != nil {
		sc := automations.Parse(e.Schedule, a.offset())
		f.name, f.prompt, f.model = e.Name, e.Prompt, e.Model
		f.repeat, f.weekday, f.custom = sc.Repeat, sc.Weekday, sc.Custom
		f.clock, f.minute = automations.Clock(sc.Hour, sc.Minute), strconv.Itoa(sc.Minute)
		if len(f.minute) < 2 {
			f.minute = "0" + f.minute
		}
		f.workdir = a.automations.workdirs[e.ID]
	}
	a.automations.form = f
}

// schedule is the form's schedule as the Daemon takes it.
func (f *automationForm) schedule(offset int) (string, error) {
	s := automations.Schedule{Repeat: f.repeat, Weekday: f.weekday, Custom: f.custom}
	switch f.repeat {
	case automations.Hourly:
		m, err := strconv.Atoi(strings.TrimSpace(f.minute))
		if err != nil || m < 0 || m > 59 {
			return "", errors.New(L("Enter the minute past the hour, from 0 to 59."))
		}
		s.Minute = m
	case automations.Daily, automations.Weekdays, automations.Weekly:
		h, m, ok := automations.ParseClock(f.clock)
		if !ok {
			return "", errors.New(L("Enter the time as hours and minutes, for example 09:30."))
		}
		s.Hour, s.Minute = h, m
	default:
		if strings.TrimSpace(f.custom) == "" {
			return "", errors.New(L("Enter a cron expression, in UTC."))
		}
	}
	return s.Expression(offset), nil
}

// formField is a field of the editor: its label above it, and under it
// what is wrong with it, or else help.
func (a *App) formField(c *ui.Context, label, help, problem string, body func()) {
	k, t := a.kit, a.kit.T
	ui.Column(c).Gap(k.Px(6)).Children(func() {
		k.Text(c, label, 13, 20).FontWeight(500)
		body()
		switch {
		case problem != "":
			ui.Row(c).Role(ui.RoleStatus).Label(problem).Gap(k.Px(6)).AlignItems(ui.Center).Children(func() {
				k.Icon(c, "circle-alert", 12, t.DestructiveForeground)
				k.Text(c, problem, 12, 18).TextColor(t.DestructiveForeground).Grow(1).MinWidth(0)
			})
		case help != "":
			k.Text(c, help, 12, 18).TextColor(t.MutedForeground)
		}
	})
}

// automationEditor is the form of a new Automation (e nil) or of e.
func (a *App) automationEditor(c *ui.Context, e *protocol.AutomationEntry) {
	k, t := a.kit, a.kit.T
	s := &a.automations
	key := newAutomationTab
	if e != nil {
		key = e.ID
	}
	if s.formFor != key {
		a.fillForm(e)
		s.formFor = key
	}
	f := &s.form
	off := a.offset()
	var choices []models.Choice
	shownModel := f.model
	if dv := a.sessionDefaults(); dv != nil {
		choices = dv.Models
		if shownModel == "" {
			shownModel = dv.ModelID
		}
	}
	problem := func(field string) string {
		if f.errOn == field {
			return f.err
		}
		return ""
	}
	crumbs, title := []crumb{allAutomations(), {label: L("New automation")}}, L("New automation")
	back := Route{Name: "automations"}
	if e != nil {
		back = Route{Name: "automations", Automation: e.ID}
		name := trimOr(e.Name, e.ID)
		crumbs, title = []crumb{allAutomations(), {label: name, name: name, to: back}, {label: L("Edit")}}, L("Edit automation")
	}
	a.automationHeader(c, crumbs, title, nil, func() {
		if a.smallButton(c, kit.Ghost, L("Cancel"), "", false).Clicked() {
			a.Go(back)
		}
		label, busyLabel, action := L("Save changes"), L("Saving…"), "save"
		if e == nil {
			label, busyLabel, action = L("Create automation"), L("Creating…"), "create"
		}
		shown := label
		if s.acting == action {
			shown = busyLabel
		}
		if a.smallButton(c, kit.Primary, shown, "", s.acting != "").Label(label).Clicked() {
			a.saveAutomation(e)
		}
	})
	a.automationNotice(c)
	preview := ""
	if expr, err := f.schedule(off); err == nil {
		preview = L("%s, in this computer’s time. The Daemon keeps it as “%s” in UTC.", automations.Describe(expr, off), expr)
	}
	if f.repeat == automations.Custom {
		preview = L("Five cron fields in UTC, or words such as “every Monday at 9am PST”.")
	}
	repeats := make([]kit.Option, len(automations.Repeats))
	for i, r := range automations.Repeats {
		repeats[i] = kit.Option{Value: string(r), Label: l10n.T(automations.RepeatLabels[r])}
	}
	ui.Column(c).Role(ui.RoleGroup).Label(title).Gap(k.Px(20)).Children(func() {
		a.formField(c, L("Name"), "", problem("name"), func() {
			ui.Row(c).Children(func() {
				in := a.field(c, &f.name, L("Automation name"), L("Daily summary"), false, false)
				if f.errOn == "name" {
					in.Border(1, t.Destructive.Alpha(0.6))
				}
			})
		})
		a.formField(c, L("Prompt"), L("Each run starts a new session with this prompt. Runs do not ask before they use tools."), problem("prompt"), func() {
			border := t.Border
			if f.errOn == "prompt" {
				border = t.Destructive.Alpha(0.6)
			}
			ui.Row(c).Radius(k.Px(10)).Border(1, border).Background(t.Background).Children(func() {
				k.TextArea(c, &f.prompt, L("Automation prompt"), L("Summarize yesterday’s commits in this repository and list anything that looks risky."),
					kit.AreaStyle{Pad: [4]float32{8, 12, 8, 12}, Size: 14, Line: 20, MinLines: 5, Color: t.Foreground})
			})
		})
		a.formField(c, L("Schedule"), preview, problem("schedule"), func() {
			ui.Row(c).Children(func() {
				if next, ok := k.Segmented(c, L("Repeat"), string(f.repeat), repeats); ok {
					f.repeat, f.err, f.errOn = automations.Repeat(next), "", ""
				}
			})
			ui.Row(c).Gap(k.Px(8)).AlignItems(ui.Center).Margin(k.Px(2), 0, 0, 0).Children(func() {
				switch f.repeat {
				case automations.Hourly:
					k.Text(c, L("At minute"), 13, 20).TextColor(t.MutedForeground).Shrink(0)
					a.field(c, &f.minute, L("Minute past the hour"), "00", true, false).Grow(0).Basis(k.Px(64)).Shrink(0)
					k.Text(c, L("past each hour"), 13, 20).TextColor(t.MutedForeground).Shrink(0)
				case automations.Custom:
					a.field(c, &f.custom, L("Cron expression"), "0 9 * * 1-5", true, false)
				default:
					if f.repeat == automations.Weekly {
						days := make([]kit.Option, 7)
						for i := range 7 {
							d := time.Weekday((i + 1) % 7)
							days[i] = kit.Option{Value: strconv.Itoa(int(d)), Label: automations.WeekdayShort(d)}
						}
						if next, ok := k.Segmented(c, L("Day of the week"), strconv.Itoa(int(f.weekday)), days); ok {
							d, _ := strconv.Atoi(next)
							f.weekday = time.Weekday(d)
						}
					}
					k.Text(c, L("At"), 13, 20).TextColor(t.MutedForeground).Shrink(0)
					a.field(c, &f.clock, L("Time of day"), "09:00", true, false).Grow(0).Basis(k.Px(88)).Shrink(0)
				}
			})
		})
		a.formField(c, L("Model"), L("Runs use your default model unless you pick another."), "", func() {
			ui.Row(c).Children(func() {
				a.modelField(c, L("Automation model"), false, &f.modelOpen, &f.picker, choices, shownModel, func(next string) { f.model = next })
			})
		})
		a.formField(c, L("Workspace"), L("Runs work in this folder, with its skills and settings. Leave it empty for the automation’s own folder in ~/.factory/automations."), "", func() {
			ui.Row(c).Gap(k.Px(8)).Children(func() {
				a.field(c, &f.workdir, L("Automation workspace"), "/Users/you/project", true, false)
				if a.smallButton(c, kit.Outline, L("Choose…"), "folder-open", false).Height(k.Px(36)).Label(L("Choose a workspace")).Clicked() {
					go func() {
						paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Title: L("Choose a workspace"), Directory: true})
						if err != nil || len(paths) == 0 {
							return
						}
						a.cfg.Update(func() { f.workdir = paths[0] })
					}()
				}
			})
		})
	})
}

// saveAutomation creates one from the form (e nil) or changes e.
func (a *App) saveAutomation(e *protocol.AutomationEntry) {
	s := &a.automations
	f := &s.form
	f.err, f.errOn = "", ""
	name, prompt, workdir := strings.TrimSpace(f.name), strings.TrimSpace(f.prompt), strings.TrimSpace(f.workdir)
	expr, err := f.schedule(a.offset())
	switch {
	case name == "":
		f.err, f.errOn = L("Give the automation a name."), "name"
	case prompt == "":
		f.err, f.errOn = L("Each run starts from the prompt, so it cannot be empty."), "prompt"
	case err != nil:
		f.err, f.errOn = err.Error(), "schedule"
	}
	if f.err != "" {
		return
	}
	model := f.model
	if e != nil {
		id := e.ID
		a.act(id, l10n.N("save"), L("Saved."), func() error { return a.updateAutomation(id, name, prompt, expr, model, workdir) }, func() {
			a.Go(Route{Name: "automations", Automation: id})
		})
		return
	}
	taken := map[string]bool{}
	for _, x := range s.list {
		taken[x.ID] = true
	}
	id := automations.ID(name, taken)
	skip := true
	a.act("", l10n.N("create"), L("Created. It runs on its schedule, and Run now starts one at once."), func() error {
		cl, err := a.ctl.Client()
		if err != nil {
			return err
		}
		res, err := cl.CreateAutomation(a.ctx, protocol.CreateAutomationParams{
			ID: id, Name: name, Instructions: prompt, Schedule: expr, Model: model, SkipFirstRun: &skip,
			CreationSource: protocol.AutomationCreationSourceDesktop,
		})
		if err = refused(err == nil && res.Success, errText(res), err); err != nil || workdir == "" {
			return err
		}
		// create_automation takes no Workspace; the full update does.
		return a.updateAutomation(id, name, prompt, expr, model, workdir)
	}, func() {
		a.Go(Route{Name: "automations", Automation: id})
	})
}

func errText(res *protocol.CreateAutomationResult) string {
	if res == nil {
		return ""
	}
	return res.Error
}

func (a *App) updateAutomation(id, name, prompt, expr, model, workdir string) error {
	cl, err := a.ctl.Client()
	if err != nil {
		return err
	}
	res, err := cl.UpdateAutomation(a.ctx, protocol.UpdateAutomationParams{
		AutomationID: id, Name: name, Prompt: prompt, Schedule: expr, Model: model, WorkingDirectory: workdir,
	})
	if err != nil {
		return err
	}
	return refused(res.Success, res.Error, nil)
}
