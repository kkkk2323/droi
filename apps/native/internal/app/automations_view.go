package app

import (
	"cmp"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/internal/activity"
	"github.com/kkkk2323/droi/apps/native/internal/automations"
	"github.com/kkkk2323/droi/apps/native/pkg/gateway"
	"github.com/kkkk2323/droi/apps/native/pkg/kit"
	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
	"github.com/kkkk2323/droi/apps/native/pkg/models"
	"github.com/kkkk2323/droi/apps/native/pkg/sessions"
	"github.com/kkkk2323/droi/apps/native/pkg/settingsview"
)

// The Host's Automations come through the Gateway (ADR 0035). This
// computer's Host tells the window when they change (Config.AutomationsChanged);
// another computer's are read again now and then, and the runs of the one
// shown more often.
const (
	automationsRefresh = 30 * time.Second
	runsRefresh        = 15 * time.Second
	runningRefresh     = 3 * time.Second
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
	list    []gateway.Automation
	listErr string
	// unavailable is why the Host shows no Automations (troubleOlderHost or
	// troubleElsewhere); troubleNone when it does.
	unavailable automationTrouble
	listAt      time.Time
	loading     bool
	// listAgain is a forced read asked for while one was under way, which
	// may answer from before the change that asked for it.
	listAgain bool

	// factory are the Factory automations on the Host's computer, read
	// with the list; nil when the Host could not list them.
	factory []gateway.FactoryAutomation
	// factoryErr is why the Factory automations did not load, "" when they
	// did.
	factoryErr string
	// zone is the time zone of the Host's computer, "" until read.
	zone string

	runs     map[string][]gateway.AutomationRun
	runsErr  map[string]string
	runsAt   map[string]time.Time
	runsBusy map[string]bool
	// runsAgain is listAgain for the runs of each Automation.
	runsAgain map[string]bool

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
	// promptOpen shows all of a long prompt.
	promptOpen bool
	// moving is the Factory automation the move dialog asks about, while
	// confirmMove, and until its move ends.
	moving      gateway.FactoryAutomation
	confirmMove bool

	// live are the runs whose Session is busy this frame, newest first;
	// liveKey names them, to read the list again when one starts or ends.
	live    []sessions.Summary
	liveKey string
	// waiting are the runs, by Session, that wait for the user's approval.
	waiting map[string]bool
	// busy is every Session's activity, this frame.
	busy map[string]activity.Activity
	// daemonByID and daemonByUUID are the Sessions the Daemon started as
	// runs of Factory automations, newest first, by the automation's folder
	// name and uuid; built from the Session list of revision daemonRev.
	daemonByID, daemonByUUID map[string][]sessions.Summary
	daemonRev                uint64
	daemonIndexed            bool
}

// noteLiveRuns finds the Automations' runs at work among the listed
// Sessions: the Daemon reports every Session's working state, a run's too.
func (a *App) noteLiveRuns(listed []sessions.Summary, busy map[string]activity.Activity) {
	s := &a.automations
	s.live = s.live[:0]
	s.busy = busy
	clear(s.waiting)
	a.indexDaemonRuns(listed)
	var ids []string
	for _, x := range listed {
		if x.CallingSessionID != "" || busy[x.SessionID] == "" {
			continue
		}
		if id, _ := sessions.AutomationRun(x.Tags); id != "" && x.ArchivedAt == "" {
			s.live = append(s.live, x)
			ids = append(ids, x.SessionID)
			if busy[x.SessionID] == activity.NeedsInput {
				if s.waiting == nil {
					s.waiting = map[string]bool{}
				}
				s.waiting[x.SessionID] = true
			}
		}
	}
	slices.SortStableFunc(s.live, newestFirst)
	key := strings.Join(ids, ",")
	if key == s.liveKey {
		return
	}
	s.liveKey = key
	if a.route.Name == "automations" {
		a.automationsChanged()
	}
}

func newestFirst(p, q sessions.Summary) int { return int(q.UpdatedAt - p.UpdatedAt) }

// indexDaemonRuns indexes the Daemon's runs among the listed Sessions, once
// per revision of the list (App.shownRev): the frames between read it.
func (a *App) indexDaemonRuns(listed []sessions.Summary) {
	s := &a.automations
	if s.daemonIndexed && s.daemonRev == a.shownRev {
		return
	}
	s.daemonIndexed, s.daemonRev = true, a.shownRev
	s.daemonByID, s.daemonByUUID = map[string][]sessions.Summary{}, map[string][]sessions.Summary{}
	for _, x := range listed {
		id, uuid := daemonRun(x.Tags)
		if x.CallingSessionID != "" || id == "" {
			continue
		}
		s.daemonByID[id] = append(s.daemonByID[id], x)
		if uuid != "" {
			s.daemonByUUID[uuid] = append(s.daemonByUUID[uuid], x)
		}
	}
	for _, runs := range s.daemonByID {
		slices.SortStableFunc(runs, newestFirst)
	}
	for _, runs := range s.daemonByUUID {
		slices.SortStableFunc(runs, newestFirst)
	}
}

// daemonRun is the Factory automation the Daemon started the Session as a
// run of (sessions.AutomationTag): its folder name and, when it has one,
// its uuid; "" for none.
func daemonRun(tags []sessions.Tag) (id, uuid string) {
	for _, t := range tags {
		if t.Name == sessions.AutomationTag && t.Metadata["type"] == "run" {
			return t.Metadata["automationId"], t.Metadata["automationUuid"]
		}
	}
	return "", ""
}

// daemonRunsOfMoved are the Daemon's runs of the Factory automation e was
// moved from, newest first. As the Daemon does (local-history.ts), a uuid
// matches alone: a later automation may take a deleted one's folder name.
func (a *App) daemonRunsOfMoved(e gateway.Automation) []sessions.Summary {
	name := cmp.Or(e.ForkedFromName, e.ForkedFrom)
	if e.ForkedFrom == name {
		return a.daemonRunsOf(name)
	}
	return a.automations.daemonByUUID[e.ForkedFrom]
}

// daemonRunsOf are the Daemon's runs of the Factory automation id, newest
// first.
func (a *App) daemonRunsOf(id string) []sessions.Summary {
	return a.automations.daemonByID[id]
}

// automationsChanged reads the list, and the runs of the Automation shown,
// again.
func (a *App) automationsChanged() {
	a.loadAutomations(true)
	if id := a.route.Automation; id != "" {
		a.loadRuns(id, true)
	}
}

// liveRuns are the busy runs of the Automation id.
func (a *App) liveRuns(id string) int {
	n := 0
	for _, x := range a.automations.live {
		if aid, _ := sessions.AutomationRun(x.Tags); aid == id {
			n++
		}
	}
	return n
}

// runLive is whether the run's Session is busy this frame.
func (a *App) runLive(sessionID string) bool {
	for _, x := range a.automations.live {
		if x.SessionID == sessionID {
			return true
		}
	}
	return false
}

// automationForm is what the editor's fields hold.
type automationForm struct {
	name, prompt, clock, minute, custom, workdir string
	repeat                                       automations.Repeat
	weekday                                      time.Weekday
	// timezone is the Automation's own zone, kept for a Custom schedule;
	// "" is the Host's computer's.
	timezone string
	// model and effort are "" for the user's defaults.
	model, effort string
	// autonomy is "" to run without asking, or low, medium or high.
	autonomy  string
	modelOpen bool
	picker    pickerState
	// err is what is wrong with the field errOn.
	err, errOn string
}

// enterAutomations starts a page of Automations afresh.
func (a *App) enterAutomations(r Route) {
	s := &a.automations
	s.confirmDelete, s.confirmMove, s.menuOpen, s.promptOpen, s.notice = false, false, false, false, ""
	if r.Tab != "" {
		s.formFor = ""
	}
}

// loadAutomations reads the Automations and the Factory automations from
// the Gateway, off the frame.
func (a *App) loadAutomations(force bool) {
	s := &a.automations
	if s.loading {
		s.listAgain = s.listAgain || force
		return
	}
	gw := a.cfg.Gateway
	if gw == nil || !force && !s.listAt.IsZero() && a.cfg.Now().Sub(s.listAt) < automationsRefresh {
		return
	}
	s.loading = true
	needZone := s.zone == ""
	go func() {
		list, err := gw.Automations(a.ctx)
		var factory []gateway.FactoryAutomation
		var factoryErr error
		zone := ""
		if err == nil {
			// Without them (the Daemon not up yet) the section stays out.
			factory, factoryErr = gw.FactoryAutomations(a.ctx)
			if needZone {
				zone, _ = gw.AutomationZone(a.ctx)
			}
		}
		a.cfg.Update(func() {
			s.loading = false
			s.listAt = a.cfg.Now()
			trouble := troubleOf(err)
			if trouble == troubleOlderHost && !a.following() {
				// This computer's Host is this Droi: a 404 is no older one.
				trouble = troubleOther
			}
			if zone != "" {
				s.zone = zone
			}
			switch {
			case trouble == troubleOlderHost || trouble == troubleElsewhere:
				s.unavailable, s.listErr = trouble, ""
			case err != nil:
				s.unavailable, s.listErr = troubleNone, L("Automations did not load: %s", err)
				// Soon again: the Host may only be starting.
				s.listAt = s.listAt.Add(5*time.Second - automationsRefresh)
			default:
				if list == nil {
					list = []gateway.Automation{}
				}
				s.unavailable, s.listErr, s.list, s.factory, s.factoryErr = troubleNone, "", list, factory, ""
				if factoryErr != nil {
					s.factoryErr = L("Automations did not load: %s", factoryErr)
				}
			}
			if s.listAgain {
				s.listAgain = false
				a.loadAutomations(true)
			}
		})
	}()
}

// runsEvery is how often the runs of the Automation id are read again:
// more often while one is running.
func (a *App) runsEvery(id string) time.Duration {
	for _, r := range a.automations.runs[id] {
		if r.Status == "running" {
			return runningRefresh
		}
	}
	return runsRefresh
}

func (a *App) loadRuns(id string, force bool) {
	s := &a.automations
	if s.runs == nil {
		s.runs, s.runsErr, s.runsAt, s.runsBusy = map[string][]gateway.AutomationRun{}, map[string]string{}, map[string]time.Time{}, map[string]bool{}
		s.runsAgain = map[string]bool{}
	}
	every := a.runsEvery(id)
	if s.runsBusy[id] {
		s.runsAgain[id] = s.runsAgain[id] || force
		return
	}
	gw := a.cfg.Gateway
	if gw == nil || !force && !s.runsAt[id].IsZero() && a.cfg.Now().Sub(s.runsAt[id]) < every {
		return
	}
	s.runsBusy[id] = true
	go func() {
		runs, err := gw.AutomationRuns(a.ctx, id)
		a.cfg.Update(func() {
			s.runsBusy[id] = false
			s.runsAt[id] = a.cfg.Now()
			if err != nil {
				s.runsErr[id] = err.Error()
			} else {
				if runs == nil {
					runs = []gateway.AutomationRun{}
				}
				s.runsErr[id], s.runs[id] = "", runs
			}
			if s.runsAgain[id] {
				s.runsAgain[id] = false
				a.loadRuns(id, true)
			}
		})
	}()
}

// automationTrouble is what went wrong with a request of the Automation
// endpoints, as the page tells it.
type automationTrouble int

const (
	troubleNone automationTrouble = iota
	troubleOther
	// troubleOlderHost is a Host without the endpoints: an older Droi.
	troubleOlderHost
	// troubleElsewhere is a Host whose Automations another Droi on its
	// computer runs.
	troubleElsewhere
	// troubleRunning is a run asked for while one works.
	troubleRunning
	// troubleGone is an Automation the Host no longer has.
	troubleGone
	// troubleRefused is a request the Host refused with a reason, already
	// in the user's language.
	troubleRefused
)

// troubleOf reads a Gateway error of the Automation endpoints; the one
// place that knows how the Host answers them.
func troubleOf(err error) automationTrouble {
	var st *gateway.StatusError
	switch {
	case err == nil:
		return troubleNone
	case errors.Is(err, gateway.ErrNotFound):
		return troubleOlderHost
	case errors.Is(err, gateway.ErrAutomationNotFound):
		return troubleGone
	case errors.Is(err, gateway.ErrAutomationsElsewhere):
		return troubleElsewhere
	case errors.Is(err, gateway.ErrAutomationRunning):
		return troubleRunning
	case errors.As(err, &st) && st.Status == http.StatusBadRequest && st.Message != "":
		return troubleRefused
	}
	return troubleOther
}

// failure is what the page says of an action on the Automation called name
// that the Host did not carry out.
func (a *App) failure(action, name string, err error) string {
	switch troubleOf(err) {
	case troubleElsewhere:
		return a.elsewhereText()
	case troubleGone:
		return L("It may have been deleted.")
	case troubleRunning:
		return L("A run of “%s” is already working.", name)
	case troubleRefused:
		return err.Error()
	}
	return L("Could not %s: %s", l10n.T(action), err)
}

// act runs one action on an Automation off the frame, then reads the list
// and the runs again and says how it went.
func (a *App) act(id, action, done string, fn func() error, then func()) {
	name := id
	if e := a.findAutomation(id); e != nil {
		name = trimOr(e.Name, e.ID)
	}
	a.actWith(id, action, func() (string, bool) {
		if err := fn(); err != nil {
			return a.failure(action, name, err), true
		}
		return done, false
	}, then)
}

// actWith is act with fn saying how it went: the notice, and whether it
// failed.
func (a *App) actWith(id, action string, fn func() (notice string, failed bool), then func()) {
	s := &a.automations
	if s.acting != "" {
		return
	}
	s.acting, s.actingOn, s.notice = action, id, ""
	go func() {
		notice, failed := fn()
		a.cfg.Update(func() {
			s.acting, s.actingOn = "", ""
			if !failed && then != nil {
				// then may go to another page, which drops the notice.
				then()
			}
			s.noticeAt, s.noticeErr, s.notice = a.cfg.Now(), failed, notice
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
	if left := automationsRefresh - a.cfg.Now().Sub(s.listAt); !s.listAt.IsZero() && left > 0 {
		c.After(left)
	}
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
				case s.unavailable != troubleNone:
					a.unavailableAutomations(c)
				case a.route.Tab == newAutomationTab:
					a.automationEditor(c, nil)
				case a.route.Factory != "":
					a.factoryView(c, a.route.Factory)
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
		if s.confirmMove {
			a.moveDialog(c)
		}
	})
}

// unavailableAutomations says why the Host shows no Automations. On this
// computer a Host without them is one whose Automations another Droi runs.
func (a *App) unavailableAutomations(c *ui.Context) {
	k, t := a.kit, a.kit.T
	a.automationHeader(c, nil, L("Automations"), nil, nil)
	msg := a.elsewhereText()
	if a.following() && a.automations.unavailable == troubleOlderHost {
		msg = L("%s runs an older Droi that cannot show its automations here. Update Droi there.", a.computerName())
	}
	k.Text(c, msg, 13, 20).Role(ui.RoleStatus).TextColor(t.MutedForeground).Margin(-k.Px(16), 0, 0, 0)
}

// elsewhereText says another Droi on the Host's computer runs its
// Automations.
func (a *App) elsewhereText() string {
	if a.following() {
		return L("Another Droi on %s runs these automations. Quit it to manage them there.", a.computerName())
	}
	return L("Another Droi on this computer runs these automations. Quit it to manage them here.")
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
			k.Text(c, title, 20, 28).Role(ui.RoleHeading).FontWeight(500).LetterSpacing(-k.Px(0.3)).SingleLine().Shrink(1).MinWidth(0)
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
	ui.Row(c.Key("notice")).Role(ui.RoleStatus).Label(s.notice).Gap(k.Px(8)).AlignItems(ui.Start).
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
	a.factorySection(c)
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

// automationStatus is "active", "paused" or "invalid".
func automationStatus(e gateway.Automation) string {
	switch {
	case e.Invalid != "":
		return "invalid"
	case e.Paused:
		return "paused"
	}
	return "active"
}

// lastRunStatus is the status of the Automation's last run, "" for none.
func lastRunStatus(e gateway.Automation) string {
	if e.LastRun == nil {
		return ""
	}
	return e.LastRun.Status
}

// scheduleWords is a schedule in words, with the zone of its own when it
// has one.
func scheduleWords(expr, ownZone string) string {
	words := automations.Describe(expr, 0)
	if ownZone != "" {
		words = strings.TrimRight(L("%s, in the %s time zone.", words, ownZone), ".。")
	}
	return words
}

// summary is an Automation's schedule and its last run, in words.
func (a *App) summary(e gateway.Automation) string {
	parts := []string{scheduleWords(e.Schedule, e.Timezone)}
	if e.LastRun != nil {
		if w := automations.When(e.LastRun.StartedAt, a.cfg.Now()); w != "" {
			parts = append(parts, listName(L("last run %s", w), strings.ToLower(l10n.T(automations.RunLabel(e.LastRun.Status)))))
		}
	}
	return strings.Join(parts, " · ")
}

// nextRun is when an Automation runs next, or why it does not.
func (a *App) nextRun(e gateway.Automation) string {
	if st := automationStatus(e); st != "active" {
		return automations.StatusLabel(st)
	}
	if w := automations.When(e.NextRunAt, a.cfg.Now()); w != "" {
		return w
	}
	return L("Not scheduled")
}

// automationMark is an Automation's state at a glance: running, active,
// paused, invalid, or with a failed last run.
func (a *App) automationMark(c *ui.Context, e gateway.Automation) {
	k, t := a.kit, a.kit.T
	ui.Box(c).Size(k.Px(16), k.Px(16)).Center().Shrink(0).Children(func() {
		switch {
		case e.Running || a.liveRuns(e.ID) > 0:
			k.Spinner(c, 14, t.Info).Role(ui.RoleImage).Label(L("Running"))
		case e.Invalid != "":
			k.Icon(c, "circle-alert", 14, t.DestructiveForeground)
		case e.Paused:
			k.Icon(c, "circle-pause", 14, t.MutedForeground)
		case automations.RunLabel(lastRunStatus(e)) == "Failed":
			kit.Dot(c, k.Px(8), t.Destructive)
		default:
			kit.Dot(c, k.Px(8), t.Success)
		}
	})
}

// runNow starts a run of the Automation id at once; the Host refuses one
// while another works, and says so.
func (a *App) runNow(id string) {
	gw := a.cfg.Gateway
	a.act(id, l10n.N("run"), L("Started a run."), func() error {
		_, err := gw.RunAutomation(a.ctx, id)
		return err
	}, nil)
}

// automationRow opens the Automation, and shows a Run now button while the
// pointer is over it.
func (a *App) automationRow(c *ui.Context, e gateway.Automation) {
	k, t := a.kit, a.kit.T
	s := &a.automations
	name := trimOr(e.Name, e.ID)
	row := ui.Row(c.Key(e.ID)).Transition(rowMotion).Height(k.Px(56)).PaddingX(k.Px(12)).Gap(k.Px(8)).AlignItems(ui.Center).
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
				k.Spinner(c, 14, t.MutedForeground).Role(ui.RoleImage).Label(L("Starting…"))
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

// factorySection lists the Factory automations on the Host's computer,
// each opening its page, with a way to move it to Droi; nothing when there
// are none.
func (a *App) factorySection(c *ui.Context) {
	k, t := a.kit, a.kit.T
	s := &a.automations
	if len(s.factory) == 0 {
		return
	}
	title := L("Factory automations on this computer")
	if a.following() {
		title = L("Factory automations on %s", a.computerName())
	}
	ui.Column(c).Role(ui.RoleGroup).Label(title).Gap(k.Px(4)).Children(func() {
		a.sectionTitle(c, title, strconv.Itoa(len(s.factory)))
		k.Text(c, L("The Daemon runs these and syncs them to Factory, which can pause one it counts as missed. Move one to Droi to run it here without the sync."), 13, 20).
			TextColor(t.MutedForeground)
		ui.Column(c).Role(ui.RoleList).Label(title).Margin(k.Px(4), 0, 0, 0).Children(func() {
			for _, f := range s.factory {
				a.factoryRow(c, f)
			}
		})
	})
}

// factorySchedule is a Factory automation's schedule as Droi keeps it once
// moved, which the Host gives: the expression and its own zone, "" for the
// Host's. One Droi cannot keep is as the Daemon has it.
func factorySchedule(f gateway.FactoryAutomation) (expr, ownZone string) {
	if f.LocalSchedule == "" {
		return f.Schedule, ""
	}
	return f.LocalSchedule, f.LocalTimezone
}

// factoryMark is a Factory automation's state at a glance: paused or not.
func (a *App) factoryMark(c *ui.Context, f gateway.FactoryAutomation) {
	k, t := a.kit, a.kit.T
	ui.Box(c).Size(k.Px(16), k.Px(16)).Center().Shrink(0).Children(func() {
		if f.Paused {
			k.Icon(c, "circle-pause", 14, t.MutedForeground).Role(ui.RoleImage).Label(L("Paused"))
		} else {
			kit.Dot(c, k.Px(8), t.MutedForeground.Alpha(0.6))
		}
	})
}

// factoryMove is a Factory automation's Move to Droi button, a spinner
// while it moves, or why it cannot move.
func (a *App) factoryMove(c *ui.Context, f gateway.FactoryAutomation, v kit.Variant) {
	k, t := a.kit, a.kit.T
	s := &a.automations
	name := trimOr(f.Name, f.ID)
	switch {
	case f.TargetsSession:
		k.Text(c, L("Posts into an existing session, so it can’t move to Droi yet"), 12, 16).TextColor(t.MutedForeground).
			MaxWidth(k.Px(220)).Shrink(1)
	case f.NotMovable != "":
		k.Text(c, L("Its schedule never ran in Factory, so it can’t move to Droi"), 12, 16).TextColor(t.MutedForeground).
			MaxWidth(k.Px(220)).Shrink(1)
	case s.acting == "move" && s.moving.ID == f.ID:
		k.Spinner(c, 14, t.MutedForeground).Role(ui.RoleImage).Label(L("Moving…"))
	default:
		if a.smallButton(c, v, L("Move to Droi"), "", s.acting != "").Label(L("Move %s to Droi", name)).Clicked() {
			s.moving, s.confirmMove = f, true
		}
	}
}

// factoryRow opens the Factory automation's page, with its Move to Droi
// button beside.
func (a *App) factoryRow(c *ui.Context, f gateway.FactoryAutomation) {
	k, t := a.kit, a.kit.T
	name := trimOr(f.Name, f.ID)
	row := ui.Row(c.Key(f.ID)).Transition(rowMotion).Height(k.Px(56)).PaddingX(k.Px(12)).Gap(k.Px(12)).AlignItems(ui.Center).
		BorderWidth(0, 0, 1, 0).BorderColor(t.Border.Alpha(0.6))
	if row.Hovered() {
		row.Background(t.Muted.Alpha(0.5))
	}
	row.Children(func() {
		open := ui.ButtonBase(c).Role(ui.RoleListItem).Label(name).Grow(1).MinWidth(0).AlignSelf(ui.Stretch).Gap(k.Px(12)).Justify(ui.Start).Cursor(ui.CursorPointer)
		open.Children(func() {
			a.factoryMark(c, f)
			ui.Column(c).Grow(1).MinWidth(0).Gap(k.Px(2)).Children(func() {
				k.Text(c, name, 14, 20).FontWeight(500).SingleLine()
				k.Text(c, scheduleWords(factorySchedule(f)), 12, 16).TextColor(t.MutedForeground).SingleLine()
			})
		})
		if open.Clicked() {
			a.Go(Route{Name: "automations", Factory: f.ID})
		}
		a.factoryMove(c, f, kit.Outline)
	})
}

func (a *App) findFactory(id string) *gateway.FactoryAutomation {
	for i := range a.automations.factory {
		if a.automations.factory[i].ID == id {
			return &a.automations.factory[i]
		}
	}
	return nil
}

// factoryView is a Factory automation's page: what it is, Move to Droi,
// and its runs, which the Daemon tagged.
func (a *App) factoryView(c *ui.Context, id string) {
	k, t := a.kit, a.kit.T
	s := &a.automations
	f := a.findFactory(id)
	if f == nil {
		// The list or the Factory automations did not load: say so, not
		// that it is gone.
		failed := s.listErr
		if s.list != nil && failed == "" {
			failed = s.factoryErr
		}
		if failed == "" {
			a.missingAutomation(c)
			return
		}
		a.automationHeader(c, []crumb{allAutomations(), {label: L("Factory automation")}}, L("Factory automation"), nil, nil)
		a.loadFailed(c, failed, func() { a.loadAutomations(true) })
		return
	}
	name := trimOr(f.Name, f.ID)
	a.automationHeader(c, []crumb{allAutomations(), {label: name}}, name, func() {
		k.Text(c, L("Factory automation"), 12, 16).FontWeight(500).TextColor(t.MutedForeground).PaddingX(k.Px(7)).Radius(k.Px(10)).
			Background(t.Muted).SingleLine().Shrink(0)
		if f.Paused {
			a.statusPill(c, false, L("Paused"))
		}
	}, func() {
		if f.NotMovable == "" {
			a.factoryMove(c, *f, kit.Primary)
		}
	})
	a.automationNotice(c)
	if why := f.NotMovable; why != "" && !f.TargetsSession {
		// The Host's reason is English: it is the tooltip, not the line.
		msg := L("Its schedule never ran in Factory, so it can’t move to Droi")
		ui.Row(c).Role(ui.RoleStatus).Label(msg).Gap(k.Px(8)).AlignItems(ui.Start).Children(func() {
			k.Icon(c, "circle-alert", 14, t.MutedForeground).Margin(k.Px(3), 0, 0, 0)
			k.Text(c, msg, 13, 20).TextColor(t.Foreground).Grow(1).MinWidth(0).Tooltip(why)
		})
	}
	ui.Column(c).Role(ui.RoleGroup).Label(L("Details")).Gap(k.Px(8)).Children(func() {
		expr, zone := factorySchedule(*f)
		if zone == "" && f.LocalSchedule != "" {
			zone = s.zone
		}
		a.scheduleFact(c, expr, zone)
		if w := trimOr(f.WorkingDirectory, f.Path); w != "" {
			a.fact(c, L("Workspace"), func() { k.Text(c, w, 12, 20).Font(k.Mono).SingleLine().Shrink(1).MinWidth(0) })
		}
	})
	a.promptSection(c, f.Prompt)
	runs := a.daemonRunsOf(id)
	ui.Column(c).Gap(k.Px(4)).Children(func() {
		count := ""
		if len(runs) > 0 {
			count = strconv.Itoa(len(runs))
		}
		a.sectionTitle(c, L("Runs"), count)
		if len(runs) == 0 {
			k.Text(c, L("No runs yet."), 13, 20).TextColor(t.MutedForeground).Padding(k.Px(8), 0)
			return
		}
		a.daemonRunList(c, L("Runs"), runs)
	})
}

// daemonRunList lists runs the Daemon made, each opening its Session.
func (a *App) daemonRunList(c *ui.Context, label string, runs []sessions.Summary) {
	ui.Column(c).Role(ui.RoleList).Label(label).Children(func() {
		for _, x := range runs {
			a.daemonRunRow(c, x)
		}
	})
}

// daemonRunRow is one of the Daemon's runs: its Session's title, what it
// is doing, and when it was last at work.
func (a *App) daemonRunRow(c *ui.Context, x sessions.Summary) {
	k, t := a.kit, a.kit.T
	when := automations.When(time.Unix(x.UpdatedAt, 0).UTC().Format(time.RFC3339), a.cfg.Now())
	doing := a.automations.busy[x.SessionID]
	b := ui.ButtonBase(c.Key(x.SessionID)).Transition(rowMotion).Role(ui.RoleListItem).Label(L("Open run from %s", when)).Height(k.Px(40)).Gap(k.Px(10)).
		PaddingX(k.Px(12)).Justify(ui.Start).BorderWidth(0, 0, 1, 0).BorderColor(t.Border.Alpha(0.6)).Cursor(ui.CursorPointer)
	hover := b.Hovered()
	if hover {
		b.Background(t.Muted.Alpha(0.5))
	}
	b.Children(func() {
		ui.Box(c).Size(k.Px(16), k.Px(16)).Center().Shrink(0).Children(func() {
			switch doing {
			case activity.NeedsInput:
				k.Icon(c, "circle-alert", 14, t.Attention)
			case "":
				k.Icon(c, "circle", 14, t.MutedForeground)
			default:
				k.Spinner(c, 14, t.Info)
			}
		})
		k.Text(c, x.Title, 13, 20).SingleLine().Shrink(1).MinWidth(0)
		switch doing {
		case activity.NeedsInput:
			k.Text(c, L("Waiting for your approval"), 13, 20).TextColor(t.MutedForeground).SingleLine().Shrink(0)
		case "":
		default:
			k.Text(c, L("Running"), 13, 20).TextColor(t.MutedForeground).SingleLine().Shrink(0)
		}
		ui.Spacer(c)
		k.Text(c, when, 12, 16).TextColor(t.MutedForeground).FontFeatures("tnum").Shrink(0)
		chevron := k.Icon(c, "chevron-right", 14, t.MutedForeground)
		if !hover {
			chevron.Opacity(0)
		}
	})
	if b.Clicked() {
		a.Go(Route{Name: "session", SessionID: x.SessionID})
	}
}

// confirmDialog asks before an action: its title, what it does, Cancel,
// and the button button draws, whose click closes it and calls confirmed.
func (a *App) confirmDialog(c *ui.Context, open *bool, title, body string, button func() ui.Element, confirmed func()) {
	k, t := a.kit, a.kit.T
	w, _ := c.Size()
	ui.DialogBase(c, open, func(backdrop, panel ui.Element) {
		backdrop.Background(t.Scrim)
		panel.Role(ui.RoleAlertDialog).Label(title).Width(min(k.Px(420), w-k.Px(32))).Padding(k.Px(20)).Radius(k.Px(12)).Border(1, t.Border).
			Background(t.Popover).TextColor(t.PopoverForeground).Shadow(0, k.Px(20), k.Px(25), -k.Px(5), t.Shadow)
		ui.Column(c).FillWidth().Gap(k.Px(6)).Children(func() {
			k.Text(c, title, 15, 22).Role(ui.RoleHeading).FontWeight(600)
			k.Text(c, body, 13, 20).TextColor(t.MutedForeground)
			ui.Row(c).Gap(k.Px(8)).Justify(ui.End).Margin(k.Px(14), 0, 0, 0).Children(func() {
				if a.smallButton(c, kit.Outline, L("Cancel"), "", false).Clicked() {
					*open = false
				}
				if button().Clicked() {
					*open = false
					confirmed()
				}
			})
		})
	})
}

// moveDialog asks before the Factory automation s.moving moves to Droi.
func (a *App) moveDialog(c *ui.Context) {
	s := &a.automations
	f := s.moving
	body := L("Droi copies it, with its schedule in this computer’s time, and deletes it from Factory’s automations. The sessions of its past runs stay.")
	if a.following() {
		body = L("Droi copies it, with its schedule in %s’s time, and deletes it from Factory’s automations. The sessions of its past runs stay.", a.computerName())
	}
	a.confirmDialog(c, &s.confirmMove, L("Move “%s” to Droi?", trimOr(f.Name, f.ID)), body,
		func() ui.Element { return a.smallButton(c, kit.Primary, L("Move to Droi"), "", s.acting != "") },
		func() { a.moveFactory(f) })
}

// moveFactory has the Host copy the Factory automation f into Droi's and
// delete it from Factory's, then reads both lists again; moved from its
// page, the copy's page opens.
func (a *App) moveFactory(f gateway.FactoryAutomation) {
	gw := a.cfg.Gateway
	name := trimOr(f.Name, f.ID)
	var moved gateway.AutomationMoved
	a.actWith("", "move", func() (string, bool) {
		var err error
		moved, err = gw.MoveFactoryAutomation(a.ctx, f.ID)
		switch {
		case err != nil:
			if tr := troubleOf(err); tr == troubleElsewhere || tr == troubleGone {
				return a.failure("", name, err), true
			}
			return L("Could not move “%s”: %s", name, err), true
		case moved.DeleteError != "":
			return L("Copied “%s” to Droi, but it is still in Factory’s automations: %s. Delete it there so it does not run twice.", name, strings.TrimRight(moved.DeleteError, ".")), true
		case moved.Automation.Paused:
			return L("Moved “%s” to Droi. It stays paused.", name), false
		}
		return L("Moved “%s” to Droi.", name), false
	}, func() {
		if a.route.Factory == f.ID && moved.Automation.ID != "" {
			a.putAutomation(moved.Automation)
			a.Go(Route{Name: "automations", Automation: moved.Automation.ID})
		}
	})
}

// findAutomation is the listed Automation id, nil when the list has none.
func (a *App) findAutomation(id string) *gateway.Automation {
	for i := range a.automations.list {
		if a.automations.list[i].ID == id {
			return &a.automations.list[i]
		}
	}
	return nil
}

// putAutomation puts e, as the Host saved it, in the list, until the list
// is read again.
func (a *App) putAutomation(e gateway.Automation) {
	s := &a.automations
	if x := a.findAutomation(e.ID); x != nil {
		*x = e
		return
	}
	s.list = append(s.list, e)
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
	k.Text(c, L("It may have been deleted."), 13, 20).TextColor(t.MutedForeground).Margin(-k.Px(16), 0, 0, 0)
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

// scheduleFact is a page's Schedule: the schedule in words, unless the
// words are the expression itself, then the expression and the zone it is
// read in.
func (a *App) scheduleFact(c *ui.Context, expr, zone string) {
	k, t := a.kit, a.kit.T
	expr = strings.TrimSpace(expr)
	a.fact(c, L("Schedule"), func() {
		if words := automations.Describe(expr, 0); words != expr {
			k.Text(c, words, 13, 20).SingleLine().Shrink(0)
		}
		shown := expr
		if zone != "" {
			shown += "  " + zone
		}
		k.Text(c, shown, 12, 20).Font(k.Mono).TextColor(t.MutedForeground).SingleLine().Shrink(1).MinWidth(0)
	})
}

// permissionsLabel is what an Automation's runs may do without asking.
func permissionsLabel(autonomy string) string {
	if autonomy == "" {
		return L("Don’t ask")
	}
	if l, ok := models.AutonomyLabels[autonomy]; ok {
		return l10n.T(l)
	}
	return autonomy
}

func (a *App) automationView(c *ui.Context, e *gateway.Automation) {
	k, t := a.kit, a.kit.T
	s := &a.automations
	id, name := e.ID, trimOr(e.Name, e.ID)
	a.loadRuns(id, false)
	if a.following() {
		// Another computer's Host tells of no change: read the runs again
		// when due, often while one is running.
		if left := a.runsEvery(id) - a.cfg.Now().Sub(s.runsAt[id]); left > 0 {
			c.After(left)
		}
	}
	busy := s.acting != ""
	status := automationStatus(*e)
	a.automationHeader(c, []crumb{allAutomations(), {label: name}}, name, func() {
		a.statusPill(c, status == "active", automations.StatusLabel(status))
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
			if e.Paused {
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
	a.automationNotice(c)
	if e.Invalid != "" {
		ui.Row(c).Role(ui.RoleStatus).Label(e.Invalid).Gap(k.Px(8)).AlignItems(ui.Start).Children(func() {
			k.Icon(c, "circle-alert", 14, t.DestructiveForeground).Margin(k.Px(3), 0, 0, 0)
			k.Text(c, e.Invalid, 13, 20).TextColor(t.DestructiveForeground).Grow(1).MinWidth(0)
		})
	}
	ui.Column(c).Role(ui.RoleGroup).Label(L("Details")).Gap(k.Px(8)).Children(func() {
		a.scheduleFact(c, e.Schedule, trimOr(e.Zone, e.Timezone))
		a.fact(c, L("Next run"), func() { k.Text(c, a.nextRun(*e), 13, 20).FontFeatures("tnum").SingleLine() })
		if e.LastRun != nil {
			if w := automations.When(e.LastRun.StartedAt, a.cfg.Now()); w != "" {
				a.fact(c, L("Last run"), func() {
					k.Text(c, w, 13, 20).FontFeatures("tnum").SingleLine()
					k.Text(c, l10n.T(automations.RunLabel(e.LastRun.Status)), 13, 20).TextColor(t.MutedForeground).SingleLine()
				})
			}
		}
		a.fact(c, L("Model"), func() {
			k.Text(c, a.modelName(e.Model), 13, 20).SingleLine().Shrink(1).MinWidth(0)
			if e.ReasoningEffort != "" {
				k.Text(c, settingsview.EffortLabel(e.ReasoningEffort), 13, 20).TextColor(t.MutedForeground).SingleLine().Shrink(0)
			}
		})
		a.fact(c, L("Permissions"), func() { k.Text(c, permissionsLabel(e.Autonomy), 13, 20).SingleLine() })
		a.fact(c, L("Workspace"), func() {
			if w := e.WorkingDirectory; w != "" {
				k.Text(c, w, 12, 20).Font(k.Mono).SingleLine().Shrink(1).MinWidth(0)
			} else {
				k.Text(c, L("Its own folder in Droi’s data"), 13, 20).TextColor(t.MutedForeground).SingleLine().Shrink(1).MinWidth(0).Tooltip(e.Folder)
			}
		})
	})
	a.promptSection(c, e.Prompt)
	a.runsList(c, id)
	if e.ForkedFrom != "" {
		if runs := a.daemonRunsOfMoved(*e); len(runs) > 0 {
			ui.Column(c).Gap(k.Px(4)).Children(func() {
				a.sectionTitle(c, L("Earlier runs in Factory"), strconv.Itoa(len(runs)))
				a.daemonRunList(c, L("Earlier runs in Factory"), runs)
			})
		}
	}
}

// promptSection is a page's prompt, its first lines while it is long and
// folded.
func (a *App) promptSection(c *ui.Context, prompt string) {
	k, t := a.kit, a.kit.T
	s := &a.automations
	ui.Column(c).Gap(k.Px(8)).Children(func() {
		a.sectionTitle(c, L("Prompt"), "")
		long := longPrompt(prompt)
		ui.Column(c).Padding(k.Px(10), k.Px(12)).Radius(k.Px(8)).Background(t.Muted.Alpha(0.5)).Children(func() {
			text := k.Text(c, prompt, 13, 20).TextColor(t.Foreground)
			if long && !s.promptOpen {
				text.MaxLines(promptLines)
			}
		})
		if long {
			a.promptToggle(c)
		}
	})
}

// promptLines is how much of a long prompt an Automation's page shows
// until it is opened, so that its runs stay in view.
const promptLines = 4

// longPrompt is whether the prompt may need more than promptLines. Text
// reports no lines it cut, so this counts: 160 characters of Chinese fill
// four lines down to a 520px column.
func longPrompt(p string) bool {
	return strings.Count(p, "\n") >= promptLines || utf8.RuneCountInString(p) > 160
}

// promptToggle shows the whole prompt of the Automation, or its first lines.
func (a *App) promptToggle(c *ui.Context) {
	k, t := a.kit, a.kit.T
	s := &a.automations
	label := L("Show full prompt")
	if s.promptOpen {
		label = L("Show less")
	}
	b := ui.ButtonBase(c).Label(label).Expanded(s.promptOpen).AlignSelf(ui.Start).Height(k.Px(24)).Gap(k.Px(4)).PaddingX(k.Px(6)).
		Margin(-k.Px(4), 0, 0, -k.Px(6)).Radius(k.Px(8)).Cursor(ui.CursorPointer)
	color := t.MutedForeground
	if b.Hovered() {
		color = t.Foreground
	}
	b.Children(func() {
		k.Text(c, label, 12, 16).TextColor(color)
		rot := float32(90)
		if s.promptOpen {
			rot = -90
		}
		k.Icon(c, "chevron-right", 12, color).Rotate(rot)
	})
	if b.Clicked() {
		s.promptOpen = !s.promptOpen
	}
}

// setPaused pauses the Automation id, or resumes it.
func (a *App) setPaused(id string, pause bool) {
	action, done := l10n.N("resume"), L("Resumed. It runs on its schedule again.")
	if pause {
		action, done = l10n.N("pause"), L("Paused. Run now still starts a run.")
	}
	gw := a.cfg.Gateway
	a.act(id, action, done, func() error { return gw.PauseAutomation(a.ctx, id, pause) }, nil)
}

// deleteDialog asks before the Automation e goes.
func (a *App) deleteDialog(c *ui.Context, e *gateway.Automation) {
	s := &a.automations
	id, name := e.ID, trimOr(e.Name, e.ID)
	gw := a.cfg.Gateway
	a.confirmDialog(c, &s.confirmDelete, L("Delete “%s”?", name),
		L("It stops running and its folder in Droi’s data is removed. The sessions of its past runs stay."),
		func() ui.Element { return a.dangerButton(c, L("Delete"), s.acting != "") },
		func() {
			a.act("", l10n.N("delete"), L("Deleted “%s”.", name), func() error { return gw.DeleteAutomation(a.ctx, id) },
				func() { a.Go(Route{Name: "automations"}) })
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

func (a *App) runRow(c *ui.Context, r gateway.AutomationRun) {
	k, t := a.kit, a.kit.T
	started := automations.When(r.StartedAt, a.cfg.Now())
	status := automations.RunLabel(r.Status)
	if a.runLive(r.SessionID) {
		status = "Running"
	}
	label := l10n.T(status)
	waiting := a.automations.waiting[r.SessionID]
	if waiting {
		label = L("Waiting for your approval")
	}
	b := ui.ButtonBase(c.Key(r.RunID)).Transition(rowMotion).Role(ui.RoleListItem).Label(L("Open run from %s", started)).Height(k.Px(40)).Gap(k.Px(10)).
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
			switch {
			case waiting:
				k.Icon(c, "circle-alert", 14, t.Attention)
			case status == "Running":
				k.Spinner(c, 14, t.Info)
			case status == "Succeeded":
				k.Icon(c, "circle-check", 14, t.Success)
			case status == "Failed":
				k.Icon(c, "circle-x", 14, t.DestructiveForeground)
			case status == "Interrupted":
				k.Icon(c, "circle-slash", 14, t.MutedForeground)
			default:
				k.Icon(c, "circle-dashed", 14, t.MutedForeground)
			}
		})
		k.Text(c, label, 13, 20).SingleLine().Shrink(0)
		if r.Trigger == "catch-up" {
			k.Text(c, L("Catch-up"), 11, 16).TextColor(t.MutedForeground).SingleLine().Shrink(0).PaddingX(k.Px(6)).Radius(k.Px(4)).Background(t.Muted)
		}
		if r.Error != "" {
			k.Text(c, l10n.T(r.Error), 13, 20).TextColor(t.MutedForeground).SingleLine().Shrink(1).MinWidth(0).Tooltip(l10n.T(r.Error))
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
func (a *App) fillForm(e *gateway.Automation) {
	f := automationForm{repeat: automations.Daily, clock: "09:00", minute: "00"}
	if e != nil {
		// A schedule read in a zone of its own stays as it is written.
		sc := automations.Schedule{Repeat: automations.Custom, Custom: e.Schedule, Hour: 9}
		if e.Timezone == "" {
			sc = automations.Parse(e.Schedule, 0)
		}
		f.name, f.prompt, f.model, f.effort, f.autonomy, f.timezone = e.Name, e.Prompt, e.Model, e.ReasoningEffort, e.Autonomy, e.Timezone
		f.repeat, f.weekday, f.custom = sc.Repeat, sc.Weekday, sc.Custom
		f.clock, f.minute = automations.Clock(sc.Hour, sc.Minute), strconv.Itoa(sc.Minute)
		if len(f.minute) < 2 {
			f.minute = "0" + f.minute
		}
		f.workdir = e.WorkingDirectory
	}
	a.automations.form = f
}

// zone is the time zone the form's schedule is saved with: the
// Automation's own for a Custom one, else "" for the Host's computer's.
func (f *automationForm) zone() string {
	if f.repeat == automations.Custom {
		return f.timezone
	}
	return ""
}

// schedule is the form's schedule as five cron fields, read in its zone.
func (f *automationForm) schedule() (string, error) {
	s := automations.Schedule{Repeat: f.repeat, Weekday: f.weekday, Custom: f.custom}
	switch f.repeat {
	case automations.Hourly:
		m, err := strconv.Atoi(strings.TrimSpace(f.minute))
		if err != nil || m < 0 || m > 59 {
			return "", errors.New(L("Enter a minute between 0 and 59."))
		}
		s.Minute = m
	case automations.Daily, automations.Weekdays, automations.Weekly:
		h, m, ok := automations.ParseClock(f.clock)
		if !ok {
			return "", errors.New(L("Enter the time as hours and minutes, for example 09:30."))
		}
		s.Hour, s.Minute = h, m
	default:
		expr := strings.TrimSpace(f.custom)
		if expr == "" {
			return "", errors.New(L("Enter five cron fields, such as 0 9 * * 1-5."))
		}
		if _, err := automations.ParseCron(expr); err != nil {
			return "", errors.New(L("Cron expression not understood: %s. Use five fields, such as 0 9 * * 1-5.", err))
		}
	}
	return s.Expression(0), nil
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

// hostZone is the time zone of the Host's computer, which a schedule
// without one of its own is read in: as the Host answered it, else as it
// gave it with e; "" until known.
func (a *App) hostZone(e *gateway.Automation) string {
	if z := a.automations.zone; z != "" {
		return z
	}
	if e != nil && e.Timezone == "" {
		return e.Zone
	}
	return ""
}

// schedulePreview is the form's schedule in words, with the zone it is
// read in, or the help of a Custom one not yet understood.
func (a *App) schedulePreview(f *automationForm, hostZone string) string {
	own := f.zone()
	expr, err := f.schedule()
	if err != nil {
		switch {
		case f.repeat != automations.Custom:
			return ""
		case own != "":
			return L("Five cron fields, in the %s time zone, such as 0 9 * * 1-5.", own)
		case a.following():
			return L("Five cron fields, in %s’s time, such as 0 9 * * 1-5.", a.computerName())
		}
		return L("Five cron fields, in this computer’s time, such as 0 9 * * 1-5.")
	}
	words := automations.Describe(expr, 0)
	switch {
	case own != "":
		return L("%s, in the %s time zone.", words, own)
	case hostZone == "":
		return ""
	case a.following():
		return L("%s, in %s’s time (%s).", words, a.computerName(), hostZone)
	}
	return L("%s, in this computer’s time (%s).", words, hostZone)
}

// permissionOptions are the choices of what a run may do without asking.
func permissionOptions() []kit.Option {
	opts := []kit.Option{{Value: "", Label: L("Don’t ask")}}
	for _, l := range []string{"low", "medium", "high"} {
		opts = append(opts, kit.Option{Value: l, Label: l10n.T(models.AutonomyLabels[l])})
	}
	return opts
}

// automationEditor is the form of a new Automation (e nil) or of e.
func (a *App) automationEditor(c *ui.Context, e *gateway.Automation) {
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
	var choices []models.Choice
	if dv := a.sessionDefaults(); dv != nil {
		choices = dv.Models
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
	repeats := make([]kit.Option, len(automations.Repeats))
	for i, r := range automations.Repeats {
		repeats[i] = kit.Option{Value: string(r), Label: l10n.T(automations.RepeatLabels[r])}
	}
	permissionsHelp := L("Runs use every tool without asking.")
	if f.autonomy != "" {
		permissionsHelp = L("A run asks before tools above this level and waits until you answer in its session.")
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
		a.formField(c, L("Prompt"), L("Each run starts a new session with this prompt."), problem("prompt"), func() {
			border := t.Border
			if f.errOn == "prompt" {
				border = t.Destructive.Alpha(0.6)
			}
			ui.Row(c).Radius(k.Px(10)).Border(1, border).Background(t.Background).Children(func() {
				k.TextArea(c, &f.prompt, L("Automation prompt"), L("Summarize yesterday’s commits in this repository and list anything that looks risky."),
					kit.AreaStyle{Pad: [4]float32{8, 12, 8, 12}, Size: 14, Line: 20, MinLines: 5, Color: t.Foreground})
			})
		})
		a.formField(c, L("Schedule"), a.schedulePreview(f, a.hostZone(e)), problem("schedule"), func() {
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
					if f.timezone != "" {
						label := L("Use this computer’s time")
						if a.following() {
							label = L("Use %s’s time", a.computerName())
						}
						if a.smallButton(c, kit.Ghost, label, "", false).Height(k.Px(36)).Shrink(0).Clicked() {
							f.timezone = ""
						}
					}
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
		a.formField(c, L("Model and reasoning effort"), L("Runs use your default model and reasoning effort unless you pick others."), "", func() {
			ui.Row(c).Children(func() {
				settingsview.ModelEffortField(c, k, a.prefs, L("Automation model"), L("Your default model"), false, &f.modelOpen, &f.picker, choices, f.model, f.effort,
					func(id string) {
						f.model = id
						if !modelHasEffort(choices, id, f.effort) {
							f.effort = ""
						}
					}, func(e string) { f.effort = e })
			})
		})
		a.formField(c, L("Permissions"), permissionsHelp, "", func() {
			ui.Row(c).Children(func() {
				if next, ok := k.FieldSelect(c, L("Permissions"), f.autonomy, permissionOptions(), false); ok {
					f.autonomy = next
				}
			})
		})
		a.formField(c, L("Workspace"), L("Runs work in this folder, with its skills and settings. Leave it empty for the automation’s own folder in Droi’s data."), "", func() {
			ui.Row(c).Gap(k.Px(8)).Children(func() {
				a.field(c, &f.workdir, L("Automation workspace"), "/Users/you/project", true, false)
				if !a.following() && a.smallButton(c, kit.Outline, L("Choose…"), "folder-open", false).Height(k.Px(36)).Label(L("Choose a workspace")).Clicked() {
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

// modelHasEffort is whether the model id offers the reasoning effort.
func modelHasEffort(choices []models.Choice, id, effort string) bool {
	for _, m := range choices {
		if m.ID == id {
			return contains(m.ReasoningEfforts, effort)
		}
	}
	return false
}

// saveAutomation creates one from the form (e nil) or changes e, through
// the Gateway; the Host names a new one from its name.
func (a *App) saveAutomation(e *gateway.Automation) {
	s := &a.automations
	f := &s.form
	f.err, f.errOn = "", ""
	name, prompt, workdir := strings.TrimSpace(f.name), strings.TrimSpace(f.prompt), strings.TrimSpace(f.workdir)
	expr, err := f.schedule()
	switch {
	case name == "":
		f.err, f.errOn = L("Enter an automation name."), "name"
	case prompt == "":
		f.err, f.errOn = L("Enter a prompt for the automation to run."), "prompt"
	case err != nil:
		f.err, f.errOn = err.Error(), "schedule"
	}
	if f.err != "" {
		return
	}
	in := gateway.Automation{
		Name: name, Prompt: prompt, Schedule: expr, Timezone: f.zone(), Model: f.model, ReasoningEffort: f.effort,
		Autonomy: f.autonomy, WorkingDirectory: workdir,
	}
	gw := a.cfg.Gateway
	var saved gateway.Automation
	save := func() (err error) {
		saved, err = gw.SaveAutomation(a.ctx, in)
		return err
	}
	opened := func() {
		a.putAutomation(saved)
		a.Go(Route{Name: "automations", Automation: saved.ID})
	}
	if e != nil {
		in.ID, in.Paused = e.ID, e.Paused
		a.act(e.ID, l10n.N("save"), L("Saved."), save, opened)
		return
	}
	a.act("", l10n.N("create"), L("Created. It runs on its schedule, and Run now starts one at once."), save, opened)
}
