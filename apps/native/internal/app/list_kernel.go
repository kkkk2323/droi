package app

// The session list on Kernel: conversation.list's pages, mapped to the rows the sidebar draws, and kept up to date
// by index.changed and conversation.busy without reading the list again.

import (
	"bytes"
	"context"
	"encoding/json"
	"slices"

	"github.com/kkkk2323/droi/apps/native/pkg/kernel"
	"github.com/kkkk2323/droi/apps/native/pkg/prefs"
	"github.com/kkkk2323/droi/apps/native/pkg/sessions"
)

// maxListPages bounds a read of the whole list: a cursor that never ends must not keep the window reading.
const maxListPages = 50

// kernelPages are Kernel's list, through conversation.list. Kernel lists the live conversations or the archived
// ones, never both, so with archived Sessions shown the pages after the first are the rest of the live ones and
// then every archived one.
type kernelPages struct {
	a      *App
	c      *kernel.Client
	show   bool
	cursor json.RawMessage
}

func (p *kernelPages) first(ctx context.Context) ([]sessions.Summary, int64, bool, error) {
	res, err := p.c.List(ctx, kernel.ListParams{Limit: sessionPage})
	if err != nil {
		return nil, 0, false, err
	}
	page := p.a.kernelSummaries(res.Items)
	more := len(res.Cursor) > 0 && len(res.Items) > 0
	if more {
		p.cursor = res.Cursor
	}
	if !more && !p.show {
		return page, 0, false, nil
	}
	var edge int64
	for i, s := range page {
		if i == 0 || s.UpdatedAt < edge {
			edge = s.UpdatedAt
		}
	}
	return page, edge, true, nil
}

func (p *kernelPages) rest(ctx context.Context, stale func() bool) ([]sessions.Summary, error) {
	var read []kernel.Summary
	if p.cursor != nil {
		items, err := p.read(ctx, kernel.ListParams{Limit: sessionPage, Cursor: p.cursor}, stale)
		if err != nil || stale() {
			return nil, err
		}
		read = append(read, items...)
	}
	if p.show {
		archived := true
		items, err := p.read(ctx, kernel.ListParams{Limit: sessionPage, Archived: &archived}, stale)
		if err != nil || stale() {
			return nil, err
		}
		read = append(read, items...)
	}
	return p.a.kernelSummaries(read), nil
}

// read reads the pages of a list from params' cursor on, to the end.
func (p *kernelPages) read(ctx context.Context, params kernel.ListParams, stale func() bool) ([]kernel.Summary, error) {
	var read []kernel.Summary
	for range maxListPages {
		res, err := p.c.List(ctx, params)
		if err != nil {
			return nil, err
		}
		if stale() {
			return nil, nil
		}
		read = append(read, res.Items...)
		if len(res.Cursor) == 0 || len(res.Items) == 0 || bytes.Equal(res.Cursor, params.Cursor) {
			break
		}
		params.Cursor = res.Cursor
	}
	return read, nil
}

// kernelSummaries are conversation.list's rows as the sidebar draws them. A memory conversation is not one: Memory
// keeps its own.
func (a *App) kernelSummaries(items []kernel.Summary) []sessions.Summary {
	out := make([]sessions.Summary, 0, len(items))
	for _, s := range items {
		if s.Kind == "memory" {
			continue
		}
		out = append(out, a.kernelSummary(s))
	}
	return out
}

func (a *App) kernelSummary(s kernel.Summary) sessions.Summary {
	sum := sessions.Summary{
		SessionID: a.sessionIDOf(s.ID),
		Title:     trimOr(s.Title, L("Untitled session")),
		Cwd:       s.Workspace,
		RepoRoot:  s.RepoRoot,
		UpdatedAt: max(s.LastActivityAt, s.CreatedAt) / 1000,
		Kind:      s.Kind,
		Preview:   s.Preview,
	}
	if s.Archived {
		sum.ArchivedAt = "archived"
	}
	if s.ViewedAt != nil {
		sum.ViewedAt = *s.ViewedAt
	}
	if s.Kind == "scratch" {
		sum.Tags = []sessions.Tag{{Name: sessions.ScratchTag}}
	}
	return sum
}

// applyIndexChanged moves a row as index.changed says: a new title, archived or not, a later activity. A
// conversation the list does not have yet (made by another window, an Automation, or this one a moment ago) is
// read with the list, once.
func (a *App) applyIndexChanged(n kernel.IndexChanged) {
	id := a.sessionIDOf(n.ID)
	show := prefs.ShowArchived.Get(a.prefs)
	hidden := n.Archived != nil && *n.Archived && !show
	a.mu.Lock()
	i := slices.IndexFunc(a.listed, func(s sessions.Summary) bool { return s.SessionID == id })
	read := false
	switch {
	case i < 0:
		read = a.listDone && !hidden && !a.asked[id]
		if read {
			if a.asked == nil {
				a.asked = map[string]bool{}
			}
			a.asked[id] = true
		}
	case hidden:
		a.listed = slices.Delete(slices.Clone(a.listed), i, i+1)
		a.listRev++
	default:
		list := slices.Clone(a.listed)
		s := &list[i]
		s.Title = L("Untitled session")
		if n.Title != nil {
			s.Title = trimOr(*n.Title, s.Title)
		}
		if n.Archived != nil {
			s.ArchivedAt = ""
			if *n.Archived {
				s.ArchivedAt = "archived"
			}
		}
		if n.LastActivityAt > 0 {
			s.UpdatedAt = n.LastActivityAt / 1000
		}
		a.listed = list
		a.listRev++
	}
	a.mu.Unlock()
	if read {
		go a.refreshList()
	}
	a.redraw()
}
