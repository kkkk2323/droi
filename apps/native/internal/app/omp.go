package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/gateway"
	"github.com/kkkk2323/droi/apps/native/pkg/host"
	"github.com/kkkk2323/droi/apps/native/pkg/omp"
	"github.com/kkkk2323/droi/apps/native/pkg/sessions"
)

// withOMP appends live and recent OMP Sessions to the sidebar list.
func (a *App) withOMP(listed []sessions.Summary, rev uint64) ([]sessions.Summary, uint64) {
	if a.cfg.Host == nil || a.following() {
		return listed, rev
	}
	mgr := a.cfg.Host.OMP()
	live := mgr.LiveSummaries()
	if len(live) == 0 {
		return listed, rev
	}
	seen := map[string]bool{}
	for _, s := range listed {
		seen[s.SessionID] = true
	}
	out := listed
	for _, s := range live {
		if seen[s.SessionID] {
			continue
		}
		out = append(out, s)
		seen[s.SessionID] = true
	}
	if len(out) == len(listed) {
		return listed, rev
	}
	return out, rev + 1
}

// isOMPSession reports that the open Session should use the OMP page.
func (a *App) isOMPSession(id string, listed []sessions.Summary) bool {
	if a.cfg.Host != nil && a.cfg.Host.OMP().Get(id) != nil {
		return true
	}
	if s := findSummary(listed, id); s != nil && host.IsOMP(*s) {
		return true
	}
	return strings.HasPrefix(id, "omp:")
}

// startOMPSession creates an OMP child for workspace and opens it.
func (a *App) startOMPSession(workspace, model, text string) {
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

	if a.cfg.Host == nil {
		fail(L("OMP needs this computer's Host."))
		return
	}
	if a.cfg.Host.LocateOMP() == "" {
		fail(L("omp was not found. Install oh-my-pi, or set its path in Settings."))
		return
	}
	cwd := workspace
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
		cwd = dir
	}
	if cwd == "" {
		fail(L("Pick a project first."))
		return
	}
	ctx, cancel := context.WithTimeout(a.ctx, 30*time.Second)
	defer cancel()
	handle, err := a.cfg.Host.OMP().Create(ctx, host.OMPCreate{
		Cwd:          cwd,
		Model:        model,
		ApprovalMode: omp.ApprovalAlwaysAsk,
		Title:        ompTitle(text),
	})
	if err != nil {
		fail(err.Error())
		return
	}
	id := handle.ID
	if strings.TrimSpace(text) != "" {
		go func() {
			pctx, pcancel := context.WithTimeout(a.ctx, 30*time.Second)
			defer pcancel()
			if _, err := handle.Session.Prompt(pctx, text, nil); err != nil {
				a.cfg.Update(func() {
					if v := a.ompViews[id]; v != nil {
						v.err = err.Error()
					}
				})
			}
		}()
	}
	s.mu.Lock()
	s.creating = false
	s.mu.Unlock()
	a.cfg.Update(func() {
		s.text, s.images, s.pasted, s.bitmaps = "", nil, nil, nil
		a.Go(Route{Name: "session", SessionID: id})
	})
}

func ompTitle(s string) string {
	s = firstLine(s)
	if len(s) > 60 {
		return s[:60]
	}
	return s
}
