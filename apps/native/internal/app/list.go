package app

// The session list: its first page, the pages after it, and the copy a frame reads. It lives in its own
// file because it is the one part of the window that talks to the list rather than to a Session, and the
// next change to it swaps where the pages come from without touching the window.

import (
	"slices"
	"time"

	"github.com/kkkk2323/droid-sdk-go/controller"
	"github.com/kkkk2323/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/pkg/prefs"
	"github.com/kkkk2323/droi/apps/native/pkg/sessions"
)

type olderState int

const (
	olderUnread olderState = iota
	olderReading
	olderRead
)

// olderPages are the pages a read of the first page leaves: the cursor
// after it, and edge, its oldest Session's date in Unix seconds.
type olderPages struct {
	gen    uint64
	cursor float64
	edge   int64
}

// refreshList reads the first page of Sessions again and keeps the older
// ones read before. The first time, or once reloadList made them stale,
// it reads every page after it as well, which can take a while.
func (a *App) refreshList() {
	cl, err := a.ctl.Client()
	if err != nil {
		return
	}
	gen := a.startList()
	limit := float64(sessionPage)
	show := prefs.ShowArchived.Get(a.prefs)
	res, err := cl.ListAvailableSessions(a.ctx, protocol.ListAvailableSessionsParams{Limit: &limit, IncludeArchived: &show})
	if p := a.setList(gen, res, err); p != nil {
		a.readOlder(*p, show)
	}
}

// unlistedReads are the waits between reads of the list for a Session
// that works before the Daemon lists it, the last one repeated: the Daemon
// announces a run of an Automation before it is on disk.
var unlistedReads = []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 15 * time.Second}

// readUntilListed reads the list until it has the Session id, the Session
// stops working, or the connection is gone.
func (a *App) readUntilListed(id string) {
	defer func() {
		a.mu.Lock()
		delete(a.unlisted, id)
		a.mu.Unlock()
	}()
	for i := 0; ; i++ {
		if _, err := a.ctl.Client(); err != nil {
			return
		}
		a.refreshList()
		a.mu.Lock()
		done := a.busy[id] == "idle" || findSummary(a.listed, id) != nil
		a.mu.Unlock()
		if done {
			return
		}
		select {
		case <-a.ctx.Done():
			return
		case <-time.After(unlistedReads[min(i, len(unlistedReads)-1)]):
		}
	}
}

// reloadList reads every page again, for a change the first page may not
// show: an older Session archived or renamed, another connection.
func (a *App) reloadList() {
	a.mu.Lock()
	a.older = olderUnread
	a.olderGen++
	a.mu.Unlock()
	a.refreshList()
}

// startList counts a read of the first page, which setList then takes.
func (a *App) startList() uint64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.listGen++
	return a.listGen
}

// setList takes the first page of read gen, unless a newer read started,
// with the older Sessions kept. It returns the pages left to read, if
// they are to be read.
func (a *App) setList(gen uint64, res *protocol.ListAvailableSessionsResult, err error) *olderPages {
	a.mu.Lock()
	defer a.mu.Unlock()
	if gen != a.listGen {
		return nil
	}
	a.listDone = true
	if err != nil {
		a.listErr = err.Error()
		return nil
	}
	a.listErr = ""
	go a.redraw()
	page := summariesOf(res.Sessions)
	a.pageIDs = make(map[string]bool, len(page))
	for _, s := range page {
		a.pageIDs[s.SessionID] = true
	}
	a.listRev++
	if !res.HasMore || res.NextCursor == nil || len(res.Sessions) == 0 {
		a.listed = page
		a.older = olderRead
		a.olderGen++
		a.keepList()
		return nil
	}
	edge := int64(res.Sessions[0].UpdatedAt)
	for _, s := range res.Sessions {
		edge = min(edge, int64(s.UpdatedAt))
	}
	list := page
	for _, s := range a.listed {
		if !a.pageIDs[s.SessionID] && s.UpdatedAt <= edge {
			list = append(list, s)
		}
	}
	a.listed = list
	if a.older != olderUnread {
		return nil
	}
	a.older = olderReading
	a.olderGen++
	return &olderPages{gen: a.olderGen, cursor: *res.NextCursor, edge: edge}
}

// readOlder reads the pages p leaves, then puts them in place of the
// older Sessions kept until then.
func (a *App) readOlder(p olderPages, show bool) {
	failed := func() {
		a.mu.Lock()
		if a.olderGen == p.gen {
			a.older = olderUnread
		}
		a.mu.Unlock()
	}
	cl, err := a.ctl.Client()
	if err != nil {
		failed()
		return
	}
	var read []protocol.DaemonAvailableSessionInfo
	limit, cursor := float64(sessionPage), p.cursor
	for {
		res, err := cl.ListAvailableSessions(a.ctx, protocol.ListAvailableSessionsParams{Limit: &limit, IncludeArchived: &show, EndBefore: &cursor})
		if err != nil {
			failed()
			return
		}
		a.mu.Lock()
		stale := a.olderGen != p.gen
		a.mu.Unlock()
		if stale {
			return
		}
		read = append(read, res.Sessions...)
		if !res.HasMore || res.NextCursor == nil || *res.NextCursor == cursor {
			break
		}
		cursor = *res.NextCursor
	}
	older := summariesOf(read)
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.olderGen != p.gen {
		return
	}
	var list []sessions.Summary
	kept := map[string]bool{}
	for _, s := range a.listed {
		if a.pageIDs[s.SessionID] || s.UpdatedAt >= p.edge {
			list = append(list, s)
			kept[s.SessionID] = true
		}
	}
	for _, s := range older {
		if !kept[s.SessionID] {
			list = append(list, s)
		}
	}
	a.listed = list
	a.listRev++
	a.older = olderRead
	a.keepList()
	go a.redraw()
}

func summariesOf(list []protocol.DaemonAvailableSessionInfo) []sessions.Summary {
	out := make([]sessions.Summary, 0, len(list))
	for _, s := range list {
		tags := make([]sessions.Tag, len(s.Tags))
		for i, t := range s.Tags {
			tags[i] = sessions.Tag{Name: t.Name, Metadata: t.Metadata}
		}
		if sessions.IsDraft(tags) || sessions.IsMemory(tags) {
			continue
		}
		sum := sessions.Summary{
			SessionID:        s.SessionID,
			Title:            trimOr(s.Title, L("Untitled session")),
			Cwd:              s.Cwd,
			RepoRoot:         s.RepoRoot,
			UpdatedAt:        int64(s.UpdatedAt),
			ArchivedAt:       s.ArchivedAt,
			Tags:             tags,
			ParentID:         sessions.ContinuationParent(tags),
			CallingSessionID: s.CallingSessionID,
			CallingToolUseID: s.CallingToolUseID,
			OrganizationID:   s.OrganizationID,
		}
		if s.MessagesCount != nil {
			n := int(*s.MessagesCount)
			sum.MessagesCount = &n
		}
		if w := s.Worktree; w != nil && w.RemovedAt == "" && w.Path != "" {
			sum.Worktree = &sessions.Worktree{Path: w.Path, Branch: w.Branch, RepoRoot: w.RepoRoot, Lifecycle: string(w.Lifecycle)}
		}
		out = append(out, sum)
	}
	return out
}

// snapshot copies what other goroutines write, for one frame.
func (a *App) snapshot() (controller.Status, []sessions.Summary, map[string]string, string, bool, uint64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	busy := make(map[string]string, len(a.busy))
	for k, v := range a.busy {
		busy[k] = v
	}
	if a.listCopy == nil || a.listCopyRev != a.listRev {
		// Clipped, so that a reader's append makes a new array.
		a.listCopy, a.listCopyRev = slices.Clip(slices.Clone(a.listed)), a.listRev
	}
	return a.status, a.listCopy, busy, a.listErr, a.listDone, a.listRev
}
