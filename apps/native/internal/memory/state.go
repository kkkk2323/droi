package memory

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// State is the Host's bookkeeping in state.json. Only the Host writes it;
// the hook reads it to tell which Memory Notes the Memory Summary has not
// taken in yet.
type State struct {
	// ImportedAt is when the entries of the earlier Memory (memory.sqlite)
	// were brought in; "" until then.
	ImportedAt string `json:"importedAt,omitempty"`
	// Sessions are the Sessions the hook recorded, by id.
	Sessions map[string]*SessionRecord `json:"sessions"`
	// Summaries are the Session Summaries, by path in the folder.
	Summaries map[string]*SummaryRecord `json:"summaries"`
	// Phase2 is the Memory Summary's.
	Phase2 Phase2Record `json:"phase2"`
	// Runs are the latest Memory Sessions, newest first.
	Runs []RunRecord `json:"runs"`
	// LastPassAt is when the Host last looked for work.
	LastPassAt string `json:"lastPassAt,omitempty"`
}

// SessionRecord is what the Host knows of one Session.
type SessionRecord struct {
	Transcript string `json:"transcript"`
	Cwd        string `json:"cwd"`
	StartedAt  string `json:"startedAt"`
	// SummarizedMtime is the transcript's modification time (Unix ms) when
	// it was last summarized; a later one means the Session went on.
	SummarizedMtime int64  `json:"summarizedMtime,omitempty"`
	SummarizedAt    string `json:"summarizedAt,omitempty"`
	// File is its Session Summary, "" when it has none.
	File string `json:"file,omitempty"`
	// Skipped says why it is never summarized, such as "subagent".
	Skipped  string `json:"skipped,omitempty"`
	Failures int    `json:"failures,omitempty"`
	RetryAt  string `json:"retryAt,omitempty"`
}

// SummaryRecord is what the Host knows of one Session Summary.
type SummaryRecord struct {
	SessionID string `json:"sessionId,omitempty"`
	// UpdatedAt is when its Session last changed.
	UpdatedAt string `json:"updatedAt"`
	// GeneratedAt is when it was written.
	GeneratedAt string `json:"generatedAt"`
	Reads       int    `json:"reads,omitempty"`
	LastRead    string `json:"lastRead,omitempty"`
	// Imported marks a file made from the earlier Memory.
	Imported bool `json:"imported,omitempty"`
}

// Phase2Record is the Memory Summary's bookkeeping.
type Phase2Record struct {
	// Inputs are the SHA-256 of every file the Memory Summary has taken in,
	// by path: a file missing or different is new to it.
	Inputs      map[string]string `json:"inputs"`
	UpdatedAt   string            `json:"updatedAt,omitempty"`
	RetryAt     string            `json:"retryAt,omitempty"`
	LastError   string            `json:"lastError,omitempty"`
	LastErrorAt string            `json:"lastErrorAt,omitempty"`
}

// The kinds of Memory Session.
const (
	RunSessionSummary = "session-summary"
	RunMemorySummary  = "memory-summary"
)

// RunRecord is one Memory Session, which the user can open.
type RunRecord struct {
	SessionID string `json:"sessionId,omitempty"`
	Title     string `json:"title"`
	Kind      string `json:"kind"`
	Model     string `json:"model"`
	StartedAt string `json:"startedAt"`
	EndedAt   string `json:"endedAt"`
	Error     string `json:"error,omitempty"`
}

// MaxRuns is how many Memory Sessions the state keeps.
const MaxRuns = 40

// LoadState reads state.json; a missing or unreadable one is empty.
func LoadState(dir string) State {
	var s State
	if b, err := os.ReadFile(filepath.Join(dir, StateFile)); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	if s.Sessions == nil {
		s.Sessions = map[string]*SessionRecord{}
	}
	if s.Summaries == nil {
		s.Summaries = map[string]*SummaryRecord{}
	}
	if s.Phase2.Inputs == nil {
		s.Phase2.Inputs = map[string]string{}
	}
	return s
}

// SaveState writes state.json whole.
func SaveState(dir string, s State) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if len(b) == 0 {
		return errors.New("empty state")
	}
	return WriteFileAtomic(filepath.Join(dir, StateFile), b)
}

// AddRun keeps a Memory Session, newest first.
func (s *State) AddRun(r RunRecord) {
	s.Runs = append([]RunRecord{r}, s.Runs...)
	if len(s.Runs) > MaxRuns {
		s.Runs = s.Runs[:MaxRuns]
	}
}
