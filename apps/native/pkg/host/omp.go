package host

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/kkkk2323/droi/apps/native/pkg/omp"
	"github.com/kkkk2323/droi/apps/native/pkg/sessions"
)

// OMPTag marks Sessions that run on oh-my-pi instead of the Droid Daemon.
const OMPTag = "droi.engine.omp"

// OMPEngine is the value stored with OMPTag.
const OMPEngine = "omp"

// IsOMP reports that summary is an OMP Session.
func IsOMP(s sessions.Summary) bool {
	for _, t := range s.Tags {
		if t.Name == OMPTag {
			return true
		}
	}
	return false
}

// OMPCreate starts a new OMP Session under cwd.
type OMPCreate struct {
	Cwd          string
	Model        string
	ApprovalMode omp.ApprovalMode
	Title        string
	// Resume attaches to an existing OMP session id or file.
	Resume string
	// ID overrides the Droi-side id; a new UUID when empty.
	ID string
}

// OMPHandle is the Host's view of one live OMP Session.
type OMPHandle struct {
	ID        string
	Cwd       string
	Title     string
	Model     string
	CreatedAt time.Time
	UpdatedAt time.Time
	Path      string // on-disk session file when known
	Session   *omp.Session
}

// OMPManager keeps live OMP children and a disk index of past ones.
type OMPManager struct {
	// Locate finds omp; Host.LocateOMP when nil.
	Locate func() string
	// Home is the user home for default session storage.
	Home string
	// UserData holds Droi's omp-index.json.
	UserData string
	// SessionRoot overrides ~/.omp/agent.
	SessionRoot string
	// Spawn is forwarded to omp.Start.
	Spawn func(ctx context.Context, path string, args []string, env []string) (omp.Process, error)
	// Now is the clock.
	Now func() time.Time
	// OnChange hears live session list/message changes.
	OnChange func()

	mu    sync.Mutex
	live  map[string]*OMPHandle
	index map[string]sessions.Summary // Droi-side id → summary
}

func (m *OMPManager) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

func (m *OMPManager) locate() string {
	if m.Locate != nil {
		return m.Locate()
	}
	return omp.Locate("", "", m.Home)
}

func (m *OMPManager) root() string {
	if m.SessionRoot != "" {
		return m.SessionRoot
	}
	return omp.DefaultSessionRoot(m.Home)
}

// Available reports that an omp binary can be found.
func (m *OMPManager) Available() bool { return m.locate() != "" }

// Create starts a new OMP child and registers it.
func (m *OMPManager) Create(ctx context.Context, c OMPCreate) (*OMPHandle, error) {
	id := c.ID
	if id == "" {
		id = uuid.NewString()
	}
	mode := c.ApprovalMode
	if mode == "" {
		mode = omp.ApprovalAlwaysAsk
	}
	opts := omp.Options{
		Path:         m.locate(),
		Cwd:          c.Cwd,
		Mode:         "rpc-ui",
		ApprovalMode: mode,
		Model:        c.Model,
		Resume:       c.Resume,
		Spawn:        m.Spawn,
	}
	// Only override storage when the Host keeps OMP sessions of its own.
	if m.SessionRoot != "" {
		opts.SessionDir = m.SessionRoot
	}
	sess, err := omp.Start(ctx, opts)
	if err != nil {
		return nil, err
	}
	h := &OMPHandle{
		ID:        id,
		Cwd:       c.Cwd,
		Title:     c.Title,
		Model:     c.Model,
		CreatedAt: m.now(),
		UpdatedAt: m.now(),
		Session:   sess,
	}
	if h.Title == "" {
		h.Title = "OMP session"
	}
	unsub := sess.OnEvent(func(ev omp.Event) {
		m.mu.Lock()
		h.UpdatedAt = m.now()
		if t := sess.Title(); t != "" {
			h.Title = t
		}
		if md := sess.Model(); md != "" {
			h.Model = md
		}
		sum := m.index[id]
		sum.Title = h.Title
		sum.UpdatedAt = h.UpdatedAt.Unix()
		m.index[id] = sum
		m.mu.Unlock()
		if m.OnChange != nil {
			m.OnChange()
		}
	})
	_ = unsub // kept for process lifetime; Close drops the session.
	m.mu.Lock()
	if m.live == nil {
		m.live = map[string]*OMPHandle{}
	}
	if m.index == nil {
		m.index = map[string]sessions.Summary{}
	}
	m.live[id] = h
	m.index[id] = sessions.Summary{
		SessionID: id,
		Title:     h.Title,
		Cwd:       c.Cwd,
		RepoRoot:  c.Cwd,
		UpdatedAt: h.UpdatedAt.Unix(),
		Tags:      []sessions.Tag{{Name: OMPTag, Metadata: map[string]string{"engine": OMPEngine}}},
	}
	m.mu.Unlock()
	if m.OnChange != nil {
		m.OnChange()
	}
	return h, nil
}

// Get returns a live handle.
func (m *OMPManager) Get(id string) *OMPHandle {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.live[id]
}

// Close ends a live Session.
func (m *OMPManager) Close(id string) error {
	m.mu.Lock()
	h := m.live[id]
	delete(m.live, id)
	m.mu.Unlock()
	if h == nil || h.Session == nil {
		return nil
	}
	return h.Session.Close()
}

// CloseAll ends every live child.
func (m *OMPManager) CloseAll() {
	m.mu.Lock()
	ids := make([]string, 0, len(m.live))
	for id := range m.live {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		_ = m.Close(id)
	}
}

// LiveSummaries are the OMP Sessions still running on this Host.
func (m *OMPManager) LiveSummaries() []sessions.Summary {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]sessions.Summary, 0, len(m.live))
	for id := range m.live {
		if s, ok := m.index[id]; ok {
			out = append(out, s)
		}
	}
	return out
}

// DiskSummaries lists OMP sessions on disk as Droi summaries. IDs are the
// OMP session ids; the window opens them by resuming.
func (m *OMPManager) DiskSummaries() []sessions.Summary {
	list, err := omp.ListSessions(m.root())
	if err != nil {
		return nil
	}
	out := make([]sessions.Summary, 0, len(list))
	m.mu.Lock()
	live := m.live
	m.mu.Unlock()
	for _, d := range list {
		if live != nil {
			// Prefer live handles when the same OMP id is already open under a Droi id.
			skip := false
			for _, h := range live {
				if h.Path == d.Path || (h.Session != nil && d.ID != "" && h.ID == d.ID) {
					skip = true
					break
				}
			}
			if skip {
				continue
			}
		}
		out = append(out, sessions.Summary{
			SessionID: "omp:" + d.ID,
			Title:     d.Title,
			Cwd:       d.Cwd,
			RepoRoot:  d.Cwd,
			UpdatedAt: d.UpdatedAt.Unix(),
			Tags:      []sessions.Tag{{Name: OMPTag, Metadata: map[string]string{"engine": OMPEngine, "ompId": d.ID, "path": d.Path}}},
		})
	}
	return out
}

// Resume opens a disk OMP session (id without omp: prefix or full path).
func (m *OMPManager) Resume(ctx context.Context, ompIDOrPath, cwd string) (*OMPHandle, error) {
	return m.Create(ctx, OMPCreate{
		Cwd:    cwd,
		Resume: ompIDOrPath,
		Title:  "OMP session",
	})
}

// OMP returns the Host's OMP manager, creating it lazily.
func (h *Host) OMP() *OMPManager {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.omp == nil {
		h.omp = &OMPManager{
			Home:     h.cfg.Home,
			UserData: h.cfg.UserData,
			Locate: func() string {
				st := h.Settings.Get()
				return omp.Locate(deref(st.OMPPath), h.cfg.Env("PATH"), h.cfg.Home)
			},
			OnChange: h.changed,
		}
	}
	return h.omp
}

// LocateOMP finds the omp binary from settings and PATH.
func (h *Host) LocateOMP() string {
	return h.OMP().locate()
}
