package automations

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Automation is one of Droi's Automations, a folder with a HEARTBEAT.md in
// the Daemon's layout (ADR 0035): the front matter keeps Factory's key
// names, so a file moved from ~/.factory/automations reads the same.
type Automation struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Prompt   string `json:"prompt"`
	Schedule string `json:"schedule"`
	// Timezone is an IANA name; "" is this computer's zone.
	Timezone        string `json:"timezone,omitempty"`
	Model           string `json:"model,omitempty"`
	ReasoningEffort string `json:"reasoningEffort,omitempty"`
	// Autonomy is "" to run without asking (as the Daemon's runs did), or
	// low, medium or high.
	Autonomy string `json:"autonomy,omitempty"`
	// WorkingDirectory is the Workspace runs work in; "" is the
	// Automation's own folder.
	WorkingDirectory string    `json:"workingDirectory,omitempty"`
	Paused           bool      `json:"paused"`
	CreatedAt        time.Time `json:"createdAt,omitzero"`
	// ForkedFrom is the Factory automation it was moved from: its uuid,
	// else its folder name; ForkedFromName is its folder name. An edit that
	// leaves them empty keeps them.
	ForkedFrom     string `json:"forkedFrom,omitempty"`
	ForkedFromName string `json:"forkedFromName,omitempty"`
	// Conversation is the conversation its runs post into, a loop's (target
	// existing); "" opens a conversation of its own for each run (target
	// new). An edit that leaves it empty keeps it.
	Conversation string `json:"conversation,omitempty"`
	// Invalid is why its HEARTBEAT.md, schedule or time zone cannot be
	// read; it is never written. List keeps such a folder, so it can be
	// seen and deleted.
	Invalid string `json:"invalid,omitempty"`
}

// Trigger is what started a run.
type Trigger string

const (
	TriggerSchedule Trigger = "schedule"
	TriggerManual   Trigger = "manual"
	// TriggerCatchUp is a scheduled run that starts more than two minutes
	// late, after the Host was down or the computer asleep.
	TriggerCatchUp Trigger = "catch-up"
)

// RunStatus is where a run stands.
type RunStatus string

const (
	RunRunning     RunStatus = "running"
	RunSucceeded   RunStatus = "succeeded"
	RunFailed      RunStatus = "failed"
	RunInterrupted RunStatus = "interrupted"
)

// Run is the record of one run, a line of the Automation's runs.jsonl.
type Run struct {
	RunID string `json:"runId"`
	// AutomationID is the Automation's; Runs fills it in.
	AutomationID string    `json:"automationId,omitempty"`
	SessionID    string    `json:"sessionId,omitempty"`
	Trigger      Trigger   `json:"trigger"`
	Status       RunStatus `json:"status"`
	StartedAt    time.Time `json:"startedAt,omitzero"`
	EndedAt      time.Time `json:"endedAt,omitzero"`
	Error        string    `json:"error,omitempty"`
}

// State is what the scheduler keeps between starts, in state.json.
type State struct {
	// LastScheduled is, per Automation, the time up to which its scheduled
	// times are handled.
	LastScheduled map[string]time.Time `json:"lastScheduled"`
}

// MaxRuns is how many records runs.jsonl keeps.
const MaxRuns = 200

// maxError is how much of a run's error its record keeps, in bytes.
const maxError = 4 << 10

const (
	heartbeatFile = "HEARTBEAT.md"
	runsFile      = "runs.jsonl"
	stateFile     = "state.json"
)

// ErrNotFound is an Automation that has no folder.
var ErrNotFound = errors.New("automation not found")

// ErrUnreadable is an Automation whose HEARTBEAT.md cannot be read; saving
// over it repairs it.
var ErrUnreadable = errors.New("the automation's HEARTBEAT.md cannot be read")

// InvalidError is an Automation or an id that cannot be saved as given.
type InvalidError struct{ Reason string }

func (e *InvalidError) Error() string { return e.Reason }

func invalid(format string, args ...any) error {
	return &InvalidError{Reason: fmt.Sprintf(format, args...)}
}

// Store keeps Droi's Automations in Dir, one folder each. It does not lock:
// its caller serialises writes.
type Store struct {
	Dir string
}

// Folder is an Automation's folder, its runs' Workspace when it has no
// working directory.
func (s Store) Folder(id string) string { return filepath.Join(s.Dir, id) }

func checkID(id string) error {
	if id == "" || strings.HasPrefix(id, ".") || strings.HasSuffix(id, ".") || strings.HasSuffix(id, " ") ||
		strings.ContainsAny(id, `/\:`) || !filepath.IsLocal(id) {
		return invalid("%q is not an automation id", id)
	}
	return nil
}

// List is every Automation, by name. A folder whose HEARTBEAT.md is
// missing or cannot be read is one with only its ID and Invalid.
func (s Store) List() ([]Automation, error) {
	entries, err := os.ReadDir(s.Dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Automation
	for _, e := range entries {
		if !e.IsDir() || checkID(e.Name()) != nil {
			continue
		}
		a, err := s.read(e.Name())
		if errors.Is(err, fs.ErrNotExist) {
			a, err = Automation{}, errors.New("HEARTBEAT.md is missing")
		}
		if err != nil {
			a = Automation{Invalid: err.Error()}
		}
		a.ID = e.Name()
		out = append(out, a)
	}
	name := func(a Automation) string {
		if a.Name == "" {
			return strings.ToLower(a.ID)
		}
		return strings.ToLower(a.Name)
	}
	slices.SortStableFunc(out, func(a, b Automation) int {
		if c := strings.Compare(name(a), name(b)); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	return out, nil
}

// Get reads one Automation.
func (s Store) Get(id string) (Automation, error) {
	if err := checkID(id); err != nil {
		return Automation{}, err
	}
	a, err := s.read(id)
	if errors.Is(err, fs.ErrNotExist) {
		return Automation{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	if err != nil {
		return Automation{}, fmt.Errorf("%w (%s): %v", ErrUnreadable, id, err)
	}
	return a, nil
}

func (s Store) read(id string) (Automation, error) {
	b, err := os.ReadFile(filepath.Join(s.Folder(id), heartbeatFile))
	if err != nil {
		return Automation{}, err
	}
	a, err := parseHeartbeat(string(b))
	a.ID = id
	return a, err
}

// Validate is why a cannot be saved, an *InvalidError, or nil.
func Validate(a Automation) error {
	if strings.TrimSpace(a.Name) == "" {
		return invalid("an automation needs a name")
	}
	if strings.TrimSpace(a.Prompt) == "" {
		return invalid("an automation needs a prompt")
	}
	if _, err := ParseCron(a.Schedule); err != nil {
		return invalid("%s", err.Error())
	}
	if a.Timezone != "" {
		if _, err := time.LoadLocation(a.Timezone); err != nil {
			return invalid("unknown time zone %q", a.Timezone)
		}
	}
	switch a.Autonomy {
	case "", "low", "medium", "high":
	default:
		return invalid("autonomy %q is not low, medium or high", a.Autonomy)
	}
	if w := strings.TrimSpace(a.WorkingDirectory); w != "" && !filepath.IsAbs(w) {
		return invalid("the working directory %q is not a full path", a.WorkingDirectory)
	}
	return nil
}

// Clean is a as Save writes it: two that Clean makes equal run the same.
func Clean(a Automation) Automation {
	a.Name = strings.TrimSpace(a.Name)
	a.Schedule = strings.Join(strings.Fields(a.Schedule), " ")
	a.Timezone = strings.TrimSpace(a.Timezone)
	a.Prompt = strings.TrimSpace(strings.ReplaceAll(a.Prompt, "\r\n", "\n"))
	a.Model = strings.TrimSpace(a.Model)
	a.ReasoningEffort = strings.TrimSpace(a.ReasoningEffort)
	a.Autonomy = strings.TrimSpace(a.Autonomy)
	if w := strings.TrimSpace(a.WorkingDirectory); w != "" {
		a.WorkingDirectory = filepath.Clean(w)
	} else {
		a.WorkingDirectory = ""
	}
	a.ForkedFrom = strings.TrimSpace(a.ForkedFrom)
	a.ForkedFromName = strings.TrimSpace(a.ForkedFromName)
	a.Conversation = strings.TrimSpace(a.Conversation)
	a.Invalid = ""
	return a
}

// newID is the id of a new Automation called name.
func (s Store) newID(name string) (string, error) {
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return "", err
	}
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		return "", err
	}
	taken := map[string]bool{}
	for _, e := range entries {
		taken[e.Name()] = true
	}
	return ID(name, taken), nil
}

// Adopt creates an Automation in folder, a folder of Dir prepared with its
// files, which becomes the Automation's own; CreatedAt is now when zero.
func (s Store) Adopt(a Automation, folder string) (Automation, error) {
	a = Clean(a)
	if err := Validate(a); err != nil {
		return Automation{}, err
	}
	id, err := s.newID(a.Name)
	if err != nil {
		return Automation{}, err
	}
	a.ID = id
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now()
	}
	a.CreatedAt = a.CreatedAt.Truncate(time.Second)
	if err := writeAtomic(filepath.Join(folder, heartbeatFile), []byte(formatHeartbeat(a))); err != nil {
		return Automation{}, err
	}
	if err := rename(folder, s.Folder(id)); err != nil {
		return Automation{}, err
	}
	return a, nil
}

// Save writes a, creating it when its ID is empty (the ID then comes from
// its name, and CreatedAt is now when zero). An edit keeps the creation
// time when a has none.
func (s Store) Save(a Automation) (Automation, error) {
	a = Clean(a)
	created := a.ID == ""
	if err := Validate(a); err != nil {
		return Automation{}, err
	}
	if a.ID == "" {
		id, err := s.newID(a.Name)
		if err != nil {
			return Automation{}, err
		}
		a.ID = id
		if a.CreatedAt.IsZero() {
			a.CreatedAt = time.Now()
		}
		if err := os.Mkdir(s.Folder(a.ID), 0o755); err != nil {
			return Automation{}, err
		}
	} else {
		// Saving over an unreadable HEARTBEAT.md repairs it.
		old, err := s.Get(a.ID)
		if err != nil && !errors.Is(err, ErrUnreadable) {
			return Automation{}, err
		}
		if a.CreatedAt.IsZero() {
			a.CreatedAt = old.CreatedAt
		}
		if a.ForkedFrom == "" {
			a.ForkedFrom, a.ForkedFromName = old.ForkedFrom, old.ForkedFromName
		}
		if a.Conversation == "" {
			a.Conversation = old.Conversation
		}
	}
	a.CreatedAt = a.CreatedAt.Truncate(time.Second)
	if err := writeAtomic(filepath.Join(s.Folder(a.ID), heartbeatFile), []byte(formatHeartbeat(a))); err != nil {
		if created {
			_ = os.RemoveAll(s.Folder(a.ID))
		}
		return Automation{}, err
	}
	return a, nil
}

// SetPaused pauses or resumes an Automation.
func (s Store) SetPaused(id string, paused bool) error {
	a, err := s.Get(id)
	if err != nil {
		return err
	}
	a.Paused = paused
	return writeAtomic(filepath.Join(s.Folder(id), heartbeatFile), []byte(formatHeartbeat(a)))
}

// Delete removes an Automation's folder, its runs' records with it, also
// when its HEARTBEAT.md cannot be read.
func (s Store) Delete(id string) error {
	if err := checkID(id); err != nil {
		return err
	}
	if info, err := os.Stat(s.Folder(id)); errors.Is(err, fs.ErrNotExist) || err == nil && !info.IsDir() {
		return fmt.Errorf("%w: %s", ErrNotFound, id)
	} else if err != nil {
		return err
	}
	return os.RemoveAll(s.Folder(id))
}

// Runs are an Automation's run records, oldest first.
func (s Store) Runs(id string) ([]Run, error) {
	if err := checkID(id); err != nil {
		return nil, err
	}
	b, err := os.ReadFile(filepath.Join(s.Folder(id), runsFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Run
	for line := range bytes.Lines(b) {
		var r Run
		if json.Unmarshal(line, &r) == nil && r.RunID != "" {
			r.AutomationID = id
			out = append(out, r)
		}
	}
	return out, nil
}

// SaveRun adds r to the Automation's records, or replaces the one with its
// RunID, keeping the last MaxRuns. It fails when the Automation's folder is
// gone, rather than bring it back. An error longer than 4 KiB is cut.
func (s Store) SaveRun(id string, r Run) error {
	r.Error = cut(r.Error, maxError)
	runs, err := s.Runs(id)
	if err != nil {
		return err
	}
	if i := slices.IndexFunc(runs, func(x Run) bool { return x.RunID == r.RunID }); i >= 0 {
		runs[i] = r
	} else {
		runs = append(runs, r)
	}
	if len(runs) > MaxRuns {
		runs = runs[len(runs)-MaxRuns:]
	}
	var b bytes.Buffer
	for _, x := range runs {
		// The folder says whose run it is.
		x.AutomationID = ""
		line, err := json.Marshal(x)
		if err != nil {
			return err
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	return writeAtomic(filepath.Join(s.Folder(id), runsFile), b.Bytes())
}

// State reads state.json; an empty State when there is none.
func (s Store) State() (State, error) {
	st := State{LastScheduled: map[string]time.Time{}}
	b, err := os.ReadFile(filepath.Join(s.Dir, stateFile))
	if errors.Is(err, fs.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return st, err
	}
	if err := json.Unmarshal(b, &st); err != nil {
		return State{LastScheduled: map[string]time.Time{}}, fmt.Errorf("%s: %w", stateFile, err)
	}
	if st.LastScheduled == nil {
		st.LastScheduled = map[string]time.Time{}
	}
	return st, nil
}

// SaveState writes state.json.
func (s Store) SaveState(st State) error {
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return err
	}
	return writeAtomic(filepath.Join(s.Dir, stateFile), append(b, '\n'))
}

// cut is s cut to at most n bytes, at the start of a character.
func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// writeAtomic replaces file whole, so a reader never sees half of it. It
// does not make the folder.
func writeAtomic(file string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(file), "."+filepath.Base(file)+".tmp*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = rename(tmp, file)
	}
	if err != nil {
		_ = os.Remove(tmp)
	}
	return err
}

func formatHeartbeat(a Automation) string {
	var b strings.Builder
	b.WriteString("---\n")
	field := func(key, value string) {
		if value != "" {
			b.WriteString(key + ": " + yamlString(value) + "\n")
		}
	}
	field("name", a.Name)
	field("schedule", a.Schedule)
	field("timezone", a.Timezone)
	field("model", a.Model)
	field("reasoningEffort", a.ReasoningEffort)
	field("autonomy", a.Autonomy)
	field("workingDirectory", a.WorkingDirectory)
	if a.Paused {
		b.WriteString("paused: true\n")
	}
	if !a.CreatedAt.IsZero() {
		field("createdAt", a.CreatedAt.Format(time.RFC3339))
	}
	field("forkedFrom", a.ForkedFrom)
	field("forkedFromName", a.ForkedFromName)
	if a.Conversation != "" {
		field("target", "existing")
		field("conversation", a.Conversation)
	}
	b.WriteString("---\n\n")
	b.WriteString(strings.TrimSpace(a.Prompt))
	b.WriteString("\n")
	return b.String()
}

// yamlString is value as a YAML scalar: plain when YAML reads it back as the
// same string, else double-quoted (a JSON string is a YAML one).
func yamlString(value string) string {
	plain := value == strings.TrimSpace(value) &&
		!strings.ContainsAny(value[:1], "-?:,[]{}#&*!|>'\"%@`") &&
		!strings.Contains(value, ": ") && !strings.Contains(value, " #") && !strings.HasSuffix(value, ":") &&
		!strings.ContainsFunc(value, func(r rune) bool { return r < ' ' || r == 0x7f })
	if plain {
		switch strings.ToLower(value) {
		case "true", "false", "yes", "no", "on", "off", "null", "~", "y", "n":
			plain = false
		}
		if _, err := strconv.ParseFloat(value, 64); err == nil {
			plain = false
		}
	}
	if plain {
		return value
	}
	b, _ := json.Marshal(value)
	return string(b)
}

// parseHeartbeat reads the front matter's top-level "key: value" lines that
// Droi uses, leaving other keys and nested values alone, and the prompt
// below it.
func parseHeartbeat(text string) (Automation, error) {
	text = strings.TrimPrefix(strings.ReplaceAll(text, "\r\n", "\n"), "\ufeff")
	lines := strings.Split(text, "\n")
	fence := func(l string) bool { return strings.TrimRight(l, " \t") == "---" }
	if !fence(lines[0]) {
		return Automation{}, errors.New("HEARTBEAT.md has no front matter")
	}
	end := slices.IndexFunc(lines[1:], fence) + 1
	if end == 0 {
		return Automation{}, errors.New("HEARTBEAT.md's front matter has no end")
	}
	var a Automation
	for _, line := range lines[1:end] {
		if line == "" || line[0] == ' ' || line[0] == '\t' || line[0] == '#' || line[0] == '-' {
			continue
		}
		key, raw, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		v := yamlValue(raw)
		switch strings.TrimSpace(key) {
		case "name":
			a.Name = v
		case "schedule":
			a.Schedule = v
		case "timezone":
			a.Timezone = v
		case "model":
			a.Model = v
		case "reasoningEffort":
			a.ReasoningEffort = v
		case "autonomy":
			a.Autonomy = v
		case "workingDirectory":
			a.WorkingDirectory = v
		case "paused":
			a.Paused = strings.EqualFold(v, "true")
		case "createdAt":
			a.CreatedAt, _ = time.Parse(time.RFC3339, v)
		case "forkedFrom":
			a.ForkedFrom = v
		case "forkedFromName":
			a.ForkedFromName = v
		case "conversation":
			a.Conversation = v
		}
	}
	if a.Name == "" || a.Schedule == "" {
		return Automation{}, errors.New("HEARTBEAT.md needs a name and a schedule")
	}
	a.Prompt = strings.TrimSpace(strings.Join(lines[end+1:], "\n"))
	return a, nil
}

// yamlValue reads a scalar: double- or single-quoted, or plain up to a
// comment. A block scalar ("|", ">") reads as "".
func yamlValue(raw string) string {
	v := strings.TrimSpace(raw)
	switch {
	case v == "":
		return ""
	case v[0] == '"':
		for i := 1; i < len(v); i++ {
			switch v[i] {
			case '\\':
				i++
			case '"':
				if s, err := strconv.Unquote(v[:i+1]); err == nil {
					return s
				}
				return v[1:i]
			}
		}
		return strings.Trim(v, `"`)
	case v[0] == '\'':
		var b strings.Builder
		for i := 1; i < len(v); i++ {
			if v[i] == '\'' {
				if i+1 < len(v) && v[i+1] == '\'' {
					b.WriteByte('\'')
					i++
					continue
				}
				break
			}
			b.WriteByte(v[i])
		}
		return b.String()
	case v[0] == '|' || v[0] == '>':
		return ""
	}
	if i := strings.Index(v, " #"); i >= 0 {
		v = strings.TrimSpace(v[:i])
	}
	return v
}
