package workstream

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Workstream is the Host's record of one Workstream:
// <dir>/<id>/workstream.json, beside its Shared Folder <dir>/<id>/shared.
type Workstream struct {
	ID             string `json:"id"`
	Title          string `json:"title"`
	Workspace      string `json:"workspace"`
	OrchestratorID string `json:"orchestratorId"`
	// AgentModel and AgentReasoningEffort are the agents' settings the user
	// picked; "" is the orchestrator's.
	AgentModel           string   `json:"agentModel,omitempty"`
	AgentReasoningEffort string   `json:"agentReasoningEffort,omitempty"`
	CreatedAt            string   `json:"createdAt"`
	ArchivedAt           string   `json:"archivedAt,omitempty"`
	Agents               []*Agent `json:"agents"`
}

// Agent is one of a Workstream's agents.
type Agent struct {
	SessionID string `json:"sessionId"`
	Title     string `json:"title"`
	// WorktreePath and Branch are "" for an agent in a Workspace that is no
	// git repository; BaseBranch is what the branch was cut from.
	WorktreePath string `json:"worktreePath,omitempty"`
	Branch       string `json:"branch,omitempty"`
	BaseBranch   string `json:"baseBranch,omitempty"`
	// Model and ReasoningEffort are the settings it last took up, also on
	// its tag: a Session whose settings differ was switched by hand.
	Model           string `json:"model,omitempty"`
	ReasoningEffort string `json:"reasoningEffort,omitempty"`
	CreatedAt       string `json:"createdAt"`
	ArchivedAt      string `json:"archivedAt,omitempty"`
	// Awaiting is set while the orchestrator waits on the agent: from a
	// message the Host sent it to the end of the turn it started.
	Awaiting bool `json:"awaiting"`
	// ReportedThisTurn is set once it reported in its current turn.
	ReportedThisTurn bool    `json:"reportedThisTurn"`
	LastReport       *Report `json:"lastReport,omitempty"`
}

// Report is what an agent reported last.
type Report struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
	At   string `json:"at"`
}

const recordFile = "workstream.json"

// SharedDir is the Shared Folder in a Workstream's folder.
const SharedDir = "shared"

// loadAll reads every Workstream record under dir; one that cannot be read
// is left out.
func loadAll(dir string, log func(string)) map[string]*Workstream {
	out := map[string]*Workstream{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name(), recordFile))
		if err != nil {
			continue
		}
		var w Workstream
		if err := json.Unmarshal(b, &w); err != nil || w.ID != e.Name() {
			log("a Workstream record that cannot be read: " + filepath.Join(dir, e.Name(), recordFile))
			continue
		}
		out[w.ID] = &w
	}
	return out
}

// save writes a record whole, so a crash never leaves half of one.
func save(dir string, w *Workstream) error {
	b, err := json.MarshalIndent(w, "", "  ")
	if err != nil {
		return err
	}
	folder := filepath.Join(dir, w.ID)
	tmp, err := os.CreateTemp(folder, recordFile+".*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), filepath.Join(folder, recordFile)); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}

// newID is a Workstream's id: its title's ASCII letters and digits, then a
// random suffix.
func newID(title string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(title) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r)
			dash = false
		default:
			dash = true
		}
		if b.Len() >= 40 {
			break
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		slug = "workstream"
	}
	suffix := make([]byte, 3)
	_, _ = rand.Read(suffix)
	return slug + "-" + hex.EncodeToString(suffix)
}
