package app

import (
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/prefs"
	"github.com/kkkk2323/droi/apps/native/pkg/sessions"
)

func TestTheSortOrderFollowsTheStoredFirstSeen(t *testing.T) {
	p := prefs.Memory()
	a := &App{prefs: p, cfg: Config{Now: func() time.Time { return time.UnixMilli(9000) }}}
	prefs.SessionsFirstSeen.Set(p, map[string]float64{"old": 1})
	if got := a.sortOrder(nil).FirstSeen["old"]; got != 1 {
		t.Fatalf("first seen %v, want 1", got)
	}
	prefs.SessionsFirstSeen.Set(p, map[string]float64{"old": 2})
	if got := a.sortOrder(nil).FirstSeen["old"]; got != 2 {
		t.Fatalf("after a change elsewhere %v, want 2", got)
	}
	o := a.sortOrder([]sessions.Summary{{SessionID: "new", UpdatedAt: 5}})
	if o.FirstSeen["new"] != 5000 || o.FirstSeen["old"] != 2 {
		t.Fatalf("with a new Session %v", o.FirstSeen)
	}
	if got := prefs.SessionsFirstSeen.Get(p); got["new"] != 5000 {
		t.Fatalf("stored %v", got)
	}
	if got := a.sortOrder(nil).FirstSeen; len(got) != 2 {
		t.Fatalf("next frame %v", got)
	}
}
