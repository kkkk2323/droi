// Package loops is what Droi knows of a Session's loops: the Daemon's
// session_prompt crons, which send a prompt to the same Session on a
// schedule (droid's /loop). It reads their records and turns an interval
// into the cron expression the Daemon takes, as droid does.
package loops

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/kkkk2323/droid-sdk-go/protocol"
)

// Loop is one loop of a Session.
type Loop struct {
	ID         string
	SessionID  string
	Prompt     string
	Expression string // 5 fields, in UTC
	Status     protocol.CronStatus
	HoldReason string
	NextRunAt  time.Time // zero when not known
	LastError  string
	CreatedAt  string
}

// FromRecord reads a cron record; false for one that is not a Session's
// loop, or is over (expired, cancelled).
func FromRecord(r protocol.CronRecord) (Loop, bool) {
	v, err := r.Value()
	if err != nil {
		return Loop{}, false
	}
	c, ok := v.(*protocol.SessionCronRecord)
	if !ok || c.Status == protocol.CronStatusExpired || c.Status == protocol.CronStatusCancelled {
		return Loop{}, false
	}
	l := Loop{
		ID:         c.ID,
		SessionID:  c.Scope.SessionID,
		Prompt:     c.Payload.Prompt,
		Expression: c.Schedule.Expression,
		Status:     c.Status,
		HoldReason: c.HoldReason,
		LastError:  c.Stats.LastError,
		CreatedAt:  c.CreatedAt,
	}
	if t, err := time.Parse(time.RFC3339, c.Schedule.NextRunAt); err == nil {
		l.NextRunAt = t
	}
	return l, true
}

// Live reports whether the loop runs or waits to: it needs its Session's
// worker. A paused one does not.
func (l Loop) Live() bool {
	switch l.Status {
	case protocol.CronStatusActive, protocol.CronStatusRunning, protocol.CronStatusHeld:
		return true
	}
	return false
}

// Sort orders loops as they were made.
func Sort(ls []Loop) {
	slices.SortFunc(ls, func(a, b Loop) int {
		if c := strings.Compare(a.CreatedAt, b.CreatedAt); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
}

// The intervals the Daemon takes for a loop.
const (
	MinInterval = time.Minute
	MaxInterval = 24 * time.Hour
)

var (
	ErrNoInterval = errors.New("no interval")
	ErrTooShort   = errors.New("interval too short")
	ErrTooLong    = errors.New("interval too long")
	// ErrUneven is an interval no cron expression repeats exactly, such as 7m.
	ErrUneven = errors.New("interval not a cron step")
)

var duration = regexp.MustCompile(`(?i)^(\d+(?:\.\d{1,2})?)\s*(s|sec|secs|second|seconds|m|min|mins|minute|minutes|h|hr|hrs|hour|hours|d|day|days)$`)

// ParseInterval reads an interval as droid's /loop does: 30s, 10m, 30min,
// 1.5h, 2 hours, 1d.
func ParseInterval(s string) (time.Duration, bool) {
	m := duration.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0, false
	}
	whole, fraction, _ := strings.Cut(m[1], ".")
	w, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return 0, false
	}
	f, _ := strconv.ParseInt((fraction + "00")[:2], 10, 64)
	unit := time.Hour * 24
	switch strings.ToLower(m[2])[0] {
	case 's':
		unit = time.Second
	case 'm':
		unit = time.Minute
	case 'h':
		unit = time.Hour
	}
	return time.Duration(w*100+f) * unit / 100, true
}

var (
	minuteSteps = []int{1, 2, 3, 4, 5, 6, 10, 12, 15, 20, 30}
	hourSteps   = []int{1, 2, 3, 4, 6, 8, 12, 24}
)

// Check tells why the Daemon would not take interval d, nil when it would.
func Check(d time.Duration) error {
	switch {
	case d < MinInterval:
		return ErrTooShort
	case d > MaxInterval:
		return ErrTooLong
	case d%time.Minute != 0:
		return ErrUneven
	case d < time.Hour && slices.Contains(minuteSteps, int(d/time.Minute)):
		return nil
	case d >= time.Hour && d%time.Hour == 0 && slices.Contains(hourSteps, int(d/time.Hour)):
		return nil
	}
	return ErrUneven
}

// Cron is the expression that repeats a checked interval d from anchor on,
// in UTC as the Daemon reads it: the first run after anchor is a whole
// interval away (15m made at :55 is "10,25,40,55 * * * *").
func Cron(d time.Duration, anchor time.Time) string {
	anchor = anchor.UTC()
	steps := func(step, offset, span int) string {
		if offset == 0 {
			return "*/" + strconv.Itoa(step)
		}
		var vs []string
		for v := offset; v < span; v += step {
			vs = append(vs, strconv.Itoa(v))
		}
		return strings.Join(vs, ",")
	}
	if m := int(d / time.Minute); m < 60 {
		return steps(m, anchor.Minute()%m, 60) + " * * * *"
	}
	if h := int(d / time.Hour); h < 24 {
		return strconv.Itoa(anchor.Minute()) + " " + steps(h, anchor.Hour()%h, 24) + " * * *"
	}
	return strconv.Itoa(anchor.Minute()) + " " + strconv.Itoa(anchor.Hour()) + " * * *"
}

var valueList = regexp.MustCompile(`^\d+(,\d+)*$`)

// Interval is how often expression repeats, false for a schedule that is
// not an even interval. A daily one also gives its time of day (UTC).
func Interval(expression string) (d time.Duration, hour, minute int, ok bool) {
	f := strings.Fields(expression)
	if len(f) != 5 || f[2] != "*" || f[3] != "*" || f[4] != "*" {
		return 0, 0, 0, false
	}
	list := func(s string) []int {
		if !valueList.MatchString(s) {
			return nil
		}
		var out []int
		for _, p := range strings.Split(s, ",") {
			n, _ := strconv.Atoi(p)
			out = append(out, n)
		}
		slices.Sort(out)
		return out
	}
	// gap is the even step values cover span with, 0 for none.
	gap := func(vs []int, span int) int {
		if len(vs) == 0 || span%len(vs) != 0 || vs[0] >= span/len(vs) {
			return 0
		}
		g := span / len(vs)
		for i := 1; i < len(vs); i++ {
			if vs[i]-vs[i-1] != g {
				return 0
			}
		}
		return g
	}
	step := func(s string) int {
		if n, found := strings.CutPrefix(s, "*/"); found {
			v, _ := strconv.Atoi(n)
			return v
		}
		return 0
	}
	checked := func(d time.Duration) (time.Duration, int, int, bool) {
		if d == 0 || Check(d) != nil {
			return 0, 0, 0, false
		}
		return d, 0, 0, true
	}
	if f[1] == "*" {
		if f[0] == "*" {
			return time.Minute, 0, 0, true
		}
		if s := step(f[0]); s > 0 {
			return checked(time.Duration(s) * time.Minute)
		}
		return checked(time.Duration(gap(list(f[0]), 60)) * time.Minute)
	}
	mins := list(f[0])
	if len(mins) != 1 {
		return 0, 0, 0, false
	}
	if s := step(f[1]); s > 0 {
		return checked(time.Duration(s) * time.Hour)
	}
	hours := list(f[1])
	if len(hours) == 1 {
		return 24 * time.Hour, hours[0], mins[0], true
	}
	return checked(time.Duration(gap(hours, 24)) * time.Hour)
}

// Command is what `/loop` was asked for.
type Command struct {
	// Stop: `/loop stop`, which deletes the Session's loops.
	Stop     bool
	Interval time.Duration
	// Prompt is "" when only an interval was given.
	Prompt string
}

var trailingEvery = regexp.MustCompile(`(?is)^(.*?)\s+every\s+(.+)$`)

// ParseCommand reads the arguments of `/loop`: `<interval> <prompt>`,
// `<prompt> every <interval>`, or `stop`. The error is ErrNoInterval, or
// Check's for an interval the Daemon would not take.
func ParseCommand(args string) (Command, error) {
	args = strings.TrimSpace(args)
	if strings.EqualFold(args, "stop") {
		return Command{Stop: true}, nil
	}
	first, rest, _ := strings.Cut(args, " ")
	d, ok := ParseInterval(first)
	prompt := strings.TrimSpace(rest)
	if !ok {
		m := trailingEvery.FindStringSubmatch(args)
		if m == nil {
			return Command{}, ErrNoInterval
		}
		if d, ok = ParseInterval(m[2]); !ok {
			return Command{}, ErrNoInterval
		}
		prompt = strings.TrimSpace(m[1])
	}
	if err := Check(d); err != nil {
		return Command{}, err
	}
	return Command{Interval: d, Prompt: prompt}, nil
}

// BuiltInPrompt is the prompt of a loop given only an interval, without a
// loop.md, as droid's.
const BuiltInPrompt = "You are running on a recurring loop with no specific task assigned.\n" +
	"Continue any unfinished work from this conversation, ask the user what to focus on,\n" +
	"or reply with a single line stating you are idle. Be brief.\n" +
	"Do not start new tasks, load new tools, or invoke skills unless the user explicitly asked."

// maxPromptBytes is how much of a loop.md droid reads.
const maxPromptBytes = 25_000

// DefaultPrompt is the prompt of a loop given only an interval, as droid
// finds it: the first loop.md under .factory, .agents or .claude in the
// Session's folder, then in home, else BuiltInPrompt. An empty folder is
// not looked in.
func DefaultPrompt(cwd, home string) string {
	for _, root := range []string{cwd, home} {
		if root == "" {
			continue
		}
		for _, dir := range []string{".factory", ".agents", ".claude"} {
			path := filepath.Join(root, dir, "loop.md")
			if fi, err := os.Lstat(path); err != nil || !fi.Mode().IsRegular() {
				continue
			}
			b, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			if s := strings.TrimSpace(string(b[:min(len(b), maxPromptBytes)])); s != "" {
				return s
			}
		}
	}
	return BuiltInPrompt
}
