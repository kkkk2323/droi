// Package smartwarm learns, from the user's own Droid session transcripts,
// how long they take to come back to a session that waits for them, and
// decides whether one more prompt cache refresh is worth its cost. Nothing
// leaves the computer: the transcripts are read in place and only wait
// lengths are kept.
package smartwarm

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"time"
)

// State is what a session waits for while a refresh is considered.
type State uint8

const (
	// Waiting: the agent loop is blocked on a tool, such as an AskUser
	// question, a permission prompt or a long command.
	Waiting State = iota
	// Idle: the turn ended and the session waits for the next prompt.
	Idle
)

// Features describe one wait. History and the live session are read by the
// same code, so a live wait lands in the bucket its history built.
type Features struct {
	State State
	// FirstTurn is an idle wait after the session's first reply; users often
	// leave a session after its first answer.
	FirstTurn bool
	// LongReply is an idle wait after a reply longer than LongReplyChars;
	// reading it takes time, and the user usually comes back.
	LongReply bool
}

const (
	LongReplyChars = 1500
	// Horizon bounds a wait: a reply later than this counts as never.
	Horizon = 3 * time.Hour
	// Window is how far back transcripts are read.
	Window = 90 * 24 * time.Hour
	// minBucket is the fewest waits a bucket needs before it is trusted;
	// smaller buckets fall back to all waits of the same state.
	minBucket = 40
	// minGap drops waits no refresh can affect.
	minGap = 4 * time.Minute
	never  = time.Duration(math.MaxInt64)
	// rebuildEvery keeps the model current with new sessions.
	rebuildEvery = time.Hour
)

type wait struct {
	f   Features
	gap time.Duration
}

type message struct {
	at         time.Time
	role       string
	text       bool
	toolUse    bool
	toolResult bool
	textLen    int
}

type transcriptLine struct {
	Type             string `json:"type"`
	Timestamp        string `json:"timestamp"`
	CallingSessionID string `json:"callingSessionId"`
	Parent           string `json:"parent"`
	Message          *struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

var errSubagent = errors.New("subagent session")

// parse reads a Droid session transcript. Subagent sessions are rejected:
// their prompts come from the parent session, not from the user.
func parse(r io.Reader) ([]message, error) {
	br := bufio.NewReaderSize(r, 1<<16)
	var msgs []message
	first := true
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			if first {
				first = false
				var start transcriptLine
				if json.Unmarshal(line, &start) == nil && start.Type == "session_start" &&
					(start.CallingSessionID != "" || start.Parent != "") {
					return nil, errSubagent
				}
			}
			// Skip tool output and other bulky lines before decoding them.
			if head := line[:min(len(line), 40)]; bytes.Contains(head, []byte(`"type":"message"`)) {
				if m, ok := parseMessage(line); ok {
					msgs = append(msgs, m)
				}
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	sort.SliceStable(msgs, func(i, j int) bool { return msgs[i].at.Before(msgs[j].at) })
	return msgs, nil
}

func parseMessage(line []byte) (message, bool) {
	var l transcriptLine
	if json.Unmarshal(line, &l) != nil || l.Message == nil || l.Timestamp == "" {
		return message{}, false
	}
	at, err := time.Parse(time.RFC3339Nano, l.Timestamp)
	if err != nil {
		return message{}, false
	}
	var blocks []contentBlock
	if json.Unmarshal(l.Message.Content, &blocks) != nil {
		return message{}, false
	}
	m := message{at: at, role: l.Message.Role}
	for _, b := range blocks {
		switch b.Type {
		case "text":
			m.text = true
			m.textLen += len([]rune(b.Text))
		case "tool_use":
			m.toolUse = true
		case "tool_result":
			m.toolResult = true
		}
	}
	return m, true
}

// waits lists every wait of a transcript. A wait starts at the last assistant
// message before the user's side speaks: on a tool call it ends with the tool
// result, at the end of a turn with the next prompt. The last wait of a live
// session has no end yet and counts as never.
func waits(msgs []message) []wait {
	var out []wait
	idleTurns := 0
	for i, m := range msgs {
		if m.role != "assistant" || (i+1 < len(msgs) && msgs[i+1].role == "assistant") {
			continue
		}
		var f Features
		var ends func(message) bool
		if m.toolUse {
			f = Features{State: Waiting}
			ends = func(n message) bool { return n.role == "user" && n.toolResult }
		} else {
			f = Features{State: Idle, FirstTurn: idleTurns == 0, LongReply: m.textLen > LongReplyChars}
			idleTurns++
			ends = func(n message) bool { return n.role == "user" && n.text }
		}
		gap := never
		for _, n := range msgs[i+1:] {
			if ends(n) {
				if g := n.at.Sub(m.at); g <= Horizon {
					gap = g
				}
				break
			}
		}
		out = append(out, wait{f: f, gap: gap})
	}
	return out
}

// Model holds the sorted wait lengths per bucket.
type Model struct {
	buckets map[Features][]time.Duration
	states  map[State][]time.Duration
	// Waits counts the waits the model learned from.
	Waits   int
	BuiltAt time.Time
}

func newModel(ws []wait, builtAt time.Time) *Model {
	m := &Model{buckets: map[Features][]time.Duration{}, states: map[State][]time.Duration{}, BuiltAt: builtAt}
	for _, w := range ws {
		if w.gap <= minGap {
			continue
		}
		m.buckets[w.f] = append(m.buckets[w.f], w.gap)
		m.states[w.f.State] = append(m.states[w.f.State], w.gap)
		m.Waits++
	}
	for _, g := range m.buckets {
		sort.Slice(g, func(i, j int) bool { return g[i] < g[j] })
	}
	for _, g := range m.states {
		sort.Slice(g, func(i, j int) bool { return g[i] < g[j] })
	}
	return m
}

// Query is one due refresh. All durations are measured from the reply that
// started the wait.
type Query struct {
	Features Features
	Elapsed  time.Duration
	// CoveredUntil is where the current cache entry expires.
	CoveredUntil time.Duration
	// Interval is the time between refreshes, TTL how long one keeps the cache.
	Interval time.Duration
	TTL      time.Duration
	// Gain is what a return inside the cache saves, in refresh costs.
	Gain float64
}

// Worth reports whether refreshing now pays off over the user's past waits
// that lasted at least as long as this one: worth is true when some number of
// further refreshes has a positive expected value. ok is false when the
// history is too thin to say.
func (m *Model) Worth(q Query) (worth, ok bool) {
	if m == nil || q.Interval <= 0 {
		return false, false
	}
	gaps := m.buckets[q.Features]
	if len(gaps) < minBucket {
		gaps = m.states[q.Features.State]
	}
	if len(gaps) < minBucket {
		return false, false
	}
	survivors := gaps[sort.Search(len(gaps), func(i int) bool { return gaps[i] > q.Elapsed }):]
	for n := 1; ; n++ {
		start := q.Elapsed + time.Duration(n-1)*q.Interval
		if start > Horizon {
			return false, true
		}
		coverEnd := start + q.TTL
		value := 0.0
		for _, g := range survivors {
			// Refresh j (1-based) is sent at Elapsed+(j-1)*Interval while the
			// user has not come back yet.
			refreshes := n
			if g != never {
				refreshes = min(n, int((g-q.Elapsed-1)/q.Interval)+1)
			}
			value -= float64(refreshes)
			if g > q.CoveredUntil && g <= coverEnd {
				value += q.Gain
			}
		}
		if value > 0 {
			return true, true
		}
	}
}

// Advisor keeps a model of the transcripts under Dir (FACTORY_HOME/sessions)
// and rebuilds it in the background. It is safe for concurrent use.
type Advisor struct {
	Dir string
	now func() time.Time

	mu       sync.Mutex
	model    *Model
	building bool
	files    map[string]fileWaits
}

type fileWaits struct {
	mod   time.Time
	size  int64
	waits []wait
}

func NewAdvisor(dir string) *Advisor {
	return &Advisor{Dir: dir, now: time.Now, files: map[string]fileWaits{}}
}

// Prepare starts a rebuild when the model is missing or older than an hour.
func (a *Advisor) Prepare() {
	a.mu.Lock()
	stale := a.model == nil || a.now().Sub(a.model.BuiltAt) > rebuildEvery
	if !stale || a.building {
		a.mu.Unlock()
		return
	}
	a.building = true
	a.mu.Unlock()
	go func() {
		m := a.Build()
		a.mu.Lock()
		a.model, a.building = m, false
		a.mu.Unlock()
	}()
}

// Build reads the transcripts changed in the last Window; unchanged files are
// taken from the previous build.
func (a *Advisor) Build() *Model {
	now := a.now()
	paths, _ := filepath.Glob(filepath.Join(a.Dir, "*", "*.jsonl"))
	a.mu.Lock()
	prev := a.files
	a.mu.Unlock()
	next := make(map[string]fileWaits, len(paths))
	var all []wait
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil || now.Sub(info.ModTime()) > Window {
			continue
		}
		fw, ok := prev[p]
		if !ok || !fw.mod.Equal(info.ModTime()) || fw.size != info.Size() {
			fw = fileWaits{mod: info.ModTime(), size: info.Size(), waits: readWaits(p)}
		}
		next[p] = fw
		all = append(all, fw.waits...)
	}
	a.mu.Lock()
	a.files = next
	a.mu.Unlock()
	return newModel(all, now)
}

func readWaits(path string) []wait {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	msgs, err := parse(f)
	if err != nil {
		return nil
	}
	return waits(msgs)
}

// Model returns the current model, or nil before the first build finished.
func (a *Advisor) Model() *Model {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.model
}

// Worth answers q from the current model and starts a rebuild when it is old.
func (a *Advisor) Worth(q Query) (worth, ok bool) {
	a.Prepare()
	return a.Model().Worth(q)
}

var sessionIDPattern = regexp.MustCompile(`^[A-Za-z0-9-]{8,64}$`)

// Features reads the live session's transcript and describes its current
// wait. state comes from the Daemon, which knows it before the transcript
// does; a missing or unreadable transcript gives the state alone.
func (a *Advisor) Features(sessionID string, state State) Features {
	f := Features{State: state}
	// The id comes from a request header; keep it out of the glob syntax.
	if state != Idle || !sessionIDPattern.MatchString(sessionID) {
		return f
	}
	paths, _ := filepath.Glob(filepath.Join(a.Dir, "*", sessionID+".jsonl"))
	if len(paths) == 0 {
		return f
	}
	ws := readWaits(paths[0])
	if len(ws) == 0 || ws[len(ws)-1].f.State != Idle {
		return f
	}
	return ws[len(ws)-1].f
}
