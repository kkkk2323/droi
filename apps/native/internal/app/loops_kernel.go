package app

import (
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/kkkk2323/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/internal/loops"
	"github.com/kkkk2323/droi/apps/native/pkg/gateway"
)

// On the Kernel a loop is one of the Host's Automations that posts into its conversation (decision Q6): `/loop`
// saves one and runs it at once, as droid's runs the first time at once, and the shelf lists the conversation's
// from the Host's Automations. The Daemon's loops (crons) stay for the Daemon's Sessions.

// loopNameRunes is how much of a loop's prompt names its Automation.
const loopNameRunes = 60

// loopAutomations are the Host's Automations as the shelf reads them: the list alone, without what the Automations
// page also reads (Factory's, the zone). Main thread only.
type loopAutomations struct {
	list    []gateway.Automation
	at      time.Time
	loading bool
	// again is a forced read asked for while one was under way.
	again bool
}

// kernelLoopsOf is the conversation whose loops are Automations of the Host: Session id's, when Kernel holds it and
// the window has the Host's Automations.
func (a *App) kernelLoopsOf(id string) (string, bool) {
	if a.cfg.Gateway == nil {
		return "", false
	}
	conv, ok := a.kernelID(id)
	if !ok {
		return "", false
	}
	return conv.String(), true
}

// loadLoopAutomations reads the Host's Automations for the shelf when they are old, or at once when forced. Main
// thread only.
func (a *App) loadLoopAutomations(force bool) {
	s := &a.loopAutos
	if s.loading {
		s.again = s.again || force
		return
	}
	gw := a.cfg.Gateway
	if gw == nil || !force && !s.at.IsZero() && a.cfg.Now().Sub(s.at) < automationsRefresh {
		return
	}
	s.loading = true
	go func() {
		list, err := gw.Automations(a.ctx)
		a.cfg.Update(func() {
			s.loading, s.at = false, a.cfg.Now()
			if err != nil {
				// Soon again: the Host may only be starting.
				s.at = s.at.Add(5*time.Second - automationsRefresh)
			} else {
				s.list = list
			}
			if s.again {
				s.again = false
				a.loadLoopAutomations(true)
			}
		})
	}()
}

// kernelLoops are Session id's loops, from the Host's Automations, read again when they are old. Main thread only.
func (a *App) kernelLoops(id, conv string) []loops.Loop {
	a.loadLoopAutomations(false)
	var out []loops.Loop
	for _, e := range a.loopAutos.list {
		if e.Conversation == conv {
			out = append(out, automationLoop(id, e))
		}
	}
	loops.Sort(out)
	return out
}

// automationLoop is the loop of Session id an Automation is, as the shelf shows a loop.
func automationLoop(id string, e gateway.Automation) loops.Loop {
	l := loops.Loop{ID: e.ID, SessionID: id, Prompt: e.Prompt, Expression: utcExpression(e.Schedule, e.Zone), Status: protocol.CronStatusActive,
		CreatedAt: e.CreatedAt, Automation: true}
	switch {
	case e.Running:
		l.Status = protocol.CronStatusRunning
	case e.Paused:
		l.Status = protocol.CronStatusPaused
	case e.Invalid != "":
		l.Status, l.LastError = protocol.CronStatusError, e.Invalid
	case e.LastRun != nil && e.LastRun.Status == "failed":
		l.Status, l.LastError = protocol.CronStatusError, e.LastRun.Error
	}
	if t, err := time.Parse(time.RFC3339, e.NextRunAt); err == nil {
		l.NextRunAt = t
	}
	return l
}

// utcExpression is a daily expression read in zone as the shelf reads one, in UTC: the time of day is today's in
// zone. Any other expression says the same cadence in any zone.
func utcExpression(expr, zone string) string {
	if zone == "" || zone == "UTC" {
		return expr
	}
	d, hour, minute, ok := loops.Interval(expr)
	if !ok || d != 24*time.Hour {
		return expr
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return expr
	}
	now := time.Now().In(loc)
	at := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, loc).UTC()
	return fmt.Sprintf("%d %d * * *", at.Minute(), at.Hour())
}

// loopName is the name of a loop's Automation: its prompt's first line, cut short.
func loopName(prompt string) string {
	name := firstLine(prompt)
	if utf8.RuneCountInString(name) > loopNameRunes {
		name = string([]rune(name)[:loopNameRunes]) + "…"
	}
	return L("Loop: %s", name)
}

// startKernelLoop saves the loop as an Automation of the Host that posts into conv, then runs it once.
func (v *sessionView) startKernelLoop(conv string, cmd loops.Command, prompt, cwd string) {
	a := v.a
	gw := a.cfg.Gateway
	saved, err := gw.SaveAutomation(a.ctx, gateway.Automation{
		Name:     loopName(prompt),
		Prompt:   prompt,
		Schedule: loops.Cron(cmd.Interval, a.cfg.Now()),
		// The expression is in UTC, as loops.Cron writes it and the shelf reads it.
		Timezone:         "UTC",
		WorkingDirectory: cwd,
		Conversation:     conv,
	})
	if err != nil {
		v.Fail(err)
		return
	}
	if _, err := gw.RunAutomation(a.ctx, saved.ID); err != nil && !errors.Is(err, gateway.ErrAutomationRunning) {
		v.Fail(err)
	}
	a.cfg.Update(func() {
		s := &a.loopAutos
		s.list = append(s.list, saved)
		a.loadLoopAutomations(true)
	})
}

// pauseKernelLoop pauses a loop's Automation, or lets a paused one run again.
func (v *sessionView) pauseKernelLoop(l loops.Loop, pause bool) {
	a := v.a
	if err := a.cfg.Gateway.PauseAutomation(a.ctx, l.ID, pause); err != nil {
		v.Fail(err)
	}
	a.cfg.Update(func() { a.loadLoopAutomations(true) })
}

// deleteKernelLoop deletes a loop's Automation.
func (v *sessionView) deleteKernelLoop(l loops.Loop) {
	a := v.a
	if err := a.cfg.Gateway.DeleteAutomation(a.ctx, l.ID); err != nil && !errors.Is(err, gateway.ErrAutomationNotFound) {
		v.Fail(err)
	}
	a.cfg.Update(func() {
		s := &a.loopAutos
		for i := range s.list {
			if s.list[i].ID == l.ID {
				s.list = append(s.list[:i:i], s.list[i+1:]...)
				break
			}
		}
		a.loadLoopAutomations(true)
	})
}
