package llmusage

import (
	"context"
	"sync"

	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/diag"
)

// PageLoader loads one page of the request history; an empty cursor is
// the newest page.
type PageLoader func(ctx context.Context, cursor string) (diag.RequestPage, error)

// History holds just the page shown and a small stack of cursors, not the
// whole history. Older pages stay as they are while new requests arrive.
type History struct {
	load PageLoader
	// OnChange, when set, is called after every change: a load's start and
	// its end.
	OnChange func()

	mu         sync.Mutex
	records    []diag.RequestRecord
	nextCursor string
	loading    bool
	err        string
	// cursors[0] is always "" (the newest page); len(cursors) is the page
	// number.
	cursors []string
	version int
}

func NewHistory(load PageLoader) *History {
	return &History{load: load, cursors: []string{""}}
}

// HistoryState is a consistent copy of the history.
type HistoryState struct {
	Records    []diag.RequestRecord
	NextCursor string
	IsLoading  bool
	Error      string
	PageNumber int
	IsLatest   bool
	// Version counts the changes, so a copy can tell it is stale.
	Version int
}

func (h *History) State() HistoryState {
	h.mu.Lock()
	defer h.mu.Unlock()
	return HistoryState{
		Records:    append([]diag.RequestRecord(nil), h.records...),
		NextCursor: h.nextCursor,
		IsLoading:  h.loading,
		Error:      h.err,
		PageNumber: len(h.cursors),
		IsLatest:   len(h.cursors) == 1,
		Version:    h.version,
	}
}

// RefreshLatest reloads the newest page; older pages never refresh by
// themselves.
func (h *History) RefreshLatest(ctx context.Context) {
	h.loadWith(ctx, func(c []string) ([]string, bool) { return c, len(c) == 1 })
}

func (h *History) Reload(ctx context.Context) {
	h.loadWith(ctx, func(c []string) ([]string, bool) { return c, true })
}

func (h *History) Next(ctx context.Context) {
	h.mu.Lock()
	next := h.nextCursor
	h.mu.Unlock()
	if next == "" {
		return
	}
	h.loadWith(ctx, func(c []string) ([]string, bool) {
		return append(append([]string(nil), c...), next), true
	})
}

func (h *History) Previous(ctx context.Context) {
	h.loadWith(ctx, func(c []string) ([]string, bool) {
		if len(c) == 1 {
			return nil, false
		}
		return append([]string(nil), c[:len(c)-1]...), true
	})
}

func (h *History) Latest(ctx context.Context) {
	h.loadWith(ctx, func([]string) ([]string, bool) { return []string{""}, true })
}

// loadWith computes the cursor stack to show from the current one and
// loads its last page. A load while one runs is dropped; a failure keeps
// the page shown.
func (h *History) loadWith(ctx context.Context, target func([]string) ([]string, bool)) {
	h.mu.Lock()
	if h.loading {
		h.mu.Unlock()
		return
	}
	cursors, ok := target(h.cursors)
	if !ok {
		h.mu.Unlock()
		return
	}
	h.loading = true
	h.version++
	h.mu.Unlock()
	h.changed()

	page, err := h.load(ctx, cursors[len(cursors)-1])

	h.mu.Lock()
	h.loading = false
	h.version++
	switch {
	case err != nil:
		if ctx.Err() == nil {
			h.err = err.Error()
		}
	case ctx.Err() == nil:
		h.records = page.Requests
		h.nextCursor = page.NextCursor
		h.cursors = cursors
		h.err = ""
	}
	h.mu.Unlock()
	h.changed()
}

func (h *History) changed() {
	if h.OnChange != nil {
		h.OnChange()
	}
}
