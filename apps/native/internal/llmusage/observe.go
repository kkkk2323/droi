package llmusage

import (
	"cmp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/diag"
)

// RequestLog is the Key Pool's request log in memory, for the Observe page:
// every record, read once and then only as the log grows. It is safe for
// concurrent use.
type RequestLog struct {
	mu      sync.Mutex
	reader  *diag.LogReader
	records []diag.RequestRecord
	strs    map[string]string
}

func NewRequestLog(path string) *RequestLog {
	return &RequestLog{reader: diag.NewLogReader(path), strs: map[string]string{}}
}

// Refresh reads what was added to the log since the last read.
func (l *RequestLog) Refresh() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.reader.Read(func(r diag.RequestRecord) { l.records = append(l.records, l.slim(r)) })
}

// slim drops what the page never shows and shares the strings that repeat,
// so a log of many weeks stays small in memory.
func (l *RequestLog) slim(r diag.RequestRecord) diag.RequestRecord {
	r.BodyFile = ""
	for _, s := range []*string{&r.Kind, &r.Model, &r.Provider, &r.Upstream, &r.RequestedPool, &r.Key, &r.Pool, &r.Error, &r.SessionID, &r.CallType} {
		if *s == "" {
			continue
		}
		if v, ok := l.strs[*s]; ok {
			*s = v
		} else {
			l.strs[*s] = *s
		}
	}
	return r
}

// Period is how far back the Observe page looks.
type Period int

const (
	PeriodToday Period = iota
	PeriodWeek
	PeriodMonth
	PeriodAll
)

// Periods are the choices, in the order the page offers them.
var Periods = []Period{PeriodToday, PeriodWeek, PeriodMonth, PeriodAll}

// Grouping is what the breakdown groups the calls by.
type Grouping int

const (
	ByModel Grouping = iota
	ByKey
	ByKind
	BySession
)

var Groupings = []Grouping{ByModel, ByKey, ByKind, BySession}

// ObserveQuery is what the page asks of the log. Model, Kind and FailedOnly
// filter only the request list; the rest of the page counts every call of
// the period.
type ObserveQuery struct {
	Period     Period
	Model      string
	Kind       string
	FailedOnly bool
}

// MaxListed is how many of the newest calls the request list holds.
const MaxListed = 2000

// Totals add up calls. Input counts the whole prompt, the cache included,
// as TokenUsage does.
type Totals struct {
	Calls, Failed, Switched int
	// NoUsage counts calls the upstream reported no tokens for.
	NoUsage                                      int
	Input, Output, CacheRead, CacheWrite, Reason int64
	// Credits are those of the priced calls; Unpriced counts calls with
	// tokens whose model has no known weight.
	Credits  float64
	Unpriced int
	// TTFT adds up the time to the first token of the TTFTCalls that have
	// one; DecodeMs and DecodeOut the time after it and the output tokens
	// of the calls whose speed is known.
	TTFTMs, DecodeMs, DecodeOut int64
	TTFTCalls                   int
	DurationMs                  int64
}

func (t *Totals) add(r diag.RequestRecord) {
	t.Calls++
	if !IsSuccess(r) {
		t.Failed++
	}
	if r.Rotated {
		t.Switched++
	}
	t.DurationMs += r.DurationMs
	first := r.TTFTMs
	if first <= 0 {
		first = r.TTFBMs
	}
	if first > 0 {
		t.TTFTMs += first
		t.TTFTCalls++
	}
	u := r.Usage
	if u == nil {
		t.NoUsage++
		return
	}
	t.Input += u.Input
	t.Output += u.Output
	t.CacheRead += u.CacheRead
	t.CacheWrite += u.CacheWrite
	t.Reason += u.Reasoning
	if c, ok := Credits(r.Model, *u); ok {
		t.Credits += c
	} else if !u.IsZero() {
		t.Unpriced++
	}
	if ms := r.DurationMs - first; first > 0 && ms > 0 && u.Output > 0 {
		t.DecodeMs += ms
		t.DecodeOut += u.Output
	}
}

func (t *Totals) merge(o Totals) {
	t.Calls += o.Calls
	t.Failed += o.Failed
	t.Switched += o.Switched
	t.NoUsage += o.NoUsage
	t.Input += o.Input
	t.Output += o.Output
	t.CacheRead += o.CacheRead
	t.CacheWrite += o.CacheWrite
	t.Reason += o.Reason
	t.Credits += o.Credits
	t.Unpriced += o.Unpriced
	t.TTFTMs += o.TTFTMs
	t.TTFTCalls += o.TTFTCalls
	t.DecodeMs += o.DecodeMs
	t.DecodeOut += o.DecodeOut
	t.DurationMs += o.DurationMs
}

// Tokens are the input and output tokens together.
func (t Totals) Tokens() int64 { return t.Input + t.Output }

// CacheRate is the share of the input read from the cache.
func (t Totals) CacheRate() (float64, bool) {
	if t.Input == 0 {
		return 0, false
	}
	return float64(t.CacheRead) / float64(t.Input) * 100, true
}

// Speed is the output tokens a second after the first token.
func (t Totals) Speed() (float64, bool) {
	if t.DecodeMs <= 0 {
		return 0, false
	}
	return float64(t.DecodeOut) / (float64(t.DecodeMs) / 1000), true
}

// MeanTTFT is the average time to the first token.
func (t Totals) MeanTTFT() (int64, bool) {
	if t.TTFTCalls == 0 {
		return 0, false
	}
	return t.TTFTMs / int64(t.TTFTCalls), true
}

// Credits are the Standard credits a call spent, as Factory counts them
// (see TokenWeight); ok is false for a model without a known weight.
func Credits(model string, u diag.TokenUsage) (float64, bool) {
	w, ok := weightOf(model)
	if !ok {
		return 0, false
	}
	plain := max(u.Input-u.CacheRead-u.CacheWrite, 0)
	return w.Input*float64(plain) + w.Input*1.25*float64(u.CacheWrite) + w.CacheRead*float64(u.CacheRead) + w.Output*float64(u.Output), true
}

func weightOf(model string) (TokenWeight, bool) {
	m := strings.ToLower(model)
	for _, w := range StandardTokenWeights {
		if strings.HasPrefix(m, w.ID) {
			return w, true
		}
	}
	return TokenWeight{}, false
}

// Group is one row of the breakdown: what the calls share, and their
// totals.
type Group struct {
	ID     string
	Totals Totals
}

// Bucket is one bar of the trend: the calls from Start, split among the
// observation's top models and the rest (the last part).
type Bucket struct {
	Start time.Time
	Parts []Totals
}

func (b Bucket) Total() Totals {
	var t Totals
	for _, p := range b.Parts {
		t.merge(p)
	}
	return t
}

// TopModels is how many models the trend shows apart; the others share
// one color.
const TopModels = 5

// Observation is what the Observe page shows of one period.
type Observation struct {
	Query    ObserveQuery
	From, To time.Time
	// Hourly says the buckets are hours; else days, or weeks for a long
	// log.
	Hourly, Weekly bool
	Totals         Totals
	Top            []string
	Buckets        []Bucket
	// Heat is the calls by weekday (Monday first) and hour.
	Heat   [7][24]Totals
	Groups map[Grouping][]Group
	// Listed are the newest calls the list's filters let through, newest
	// first; Matching counts them all.
	Listed   []diag.RequestRecord
	Matching int
	// Models and Kinds are the filters' choices: those seen in the period.
	Models, Kinds []string
	// Records is how many calls the whole log holds.
	Records int
}

// Observe adds up the period of q as of now.
func (l *RequestLog) Observe(q ObserveQuery, now time.Time) Observation {
	l.mu.Lock()
	defer l.mu.Unlock()
	o := Observation{Query: q, To: now, Groups: map[Grouping][]Group{}, Records: len(l.records)}
	day := startOfDay(now)
	switch q.Period {
	case PeriodToday:
		o.From, o.Hourly = day, true
	case PeriodWeek:
		o.From = day.AddDate(0, 0, -6)
	case PeriodMonth:
		o.From = day.AddDate(0, 0, -29)
	default:
		o.From = day
		for _, r := range l.records {
			if r.Time.Before(o.From) {
				o.From = startOfDay(r.Time.Local())
			}
		}
		o.Weekly = day.Sub(o.From) > 90*24*time.Hour
		if o.Weekly {
			o.From = o.From.AddDate(0, 0, -(int(o.From.Weekday())+6)%7)
		}
	}

	var in []diag.RequestRecord
	byModel := map[string]Totals{}
	for _, r := range l.records {
		if r.Time.Before(o.From) || r.Time.After(now) {
			continue
		}
		in = append(in, r)
		t := byModel[r.Model]
		t.add(r)
		byModel[r.Model] = t
	}
	models := sortedGroups(byModel)
	o.Groups[ByModel] = models
	for _, g := range models {
		if len(o.Top) == TopModels {
			break
		}
		o.Top = append(o.Top, g.ID)
	}
	part := func(model string) int {
		if i := slices.Index(o.Top, model); i >= 0 {
			return i
		}
		return len(o.Top)
	}

	next := func(t time.Time) time.Time {
		switch {
		case o.Hourly:
			return t.Add(time.Hour)
		case o.Weekly:
			return t.AddDate(0, 0, 7)
		}
		return t.AddDate(0, 0, 1)
	}
	for t := o.From; !t.After(now); t = next(t) {
		o.Buckets = append(o.Buckets, Bucket{Start: t, Parts: make([]Totals, len(o.Top)+1)})
	}
	bucketOf := func(t time.Time) int {
		t = t.Local()
		switch {
		case o.Hourly:
			return t.Hour()
		case o.Weekly:
			return int(startOfDay(t).Sub(o.From).Hours()/24) / 7
		}
		return int(startOfDay(t).Sub(o.From).Hours()/24 + 0.5)
	}

	byKey, byKind, bySession := map[string]Totals{}, map[string]Totals{}, map[string]Totals{}
	seenKinds := map[string]bool{}
	for _, r := range in {
		o.Totals.add(r)
		if i := bucketOf(r.Time); i >= 0 && i < len(o.Buckets) {
			o.Buckets[i].Parts[part(r.Model)].add(r)
		}
		local := r.Time.Local()
		o.Heat[(int(local.Weekday())+6)%7][local.Hour()].add(r)
		addTo(byKey, r.Key, r)
		addTo(byKind, r.Kind, r)
		seenKinds[r.Kind] = true
		if r.SessionID != "" {
			addTo(bySession, r.SessionID, r)
		}
	}
	o.Groups[ByKey] = sortedGroups(byKey)
	o.Groups[ByKind] = sortedGroups(byKind)
	o.Groups[BySession] = sortedGroups(bySession)
	for _, g := range models {
		o.Models = append(o.Models, g.ID)
	}
	for _, k := range []string{"", "cache_warm", "key_open"} {
		if seenKinds[k] {
			o.Kinds = append(o.Kinds, kindID(k))
		}
	}

	for i := len(in) - 1; i >= 0; i-- {
		r := in[i]
		if q.Model != "" && r.Model != q.Model || q.Kind != "" && kindID(r.Kind) != q.Kind || q.FailedOnly && IsSuccess(r) {
			continue
		}
		o.Matching++
		o.Listed = append(o.Listed, r)
	}
	// The log is in the order the files were written, which an imported
	// archive can break.
	slices.SortStableFunc(o.Listed, func(a, b diag.RequestRecord) int { return b.Time.Compare(a.Time) })
	if len(o.Listed) > MaxListed {
		o.Listed = o.Listed[:MaxListed]
	}
	return o
}

// kindID names a model request "request" in the list's filter, where ""
// means every kind.
func kindID(kind string) string {
	if kind == "" {
		return "request"
	}
	return kind
}

func addTo(m map[string]Totals, id string, r diag.RequestRecord) {
	t := m[id]
	t.add(r)
	m[id] = t
}

// sortedGroups puts the groups that used the most tokens first, then the
// most calls.
func sortedGroups(m map[string]Totals) []Group {
	out := make([]Group, 0, len(m))
	for id, t := range m {
		out = append(out, Group{ID: id, Totals: t})
	}
	slices.SortFunc(out, func(a, b Group) int {
		if c := cmp.Compare(b.Totals.Tokens(), a.Totals.Tokens()); c != 0 {
			return c
		}
		if c := cmp.Compare(b.Totals.Calls, a.Totals.Calls); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	return out
}

func startOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}
