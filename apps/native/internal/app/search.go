package app

import (
	"cmp"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/internal/host"
	"github.com/kkkk2323/droi/apps/native/internal/sessionfile"
	"github.com/kkkk2323/droi/apps/native/internal/sessions"
)

// Fewer characters and the Daemon scans thousands of files for nothing much.
const (
	searchMinLength = 2
	searchLimit     = 20
	searchDebounce  = 250 * time.Millisecond
	// searchRecentWindow is the span searched first; see search.
	searchRecentWindow = 30 * 24 * time.Hour
)

type searchHit struct {
	sessionID string
	title     string
	updatedAt int64
	snippet   []snippetRun
}

type snippetRun struct {
	text  string
	match bool
}

// searchState is the sidebar's search, which the Daemon does
// (daemon.search_sessions) once typing settles.
type searchState struct {
	mu      sync.Mutex
	query   string // the query the hits are for
	hits    []searchHit
	err     string
	asked   string // the query sent last
	timer   *time.Timer
	pending bool
}

func (a *App) searchBox(c *ui.Context) {
	k, t := a.kit, a.kit.T
	box := ui.Row(c).Grow(1).MinWidth(0).Height(k.Px(32))
	box.Children(func() {
		in := ui.TextInputBase(c, &a.sidebar.query).Label(L("Search sessions")).Placeholder(L("Search sessions")).
			Fill().Radius(k.Px(10)).Padding(0, k.Px(28), 0, k.Px(32)).FontSize(k.Px(13)).TextColor(t.Foreground).Border(1, ui.Transparent)
		if in.Focused() {
			in.Background(t.Background).Border(1, t.Ring.Alpha(0.6))
		} else {
			in.Background(t.SidebarAccent.Alpha(0.5))
		}
		if in.Changed() {
			a.search(a.sidebar.query)
		}
		if a.sidebar.query != "" && in.Shortcut(0, ui.KeyEscape) {
			a.sidebar.query = ""
		}
		k.Icon(c, "search", 16, t.MutedForeground).Absolute().Left(k.Px(8)).Top(k.Px(8)).PassThrough()
		if a.sidebar.query != "" {
			x := k.IconButton(c, "x", L("Clear search"), 24).Absolute().Right(k.Px(4)).Top(k.Px(4))
			if x.Clicked() {
				a.sidebar.query = ""
			}
		}
	})
}

// search asks the Daemon once the query stops changing for a moment.
func (a *App) search(query string) {
	s := &a.sidebar.search
	q := strings.TrimSpace(query)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.timer != nil {
		s.timer.Stop()
	}
	if len([]rune(q)) < searchMinLength || a.ctl == nil {
		return
	}
	s.pending = true
	s.timer = time.AfterFunc(searchDebounce, func() {
		cl, err := a.ctl.Client()
		if err != nil {
			return
		}
		_, listed, _, _, _, _ := a.snapshot()
		hits := titleHits(listed, q)
		// publish reports whether q is still the query to answer.
		publish := func(done bool, err error) bool {
			s.mu.Lock()
			current := s.asked == q
			if current {
				s.pending = !done
				s.query = q
				s.hits = hits
				s.err = ""
				if err != nil && len(hits) == 0 {
					s.err = err.Error()
				}
			}
			s.mu.Unlock()
			if current {
				a.redraw()
			}
			return current
		}
		s.mu.Lock()
		s.asked = q
		s.mu.Unlock()
		if !publish(false, nil) {
			return
		}
		limit, perSession, context := float64(searchLimit), 1.0, 80.0
		ask := func(after *float64) ([]searchHit, error) {
			res, err := cl.SearchSessions(a.ctx, protocol.SearchSessionsParams{Query: q, Kind: "message_text", LimitSessions: &limit,
				LimitHitsPerSession: &perSession, ContextChars: &context, UpdatedAfterMs: after})
			if err != nil {
				return nil, err
			}
			return contentHits(res), nil
		}
		// The Daemon reads only its best few hundred candidates, and with thousands
		// of Sessions a short query ranks them all alike, so a search of all of them
		// misses most. The last month is few enough to be read whole.
		after := float64(a.cfg.Now().Add(-searchRecentWindow).UnixMilli())
		recent, err := ask(&after)
		hits = mergeHits(hits, recent)
		if err != nil || len(hits) >= searchLimit {
			publish(true, err)
			return
		}
		if !publish(false, nil) {
			return
		}
		older, err := ask(nil)
		hits = mergeHits(hits, older)
		publish(true, err)
	})
}

// titleHits are the listed Sessions whose title has the query, newest first.
func titleHits(listed []sessions.Summary, query string) []searchHit {
	q := strings.ToLower(query)
	var found []sessions.Summary
	for _, s := range sessions.MainSessions(listed) {
		if strings.Contains(strings.ToLower(s.Title), q) {
			found = append(found, s)
		}
	}
	slices.SortStableFunc(found, func(a, b sessions.Summary) int { return cmp.Compare(b.UpdatedAt, a.UpdatedAt) })
	hits := make([]searchHit, 0, len(found))
	for _, s := range found {
		hits = append(hits, searchHit{sessionID: s.SessionID, title: trimOr(s.Title, L("Untitled session")), updatedAt: s.UpdatedAt})
	}
	return hits
}

func contentHits(res *protocol.SearchSessionsResult) []searchHit {
	var hits []searchHit
	for _, r := range res.Sessions {
		h := searchHit{sessionID: r.SessionID, title: trimOr(r.Title, L("Untitled session"))}
		if r.UpdatedAt != nil {
			h.updatedAt = int64(*r.UpdatedAt / 1000)
		}
		if len(r.Hits) > 0 && len(r.Hits[0].Snippets) > 0 {
			h.snippet = snippetRuns(r.Hits[0].Snippets[0])
		}
		hits = append(hits, h)
	}
	return hits
}

// mergeHits adds the hits a Session does not have yet, up to searchLimit; a
// Session found by its title takes the snippet a later search found in it.
func mergeHits(hits, more []searchHit) []searchHit {
	out := slices.Clone(hits)
	at := map[string]int{}
	for i, h := range out {
		at[h.sessionID] = i
	}
	for _, h := range more {
		if i, ok := at[h.sessionID]; ok {
			if len(out[i].snippet) == 0 {
				out[i].snippet = h.snippet
			}
			continue
		}
		at[h.sessionID] = len(out)
		out = append(out, h)
	}
	if len(out) > searchLimit {
		out = out[:searchLimit]
	}
	return out
}

var (
	markTag  = regexp.MustCompile(`<mark>|</mark>`)
	anyTag   = regexp.MustCompile(`<[^>]+>`)
	spaceRun = regexp.MustCompile(`\s+`)
)

// snippetRuns splits a Daemon snippet on its <mark> tags; any other tag is
// dropped.
func snippetRuns(snippet string) []snippetRun {
	var runs []snippetRun
	for i, part := range markTag.Split(snippet, -1) {
		if t := spaceRun.ReplaceAllString(anyTag.ReplaceAllString(part, ""), " "); t != "" {
			runs = append(runs, snippetRun{t, i%2 == 1})
		}
	}
	return runs
}

func (a *App) searchResults(c *ui.Context, selected string) {
	k, t := a.kit, a.kit.T
	s := &a.sidebar.search
	q := strings.TrimSpace(a.sidebar.query)
	note := func(text string, spin bool) {
		ui.Row(c).Gap(k.Px(8)).Padding(k.Px(4), k.Px(8)).Children(func() {
			if spin {
				k.Spinner(c, 12, t.MutedForeground)
			}
			k.Text(c, text, 12, 16).TextColor(t.MutedForeground)
		})
	}
	if len([]rune(q)) < searchMinLength {
		note(L("Type a little more to search."), false)
		return
	}
	s.mu.Lock()
	hits, err, forQuery, pending := s.hits, s.err, s.query, s.pending
	s.mu.Unlock()
	switch {
	case err != "" && forQuery == q:
		k.Text(c, err, 12, 16).TextColor(t.DestructiveForeground).Padding(k.Px(4), k.Px(8))
		return
	case forQuery == "", len(hits) == 0 && pending:
		note(L("Searching…"), true)
		return
	case len(hits) == 0:
		note(L("No sessions match."), false)
		return
	}
	ui.Column(c).Role(ui.RoleGroup).Label(L("Search results")).Gap(1).Children(func() {
		for _, h := range hits {
			b := ui.ButtonBase(c.Key(h.sessionID)).Label(h.title).Tooltip(h.title).Column().AlignItems(ui.Stretch).Justify(ui.Start).
				Gap(k.Px(2)).Padding(k.Px(6), k.Px(8)).Radius(k.Px(10)).Cursor(ui.CursorPointer)
			switch {
			case h.sessionID == selected:
				b.Background(t.SidebarAccent)
			case b.Hovered():
				b.Background(t.SidebarAccent.Alpha(0.6))
			}
			b.Children(func() {
				ui.Row(c).Gap(k.Px(6)).Children(func() {
					k.Text(c, h.title, 13, 19.5).TextColor(t.Foreground).SingleLine().Grow(1).Shrink(1)
					if h.updatedAt > 0 {
						k.Text(c, relativeTime(time.Unix(h.updatedAt, 0), a.cfg.Now()), 11, 16.5).TextColor(t.MutedForeground).FontFeatures("tnum")
					}
				})
				if len(h.snippet) > 0 {
					spans := make([]ui.Span, len(h.snippet))
					for i, r := range h.snippet {
						spans[i] = ui.Span{Text: r.text}
						if r.match {
							spans[i].Background, spans[i].Color = t.Highlight, t.Foreground
						}
					}
					ui.RichText(c, spans...).FontSize(k.Px(11)).FixedLineHeight(k.Px(16)).TextColor(t.MutedForeground).MaxLines(2)
				}
			})
			if b.Clicked() {
				a.Go(Route{Name: "session", SessionID: h.sessionID})
			}
		}
	})
}

// sessionDetails are the lines Copy session details puts on the clipboard,
// with the transcript file the Daemon wrote.
func (a *App) sessionDetails(s sessions.Summary) string {
	file := ""
	if home := a.cfg.FactoryHome; home != "" {
		file = host.FindSessionTranscript(filepath.Join(home, "sessions"), s.SessionID)
	}
	return sessionfile.Details(s.SessionID, s.Title, s.Cwd, file)
}
