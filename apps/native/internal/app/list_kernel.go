package app

// The session list on Kernel: conversation.list read whole, mapped to the rows the sidebar draws, and kept up to
// date by index.changed and conversation.busy without reading the list again.

import (
	"bytes"
	"context"
	"slices"
	"strings"

	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/prefs"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/sessions"
)

// maxListPages bounds a read of the whole list: a cursor that never ends must not keep the window reading.
const maxListPages = 50

// kernelPage is how many rows one conversation.list asks for: Kernel's own cap (limits.maxConversationsPerPage).
const kernelPage = 200

// kernelPages are Kernel's list, through conversation.list. Every read is the whole list, at once: Kernel lists the
// live conversations or the archived ones, never both, and a row comes back to the list where its last activity
// puts it (an unarchived one can be far down), so no part of an earlier read can stand for the rows a new one has
// not reached yet. Kernel answers from one SQL table, so the whole list is a few calls.
type kernelPages struct {
	a    *App
	c    *kernel.Client
	show bool
}

func (p *kernelPages) first(ctx context.Context) ([]sessions.Summary, int64, bool, error) {
	read, err := p.read(ctx, kernel.ListParams{Limit: kernelPage})
	if err != nil {
		return nil, 0, false, err
	}
	if p.show {
		archived := true
		more, err := p.read(ctx, kernel.ListParams{Limit: kernelPage, Archived: &archived})
		if err != nil {
			return nil, 0, false, err
		}
		// One archived between the two reads is in both; its index.changed puts the row it keeps right.
		live := make(map[kernel.ID]bool, len(read))
		for _, s := range read {
			live[s.ID] = true
		}
		for _, s := range more {
			if !live[s.ID] {
				read = append(read, s)
			}
		}
	}
	return p.a.kernelSummaries(read), 0, false, nil
}

// rest is never asked for: first reads the whole list.
func (p *kernelPages) rest(context.Context, func() bool) ([]sessions.Summary, error) { return nil, nil }

// read reads the pages of a list from params' cursor on, to the end.
func (p *kernelPages) read(ctx context.Context, params kernel.ListParams) ([]kernel.Summary, error) {
	var read []kernel.Summary
	for range maxListPages {
		res, err := p.c.List(ctx, params)
		if err != nil {
			return nil, err
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
		SessionID:   a.sessionIDOf(s.ID),
		Title:       trimOr(s.Title, L("Untitled session")),
		Cwd:         s.Workspace,
		RepoRoot:    s.RepoRoot,
		UpdatedAt:   max(s.LastActivityAt, s.CreatedAt) / 1000,
		ActivityAt:  max(s.LastActivityAt, s.CreatedAt),
		Kind:        s.Kind,
		Preview:     s.Preview,
		TitleSource: s.TitleSource,
	}
	if s.Archived {
		sum.ArchivedAt = "archived"
	}
	if s.ViewedAt != nil {
		sum.ViewedAt = *s.ViewedAt
	}
	switch s.Kind {
	case "scratch":
		sum.Tags = []sessions.Tag{{Name: sessions.ScratchTag}}
	case kernelAutomationKind:
		// A run of the Host's Automations (M4 4L): the Automations page and the sidebar's running runs know it by
		// the tag a Daemon run has. The Automation is named by the run's title; its id is the run's own, since
		// what the page reads of a running Automation is the Host's.
		sum.Tags = []sessions.Tag{{Name: sessions.DroiAutomationTag, Metadata: map[string]string{
			"automationId":   "run:" + sum.SessionID,
			"automationName": strings.TrimPrefix(s.Title, automationTitlePrefix),
		}}}
	}
	return sum
}

// kernelAutomationKind is the kind of an Automation run's conversation, and automationTitlePrefix starts its title.
const (
	kernelAutomationKind  = "automation"
	automationTitlePrefix = "[Automation] "
)

// indexEvent is an index.changed the window applied while a read of the list was out: the read's rows are from
// before it, so it is applied to them again when they land.
type indexEvent struct {
	seq uint64
	n   kernel.IndexChanged
}

// applyIndexChanged moves a row as index.changed says: a new title, archived or not, a later activity. A
// conversation the list does not have yet (made by another window, an Automation, or this one a moment ago) is
// read with the list, once.
func (a *App) applyIndexChanged(n kernel.IndexChanged) {
	show := prefs.ShowArchived.Get(a.prefs)
	a.mu.Lock()
	if len(a.reading) > 0 {
		a.indexSeq++
		a.indexLog = append(a.indexLog, indexEvent{seq: a.indexSeq, n: n})
	}
	read := a.applyIndexLocked(n, show)
	id := a.sessionIDOf(n.ID)
	s := findSummary(a.listed, id)
	moved := s != nil && s.Unread()
	a.mu.Unlock()
	if read {
		go a.refreshList()
	}
	if moved {
		// What arrives while the conversation is on screen is seen as it arrives.
		a.cfg.Update(func() {
			if a.route.Name == "session" && a.route.SessionID == id {
				a.viewed(id)
			}
		})
	}
	a.redraw()
}

// applyIndexLocked is applyIndexChanged with a.mu held. It reports whether the list is to be read for the row.
func (a *App) applyIndexLocked(n kernel.IndexChanged, show bool) bool {
	id := a.sessionIDOf(n.ID)
	hidden := n.Archived != nil && *n.Archived && !show
	i := slices.IndexFunc(a.listed, func(s sessions.Summary) bool { return s.SessionID == id })
	switch {
	case i < 0:
		if !a.listDone || hidden || a.asked[id] {
			return false
		}
		if a.asked == nil {
			a.asked = map[string]bool{}
		}
		a.asked[id] = true
		return true
	case hidden:
		a.listed = slices.Delete(slices.Clone(a.listed), i, i+1)
		a.listRev++
		// It was a list row: unarchived, it is one again, and the list is read for it then.
		delete(a.asked, id)
	default:
		list := slices.Clone(a.listed)
		s := &list[i]
		s.Title, s.TitleSource = L("Untitled session"), n.TitleSource
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
			s.ActivityAt = n.LastActivityAt
		}
		s.ViewedAt = 0
		if n.ViewedAt != nil {
			s.ViewedAt = *n.ViewedAt
		}
		a.listed = list
		a.listRev++
	}
	return false
}

// replayIndexLocked applies again, to rows a read of the list started at from just put in place, the index changes
// that arrived while it was out. It reports whether one of them is of a conversation the read did not have, for which
// the list is to be read again. a.mu is held.
func (a *App) replayIndexLocked(from uint64) bool {
	show := prefs.ShowArchived.Get(a.prefs)
	read := false
	for _, e := range a.indexLog {
		if e.seq > from && a.applyIndexLocked(e.n, show) {
			read = true
		}
	}
	return read
}
