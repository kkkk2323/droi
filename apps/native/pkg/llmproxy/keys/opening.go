package keys

import (
	"sort"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/config"
)

// A Standard month runs this long from the first request after the last one
// ended. Keys are bought by the year, so a key whose month is not running is
// idle quota. The rotator opens months worth at most config.DailyKeyOpens Pro
// months per local day (a Max key's counts ten, see PlanScale), so the months
// end on different days, and opens more only when every open key is spent.
// The first key of a day opens whatever its plan.
const monthlyWindow = 30 * 24 * time.Hour

// openMode says what a key selection is for.
type openMode int

const (
	// openNone picks the sticky active key: no planned opening.
	openNone openMode = iota
	// openPreview answers what a request would get without counting it.
	openPreview
	// openRoute selects for a request about to be sent; an opening is recorded.
	openRoute
)

// standardMonth reports whether the key's Standard month is running and when
// it started.
func (u *KeyUsage) standardMonth(now time.Time) (time.Time, bool) {
	end, ok := u.standardMonthEnd()
	if !ok || !end.After(now) {
		return time.Time{}, false
	}
	return end.Add(-monthlyWindow), true
}

func (u *KeyUsage) standardMonthEnd() (time.Time, bool) {
	if u == nil || u.StandardLimits == nil || u.StandardLimits.Monthly == nil || u.StandardLimits.Monthly.WindowEnd == "" {
		return time.Time{}, false
	}
	end, err := time.Parse(time.RFC3339, u.StandardLimits.Monthly.WindowEnd)
	return end, err == nil
}

// waitingSince orders the opening queue: a key that never ran waits from the
// zero time, one whose month ended from that end.
func (u *KeyUsage) waitingSince() time.Time {
	end, _ := u.standardMonthEnd()
	return end
}

// pendingOpen: a request went to the key since its last usage probe, so its
// month has started even though the probe does not show it yet.
func (r *KeyRotator) pendingOpenLocked(key string, usage *KeyUsage) bool {
	at, ok := r.openedAt[key]
	return ok && usage != nil && !at.Before(usage.LastCheckedAt)
}

// waitingToOpen reports a routable key whose Standard month is not running.
// Keys whose probe has no monthly bucket (legacy billing) are not planned.
// Caller holds r.mu (read) and r.openMu.
func (r *KeyRotator) waitingToOpenLocked(key string, usage *KeyUsage, now time.Time) bool {
	if usage == nil || usage.Error != "" || usage.StandardLimits == nil || usage.StandardLimits.Monthly == nil {
		return false
	}
	_, open := usage.standardMonth(now)
	return !open && !r.pendingOpenLocked(key, usage)
}

func startOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

// openedTodayLocked is what the months that started today are worth, in
// Pro months. Caller holds r.mu (read) and r.openMu.
func (r *KeyRotator) openedTodayLocked(now time.Time) float64 {
	day := startOfDay(now)
	n := 0.0
	for _, key := range r.keys {
		usage := r.usages[key]
		if start, ok := usage.standardMonth(now); ok && !start.Before(day) {
			n += PlanScale(usage.Plan)
		} else if r.pendingOpenLocked(key, usage) && !r.openedAt[key].Before(day) {
			n += PlanScale(usage.Plan)
		}
	}
	return n
}

// mayOpenLocked reports whether today's openings leave room for another.
func (r *KeyRotator) mayOpenLocked(now time.Time) bool {
	return r.openedTodayLocked(now) < float64(config.DailyKeyOpens)
}

// sortOpeningQueue orders keys waiting to open: longest wait first, then
// configuration order.
func sortOpeningQueue(c []candidate) {
	sort.SliceStable(c, func(i, j int) bool {
		if !c[i].waitingSince.Equal(c[j].waitingSince) {
			return c[i].waitingSince.Before(c[j].waitingSince)
		}
		return c[i].order < c[j].order
	})
}

// SetScheduledOpening tells the rotator that a scheduled opener starts the
// planned months, so requests open one only when every open key is spent.
func (r *KeyRotator) SetScheduledOpening(on bool) { r.scheduledOpening.Store(on) }

// ClaimNextOpening returns the next key to open while today's openings are
// worth less than config.DailyKeyOpens Pro months, and counts it as opened.
func (r *KeyRotator) ClaimNextOpening() (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	r.openMu.Lock()
	defer r.openMu.Unlock()
	now := r.nowTime()
	if !r.mayOpenLocked(now) {
		return "", false
	}
	var waiting []candidate
	for i, key := range r.keys {
		usage := r.usages[key]
		if r.waitingToOpenLocked(key, usage, now) && usage.StandardAvailable() && !r.isPoolCooledDown(usage, PoolStandard) {
			waiting = append(waiting, candidate{key: key, order: i, waitingSince: usage.waitingSince()})
		}
	}
	if len(waiting) == 0 {
		return "", false
	}
	sortOpeningQueue(waiting)
	if r.openedAt == nil {
		r.openedAt = make(map[string]time.Time)
	}
	r.openedAt[waiting[0].key] = now
	return waiting[0].key, true
}

// ReleaseOpening forgets a claimed opening whose request failed.
func (r *KeyRotator) ReleaseOpening(key string) {
	r.openMu.Lock()
	defer r.openMu.Unlock()
	delete(r.openedAt, key)
}

// OpeningStatus is the month-opening plan for /debug/state.
type OpeningStatus struct {
	// DailyOpens is how many Pro months open a day; OpenedToday what the
	// months opened today are worth in Pro months.
	DailyOpens  int     `json:"dailyOpens"`
	OpenedToday float64 `json:"openedToday"`
	// Scheduled: a scheduled opener starts the months with a small request.
	Scheduled bool `json:"scheduled"`
	// Queue lists the keys waiting to open, next first, as 1-based indices
	// in configuration order.
	Queue []int `json:"queue"`
}

func (r *KeyRotator) OpeningStatus() OpeningStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()
	r.openMu.Lock()
	defer r.openMu.Unlock()
	now := r.nowTime()
	st := OpeningStatus{DailyOpens: config.DailyKeyOpens, OpenedToday: r.openedTodayLocked(now), Scheduled: r.scheduledOpening.Load(), Queue: []int{}}
	var waiting []candidate
	for i, key := range r.keys {
		if usage := r.usages[key]; r.waitingToOpenLocked(key, usage, now) {
			waiting = append(waiting, candidate{key: key, order: i, waitingSince: usage.waitingSince()})
		}
	}
	sortOpeningQueue(waiting)
	for _, c := range waiting {
		st.Queue = append(st.Queue, c.order+1)
	}
	return st
}
