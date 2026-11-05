package workstream

import (
	"context"
	"os"
	"path/filepath"
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
	// none; a transcript's first line never changes.
	parents map[string]string
}

func newLineage(dir string) *lineage {
	return &lineage{dir: dir, folders: map[string]string{}, parents: map[string]string{}}
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

// successor is the Session id was last handed off to; "" for none.
func (l *lineage) successor(id string) string {
	file := l.transcript(id)
	if file == "" {
		return ""
	}
	folder := filepath.Dir(file)
	entries, err := os.ReadDir(folder)
	if err != nil {
		return ""
	}
	var (
		next   string
		newest time.Time
	)
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
			next, newest = strings.TrimSuffix(name, ".jsonl"), info.ModTime()
		}
	}
	if next != "" {
		l.mu.Lock()
		l.folders[next] = folder
		l.mu.Unlock()
	}
	return next
}

// newestLink follows the handoffs from the Session id, of the Workstream
// wsID in role, as far as they go; id when there are none.
func (c *Coordinator) newestLink(wsID, id, role string) string {
	for range maxLinks {
		next := c.links.successor(id)
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

// newestOrchestrator is w's orchestrator, taken over first from the Session
// it was handed off to, if any.
func (c *Coordinator) newestOrchestrator(ctx context.Context, w *Workstream) string {
	c.mu.Lock()
	id := w.OrchestratorID
	c.mu.Unlock()
	if next := c.newestLink(w.ID, id, sessions.WorkstreamOrchestrator); next != id {
		c.takeOver(ctx, w, id, next)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return w.OrchestratorID
}

// newestAgent is the Session of w's agent a, taken over first from the
// Session it was handed off to, if any.
func (c *Coordinator) newestAgent(ctx context.Context, w *Workstream, a *Agent) string {
	c.mu.Lock()
	id := a.SessionID
	c.mu.Unlock()
	if next := c.newestLink(w.ID, id, sessions.WorkstreamAgent); next != id {
		c.takeOver(ctx, w, id, next)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return a.SessionID
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
// prev's place, and follows it instead.
func (c *Coordinator) takeOver(ctx context.Context, w *Workstream, prev, next string) {
	c.mu.Lock()
	switch a := agentIn(w, prev); {
	case w.OrchestratorID == prev:
		w.OrchestratorID = next
	case a != nil:
		a.PreviousSessionIDs = append(a.PreviousSessionIDs, prev)
		a.SessionID = next
	default:
		c.mu.Unlock()
		return
	}
	c.activity[next] = c.activity[prev]
	c.save(w)
	c.mu.Unlock()
	c.cfg.Log("following " + prev + ", handed off to " + next)
	if err := c.cfg.Sessions.Load(ctx, next); err != nil {
		c.cfg.Log("following " + next + ": " + err.Error())
	}
	c.cfg.Sessions.Unfollow(prev)
}
