package llmusage

import (
	"sort"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/keys"
)

// RunwayDays is how far ahead the runway looks.
const RunwayDays = 30

// monthDays is how long a Standard month runs once it starts.
const monthDays = 30

// runwayMonth is one key's Standard month as the runway sees it: left
// percent until end days from now, or, when !live, a whole month that
// starts on the key's next request.
type runwayMonth struct {
	left, end float64
	live      bool
}

// Runway answers how long the Standard pool lasts at a steady spend per day,
// in percent of one key's monthly quota. A month that has not started, or
// has ended, starts on the key's next request: it is a whole month waiting.
// A live month ends at its reset and what is left of it then is lost. Live
// months are spent soonest end first, and a waiting one starts only when
// they are spent. The weekly and 5-hour windows and the Key Pool's routing
// are left out: across a pool the monthly windows are what runs out, and
// this is a planning figure.
type Runway struct {
	months []runwayMonth
}

// NewRunway reads the routable keys' Standard months. ok is false when no
// key reports one.
func NewRunway(pool []keys.KeyState, now time.Time) (Runway, bool) {
	var r Runway
	for _, k := range pool {
		monthly := WindowOf(PoolOf(k, Standard), Monthly)
		if k.Status != "ok" || monthly == nil {
			continue
		}
		m := runwayMonth{}
		if IsLive(monthly, now) {
			end, _ := ResetAt(monthly)
			m = runwayMonth{left: Remaining(monthly), end: end.Sub(now).Hours() / 24, live: true}
		}
		r.months = append(r.months, m)
	}
	return r, len(r.months) > 0
}

// Keys is how many keys' months the runway counts: together they are the
// pool's whole month.
func (r Runway) Keys() int { return len(r.months) }

// Lasts is how many days the pool lasts at perDay, and whether it lasts all
// RunwayDays.
func (r Runway) Lasts(perDay float64) (days float64, lasts bool) {
	days, lasts, _ = r.simulate(perDay)
	return days, lasts
}

// LeftAfter is what the pool still holds after RunwayDays at perDay: the
// rest of the live months and the months not started yet. It is 0 when the
// pool runs out first.
func (r Runway) LeftAfter(perDay float64) float64 {
	_, _, left := r.simulate(perDay)
	return left
}

func (r Runway) simulate(perDay float64) (days float64, lasts bool, left float64) {
	perDay = max(perDay, 0)
	months := append([]runwayMonth(nil), r.months...)
	for d := 0; d < RunwayDays; d++ {
		for i := range months {
			if months[i].live && months[i].end <= float64(d) {
				months[i] = runwayMonth{}
			}
		}
		sort.SliceStable(months, func(i, j int) bool {
			if months[i].live != months[j].live {
				return months[i].live
			}
			return months[i].end < months[j].end
		})
		need := perDay
		for i := range months {
			if need <= 0 {
				break
			}
			if !months[i].live {
				months[i] = runwayMonth{left: 100, end: float64(d + monthDays), live: true}
			}
			take := min(months[i].left, need)
			months[i].left -= take
			need -= take
		}
		if need > 1e-9 {
			return float64(d) + (perDay-need)/perDay, false, 0
		}
	}
	for _, m := range months {
		switch {
		case !m.live:
			left += 100
		case m.end > RunwayDays:
			left += m.left
		}
	}
	return RunwayDays, true, left
}

// DailyLimit is the most the pool can give every day for RunwayDays.
func (r Runway) DailyLimit() float64 {
	// No key gives more than what is left of its month and one more month.
	lo, hi := 0.0, float64(len(r.months))*200
	for range 50 {
		mid := (lo + hi) / 2
		if _, ok := r.Lasts(mid); ok {
			lo = mid
		} else {
			hi = mid
		}
	}
	return lo
}
