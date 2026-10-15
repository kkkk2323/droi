package app

import (
	"encoding/json"
	"errors"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/kkkk2323/droid-sdk-go/protocol"
	"github.com/kkkk2323/droid-sdk-go/session"

	"github.com/kkkk2323/droi/apps/native/internal/loops"
)

// loopCommand is `/loop <interval> <prompt>`, `/loop <prompt> every
// <interval>` or `/loop stop`, which the composer runs itself.
var loopCommand = regexp.MustCompile(`^/loop(?:\s+([\s\S]*))?$`)

// loopsState is the Daemon's loops, as daemon.list_crons and
// daemon.cron.state_changed tell them. Guarded by App.mu.
type loopsState struct {
	byID map[string]loops.Loop
	// kept are the Sessions loaded without the Daemon's inactivity timeout
	// for their loops: a loop fires only while its Session has a worker,
	// and the Daemon closes an idle one after 30 minutes.
	kept map[string]bool
}

// readLoops reads every loop once connected, and keeps the worker of each
// Session with one that runs or waits to run. Loading a Session also
// resumes the loops the Daemon held when it started, or when the worker
// was closed. A Session kept before whose loops ended while the window was
// not connected gets the timeout back.
func (a *App) readLoops() {
	cl, err := a.ctl.Client()
	if err != nil {
		return
	}
	res, err := cl.ListCrons(a.ctx, protocol.ListCronsParams{})
	if err != nil {
		return
	}
	byID := map[string]loops.Loop{}
	for _, r := range res.Crons {
		if l, ok := loops.FromRecord(r); ok {
			byID[l.ID] = l
		}
	}
	a.mu.Lock()
	sessions := map[string]bool{}
	for _, l := range a.loops.byID {
		sessions[l.SessionID] = true
	}
	for _, l := range byID {
		sessions[l.SessionID] = true
	}
	a.loops.byID = byID
	// A new connection may be to a new Daemon, which has no worker yet.
	wasKept := a.loops.kept
	a.loops.kept = nil
	a.mu.Unlock()
	a.keepWorkers(sessions)
	a.mu.Lock()
	var release []string
	for id := range wasKept {
		if !a.loops.kept[id] {
			release = append(release, id)
		}
	}
	a.mu.Unlock()
	for _, id := range release {
		go a.keepWorker(id, false)
	}
	a.redraw()
}

// loopsChanged takes a daemon.cron.state_changed.
func (a *App) loopsChanged(raw json.RawMessage) {
	var p protocol.CronStateChangedParams
	if jsonUnmarshal(raw, &p) != nil {
		return
	}
	sessions := map[string]bool{}
	a.mu.Lock()
	if a.loops.byID == nil {
		a.loops.byID = map[string]loops.Loop{}
	}
	for _, id := range p.CronIDs {
		if l, ok := a.loops.byID[id]; ok {
			sessions[l.SessionID] = true
		}
		if p.Reason == protocol.CronStateChangedParamsReasonDeleted {
			delete(a.loops.byID, id)
		}
	}
	a.mu.Unlock()
	if p.Reason != protocol.CronStateChangedParamsReasonDeleted {
		for id := range a.takeRecords(p.Crons...) {
			sessions[id] = true
		}
	}
	a.keepWorkers(sessions)
}

// takeRecords puts records in place of the loops of their ids, and returns
// the Sessions they are of.
func (a *App) takeRecords(records ...protocol.CronRecord) map[string]bool {
	sessions := map[string]bool{}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.loops.byID == nil {
		a.loops.byID = map[string]loops.Loop{}
	}
	for _, r := range records {
		if l, ok := loops.FromRecord(r); ok {
			a.loops.byID[l.ID] = l
			sessions[l.SessionID] = true
			continue
		}
		var head struct {
			ID string `json:"id"`
		}
		if jsonUnmarshal(r.Raw, &head) == nil {
			if l, ok := a.loops.byID[head.ID]; ok {
				sessions[l.SessionID] = true
				delete(a.loops.byID, head.ID)
			}
		}
	}
	go a.redraw()
	return sessions
}

// keepWorkers loads each of sessions that has a loop to run without the
// Daemon's inactivity timeout, again when one of its loops is held, and
// with it once none is left to run, so that its worker can close again.
// Another computer's Daemon outlives this window, which would then never
// give the timeout back: there a Session with a held loop is only loaded,
// which resumes it, as droid keeps the timeout for a remote Session.
func (a *App) keepWorkers(sessions map[string]bool) {
	remote := a.following()
	for id := range sessions {
		live, held := false, false
		a.mu.Lock()
		for _, l := range a.loops.byID {
			if l.SessionID == id {
				live = live || l.Live()
				held = held || l.Status == protocol.CronStatusHeld
			}
		}
		keep := live && !remote
		kept := a.loops.kept[id]
		switch {
		case keep && (!kept || held):
			if a.loops.kept == nil {
				a.loops.kept = map[string]bool{}
			}
			a.loops.kept[id] = true
		case !keep && kept:
			delete(a.loops.kept, id)
		case remote && held:
		default:
			a.mu.Unlock()
			continue
		}
		a.mu.Unlock()
		go a.keepWorker(id, keep)
	}
}

func (a *App) keepWorker(id string, keep bool) {
	limit := int64(LoadedMessageLimit)
	_, err := a.ctl.LoadSession(a.ctx, protocol.LoadSessionParams{SessionID: id, MessageLimit: &limit, DisableInactivityTimeout: &keep})
	if err != nil && keep {
		a.mu.Lock()
		delete(a.loops.kept, id)
		a.mu.Unlock()
	}
}

// sessionLoops are the loops of Session id, as they were made.
func (a *App) sessionLoops(id string) []loops.Loop {
	a.mu.Lock()
	var out []loops.Loop
	for _, l := range a.loops.byID {
		if l.SessionID == id {
			out = append(out, l)
		}
	}
	a.mu.Unlock()
	loops.Sort(out)
	return out
}

// loopErr is what the composer says of a `/loop` it cannot run.
func loopErr(err error) string {
	switch {
	case errors.Is(err, loops.ErrTooShort):
		return L("A loop can run at most once a minute.")
	case errors.Is(err, loops.ErrTooLong):
		return L("A loop has to run at least once a day.")
	case errors.Is(err, loops.ErrUneven):
		return L("A loop runs every 1, 2, 3, 4, 5, 6, 10, 12, 15, 20 or 30 minutes, or every 1, 2, 3, 4, 6, 8, 12 or 24 hours.")
	}
	return L("Give the loop an interval: /loop 10m check the build, or /loop check the build every 2h.")
}

// loop runs `/loop args`, true once it started: a loop of this Session, or
// `stop`, which deletes them.
func (v *sessionView) loop(s *session.Session, args string) bool {
	a := v.a
	cmd, err := loops.ParseCommand(args)
	if err != nil {
		v.SetErr(loopErr(err))
		return false
	}
	if cmd.Stop {
		ls := a.sessionLoops(v.id)
		if len(ls) == 0 {
			v.SetErr(L("This session has no loops."))
			return false
		}
		v.SetErr("")
		for _, l := range ls {
			go v.deleteLoop(l)
		}
		return true
	}
	cwd := s.Cwd()
	if cwd == "" {
		a.mu.Lock()
		if sum := findSummary(a.listed, v.id); sum != nil {
			cwd = sum.Cwd
		}
		a.mu.Unlock()
	}
	prompt := cmd.Prompt
	if prompt == "" {
		prompt = loops.BuiltInPrompt
		// Another computer's files are not on this one.
		if !a.following() {
			home, _ := os.UserHomeDir()
			prompt = loops.DefaultPrompt(cwd, home)
		}
	}
	v.SetErr("")
	yes := true
	params, err := protocol.NewDaemonCreateCronRequestParams(protocol.DaemonCreateCronRequestParamsSessionPrompt{
		Kind:           protocol.DaemonCreateCronRequestParamsKindSessionPrompt,
		Source:         protocol.CronSourceLoopCommand,
		Schedule:       protocol.DaemonCreateCronRequestParamsSessionPromptSchedule{Expression: loops.Cron(cmd.Interval, a.cfg.Now()), Recurring: true},
		RunImmediately: &yes,
		RunPolicy:      &protocol.DaemonCreateCronRequestParamsSessionPromptRunPolicy{WhenSessionInactive: "hold"},
		Scope:          protocol.CronCreateSessionScope{Type: "session", SessionID: v.id, SessionCwd: cwd},
		Payload:        protocol.SameSessionPromptPayload{Type: "prompt", Prompt: prompt, Target: protocol.SameSessionPromptPayloadTarget{Type: "same_session"}},
	})
	if err != nil {
		v.SetErr(err.Error())
		return false
	}
	go func() {
		cl, err := a.ctl.Client()
		var res *protocol.CreateCronResult
		if err == nil {
			res, err = cl.CreateCron(a.ctx, params)
		}
		if err != nil {
			v.Fail(err)
			return
		}
		a.keepWorkers(a.takeRecords(res.Cron))
	}()
	return true
}

// pauseLoop pauses a loop, or lets a paused one run again.
func (v *sessionView) pauseLoop(l loops.Loop, pause bool) {
	a := v.a
	status := protocol.UserUpdatableCronStatusActive
	if pause {
		status = protocol.UserUpdatableCronStatusPaused
	}
	cl, err := a.ctl.Client()
	var res *protocol.UpdateCronResult
	if err == nil {
		res, err = cl.UpdateCron(a.ctx, protocol.UpdateCronParams{CronID: l.ID, Status: status})
	}
	if err != nil {
		v.Fail(err)
		return
	}
	if res.Cron != nil {
		a.keepWorkers(a.takeRecords(*res.Cron))
	}
}

func (v *sessionView) deleteLoop(l loops.Loop) {
	a := v.a
	cl, err := a.ctl.Client()
	if err == nil {
		_, err = cl.DeleteCron(a.ctx, protocol.DeleteCronParams{CronID: l.ID, SessionID: l.SessionID})
	}
	if err != nil {
		v.Fail(err)
		return
	}
	a.mu.Lock()
	delete(a.loops.byID, l.ID)
	a.mu.Unlock()
	a.keepWorkers(map[string]bool{l.SessionID: true})
	a.redraw()
}

// loopCadence is how often a loop runs.
func loopCadence(expression string) string {
	d, hour, minute, ok := loops.Interval(expression)
	switch {
	case !ok:
		return L("On a custom schedule")
	case d == 24*time.Hour:
		at := time.Date(2000, 1, 1, hour, minute, 0, 0, time.UTC).Local()
		return L("Daily at %s", at.Format("15:04"))
	case d == time.Hour:
		return L("Every hour")
	case d%time.Hour == 0:
		return L("Every %d hours", int(d/time.Hour))
	case d == time.Minute:
		return L("Every minute")
	}
	return L("Every %d minutes", int(d/time.Minute))
}

// loopStatus is where a loop is: its next run, or why it does not run.
func loopStatus(l loops.Loop, now time.Time) string {
	switch l.Status {
	case protocol.CronStatusRunning:
		return L("Running")
	case protocol.CronStatusPaused:
		return L("Paused")
	case protocol.CronStatusHeld:
		return L("Waiting for the session")
	case protocol.CronStatusError:
		return L("Failed")
	}
	if l.NextRunAt.After(now) {
		return L("Next at %s", l.NextRunAt.Local().Format("15:04"))
	}
	return ""
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i]) + "…"
	}
	return s
}

// loopList is the Session's loops, in the shelf: up to two as a list, more
// folded behind the first.
func (v *sessionView) loopList(c *ui.Context, ls []loops.Loop) {
	a := v.a
	k, t := a.kit, a.kit.T
	now := a.cfg.Now()
	info := func(l loops.Loop) {
		text := loopCadence(l.Expression)
		if st := loopStatus(l, now); st != "" {
			text += " · " + st
		}
		color := t.MutedForeground
		if l.Status == protocol.CronStatusError {
			color = t.DestructiveForeground
		}
		el := k.Text(c, text, 11, 16.5).TextColor(color).FontFeatures("tnum").SingleLine().Shrink(0)
		if l.Status == protocol.CronStatusError && l.LastError != "" {
			el.Tooltip(l.LastError)
		}
	}
	list := func() {
		ui.Column(c).Role(ui.RoleList).Label(L("Loops")).Children(func() {
			for _, l := range ls {
				ui.Row(c.Key(l.ID)).Height(k.Px(30)).Gap(k.Px(8)).Padding(0, k.Px(6), 0, k.Px(12)).Children(func() {
					k.Icon(c, "repeat", 12, t.MutedForeground)
					k.Text(c, firstLine(l.Prompt), 12.5, 18.75).TextColor(t.Foreground).Tooltip(l.Prompt).SingleLine().Selectable().Grow(1).Shrink(1).MinWidth(0)
					info(l)
					if l.Status == protocol.CronStatusPaused {
						if k.IconButton(c, "play", L("Resume loop"), 24).Clicked() {
							go v.pauseLoop(l, false)
						}
					} else if k.IconButton(c, "pause", L("Pause loop"), 24).Clicked() {
						go v.pauseLoop(l, true)
					}
					if k.IconButton(c, "x", L("Delete loop"), 24).Clicked() {
						go v.deleteLoop(l)
					}
				})
			}
		})
	}
	ui.Column(c).PaddingY(k.Px(4)).Children(func() {
		if len(ls) <= 2 {
			list()
			return
		}
		open := v.Flag("loops", false)
		p := ui.CollapsibleBase(c, open)
		p.Trigger.Label(L("Loops, %d", len(ls))).FillWidth().Height(k.Px(30)).Gap(k.Px(8)).
			Padding(0, k.Px(6), 0, k.Px(12)).Cursor(ui.CursorPointer).Children(func() {
			k.Icon(c, "repeat", 12, t.MutedForeground)
			if *open {
				k.Text(c, L("Loops"), 12.5, 18.75).TextColor(t.MutedForeground).Grow(1)
			} else {
				k.Text(c, firstLine(ls[0].Prompt), 12.5, 18.75).TextColor(t.Foreground).SingleLine().Grow(1).Shrink(1).MinWidth(0)
			}
			k.Text(c, L("%d loops", len(ls)), 11, 16.5).TextColor(t.MutedForeground).FontFeatures("tnum").Shrink(0)
			ui.Box(c).Size(k.Px(24), k.Px(24)).Center().Shrink(0).Children(func() {
				k.Icon(c, "chevron-down", 12, t.MutedForeground).Rotate(180 * p.Progress())
			})
		})
		p.Panel(list)
	})
}
