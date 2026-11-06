package workstream

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/kkkk2323/droi/apps/native/internal/memory"
	"github.com/kkkk2323/droi/apps/native/pkg/sessions"
)

// A Session the user hands off (/handoff) goes on in a new Session: droid
// creates it with the old one's tags, in the same Workspace folder, and
// names the old one as its parent on the session_start line of its
// transcript (SessionService.createInactiveSessionWithId). The Daemon
// answers the new id only to the Client that asked and tells no one else,
// and its session lists carry no parent, so the Host reads the transcripts
// the Daemon keeps.

// maxLinks bounds how many handoffs the Host follows from one Session.
const maxLinks = 32

// lineage reads which Session was handed off to which.
type lineage struct {
	// dir is the Daemon's sessions folder; "" follows no handoff.
	dir string

	mu sync.Mutex
	// folders are the folders of the transcripts found.
	folders map[string]string
	// parents are the Sessions transcripts were handed off from, "" for
	// none or a fork. Droid rewrites a transcript's first line for its title
	// and lastCwd, but never its parent.
	parents map[string]string
	// settings are the settings files read, by file, until they change.
	settings map[string]settingsRead
}

// settingsRead is what the Host reads of a Session's settings file.
type settingsRead struct {
	// ToolMode and ToolModeModel are the tool execution mode the Daemon
	// resolved and the model it did so for; Model is the Session's model.
	ToolMode, ToolModeModel, Model string

	size int64
	mod  time.Time
}

func newLineage(dir string) *lineage {
	return &lineage{dir: dir, folders: map[string]string{}, parents: map[string]string{}, settings: map[string]settingsRead{}}
}

// sessionSettings reads the Session id's settings file, which the Daemon
// writes for every change, a tool mode that droid resolved again at a turn's
// boundary included: the Daemon sends the mode to no one, and the SDK's
// Session keeps none. false when there is no such file or it cannot be read.
func (l *lineage) sessionSettings(id string) (settingsRead, bool) {
	file := l.transcript(id)
	if file == "" {
		return settingsRead{}, false
	}
	file = strings.TrimSuffix(file, ".jsonl") + ".settings.json"
	info, err := os.Stat(file)
	if err != nil {
		return settingsRead{}, false
	}
	l.mu.Lock()
	known, ok := l.settings[file]
	l.mu.Unlock()
	if ok && known.size == info.Size() && known.mod.Equal(info.ModTime()) {
		return known, true
	}
	b, err := os.ReadFile(file)
	if err != nil {
		return settingsRead{}, false
	}
	var s struct {
		Model                    string `json:"model"`
		ToolExecutionMode        string `json:"toolExecutionMode"`
		ToolExecutionModeModelID string `json:"toolExecutionModeModelId"`
	}
	if json.Unmarshal(b, &s) != nil {
		return settingsRead{}, false
	}
	read := settingsRead{ToolMode: s.ToolExecutionMode, ToolModeModel: s.ToolExecutionModeModelID, Model: s.Model, size: info.Size(), mod: info.ModTime()}
	l.mu.Lock()
	l.settings[file] = read
	l.mu.Unlock()
	return read, true
}

// transcript is the Session id's transcript; "" when there is none.
func (l *lineage) transcript(id string) string {
	if l.dir == "" || id == "" {
		return ""
	}
	l.mu.Lock()
	folder := l.folders[id]
	l.mu.Unlock()
	if folder != "" {
		return filepath.Join(folder, id+".jsonl")
	}
	file := memory.FindSessionTranscript(l.dir, id)
	if file != "" {
		l.mu.Lock()
		l.folders[id] = filepath.Dir(file)
		l.mu.Unlock()
	}
	return file
}

func (l *lineage) parentIn(file string) string {
	l.mu.Lock()
	parent, ok := l.parents[file]
	l.mu.Unlock()
	if ok {
		return parent
	}
	parent, whole := memory.ReadSessionParent(file)
	if whole {
		l.mu.Lock()
		l.parents[file] = parent
		l.mu.Unlock()
	}
	return parent
}

// parent is the Session id was handed off from; "" for none.
func (l *lineage) parent(id string) string {
	if file := l.transcript(id); file != "" {
		return l.parentIn(file)
	}
	return ""
}

// tag is the metadata of the Session id's droi.workstream tag.
func (l *lineage) tag(id string) (map[string]string, bool) {
	file := l.transcript(id)
	if file == "" {
		return nil, false
	}
	return memory.SessionTagMetadata(file, sessions.WorkstreamTag)
}

// successor is the Session id was last handed off to; "" for none. droid
// writes the new transcript in the folder of the Session's cwd as it is
// then, so cwd, when the Host knows it, is looked in too: /cwd may have
// moved the Session from the folder of its own transcript.
func (l *lineage) successor(id, cwd string) string {
	var folders []string
	if file := l.transcript(id); file != "" {
		folders = append(folders, filepath.Dir(file))
	}
	if cwd != "" && l.dir != "" {
		if folder := memory.SessionFolder(l.dir, cwd); !slices.Contains(folders, folder) {
			folders = append(folders, folder)
		}
	}
	var (
		next, nextFolder string
		newest           time.Time
	)
	for _, folder := range folders {
		entries, err := os.ReadDir(folder)
		if err != nil {
			continue
		}
		for _, e := range entries {
			name := e.Name()
			if !strings.HasSuffix(name, ".jsonl") || name == id+".jsonl" || l.parentIn(filepath.Join(folder, name)) != id {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			if next == "" || info.ModTime().After(newest) {
				next, nextFolder, newest = strings.TrimSuffix(name, ".jsonl"), folder, info.ModTime()
			}
		}
	}
	if next != "" {
		l.mu.Lock()
		l.folders[next] = nextFolder
		l.mu.Unlock()
	}
	return next
}

// newestLink follows the handoffs from the Session id, of the Workstream
// wsID in role, as far as they go; id when there are none.
func (c *Coordinator) newestLink(wsID, id, role string) string {
	for range maxLinks {
		cwd := ""
		if s := c.cfg.Sessions.Session(id); s != nil {
			cwd = s.Cwd()
		}
		next := c.links.successor(id, cwd)
		if next == "" {
			break
		}
		if md, ok := c.links.tag(next); !ok || md["workstreamId"] != wsID || md["role"] != role {
			break
		}
		id = next
	}
	return id
}

// newest is the Session of w's agent a, or of its orchestrator when a is
// nil, taken over first from the Session it was handed off to, if any.
func (c *Coordinator) newest(ctx context.Context, w *Workstream, a *Agent) string {
	role, current := sessions.WorkstreamOrchestrator, func() string { return w.OrchestratorID }
	if a != nil {
		role, current = sessions.WorkstreamAgent, func() string { return a.SessionID }
	}
	c.mu.Lock()
	id := current()
	c.mu.Unlock()
	if next := c.newestLink(w.ID, id, role); next != id {
		c.takeOver(ctx, w, id, next)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return current()
}

// adopt takes over, for a tool call from the Session id the Host has no
// record of, the orchestrator or agent it was handed off from: one of the
// Workstream its tag names, in the role it names. It reports whether it did.
func (c *Coordinator) adopt(ctx context.Context, id, role string) bool {
	md, ok := c.links.tag(id)
	if !ok || md["role"] != role {
		return false
	}
	c.mu.Lock()
	w := c.all[md["workstreamId"]]
	c.mu.Unlock()
	if w == nil {
		return false
	}
	prev := id
	for range maxLinks {
		if prev = c.links.parent(prev); prev == "" {
			return false
		}
		c.mu.Lock()
		recorded := w.ArchivedAt == "" && (role == sessions.WorkstreamOrchestrator && w.OrchestratorID == prev ||
			role == sessions.WorkstreamAgent && activeAgent(w, prev) != nil)
		c.mu.Unlock()
		if recorded {
			c.takeOver(ctx, w, prev, id)
			return true
		}
	}
	return false
}

func activeAgent(w *Workstream, id string) *Agent {
	if a := agentIn(w, id); a != nil && a.ArchivedAt == "" {
		return a
	}
	return nil
}

// takeOver records the Session next, which prev was handed off to, in
// prev's place, and follows it instead, without the tools its role does
// without: droid hands off with none hidden.
func (c *Coordinator) takeOver(ctx context.Context, w *Workstream, prev, next string) {
	var hidden []string
	c.mu.Lock()
	switch a := agentIn(w, prev); {
	case w.OrchestratorID == prev:
		w.PreviousOrchestratorIDs = append(w.PreviousOrchestratorIDs, prev)
		w.OrchestratorID = next
		hidden = orchestratorHidden()
	case a != nil:
		a.PreviousSessionIDs = append(a.PreviousSessionIDs, prev)
		a.SessionID = next
		hidden = agentHidden()
	default:
		c.mu.Unlock()
		return
	}
	c.activity[next] = c.activity[prev]
	c.save(w)
	c.mu.Unlock()
	c.cfg.Log("following " + prev + ", handed off to " + next)
	if err := c.cfg.Sessions.Load(ctx, next, hidden...); err != nil {
		c.cfg.Log("following " + next + ": " + err.Error())
	}
	c.cfg.Sessions.Unfollow(prev)
}
