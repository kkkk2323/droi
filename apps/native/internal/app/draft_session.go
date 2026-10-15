package app

import (
	"time"

	"github.com/kkkk2323/droid-sdk-go/protocol"
	"github.com/kkkk2323/droid-sdk-go/session"

	"github.com/kkkk2323/droi/apps/native/internal/defaults"
	"github.com/kkkk2323/droi/apps/native/internal/sessions"
	"github.com/kkkk2323/droi/apps/native/internal/slash"
)

// draftSession is the New session page's Draft Session: the Daemon lists
// skills and commands per Session only, and the composer wants them before
// the first send. It is opened with the tool mode the page asks for, which a
// Session keeps for good, so another mode opens another draft. Fields but
// path and mode are guarded by newSessionState.mu.
type draftSession struct {
	path string
	mode defaults.ToolMode
	id   string // "" until the Daemon answered
	// loaded is set once the store has the draft loaded, which it may
	// record a moment after the Daemon answered.
	loaded  bool
	slash   []slash.Item
	slashAt time.Time
}

// draftItems are the commands and skills of the Workspace's draft, opened
// (or opened again) when there is none usable. A Scratch Workspace gets
// none: its folder is only made when the Session starts.
func (a *App) draftItems(workspace string, mode defaults.ToolMode) []slash.Item {
	s := &a.newPage
	if workspace == "" || workspace == scratchPick || a.ctl == nil || !a.ctl.Status().Connected {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// A send under way has taken the draft, or is about to create a Session.
	if s.creating {
		return nil
	}
	d := s.draft
	// A dropped connection leaves every Session not loaded; the Daemon may
	// have restarted without the draft.
	lost := false
	if d != nil && d.id != "" {
		now := a.ctl.Store().LoadState(d.id) == session.Loaded
		lost = d.loaded && !now
		d.loaded = d.loaded || now
	}
	if d == nil || d.path != workspace || d.mode != mode || lost {
		if d != nil {
			a.closeDraft(d.id)
		}
		d = &draftSession{path: workspace, mode: mode, slashAt: time.Now()}
		s.draft = d
		go a.openDraft(d)
		return nil
	}
	if d.id != "" && time.Since(d.slashAt) > time.Minute {
		d.slashAt = time.Now()
		go a.readDraftItems(d)
	}
	return d.slash
}

func (a *App) openDraft(d *draftSession) {
	s := &a.newPage
	cl, err := a.ctl.Client()
	if err != nil {
		return
	}
	// An unusable directory is reported by the ordinary create on send.
	cwd, err := a.validWorkspace(cl, d.path)
	if err != nil {
		return
	}
	p := protocol.InitializeSessionParams{Cwd: cwd, ToolExecutionMode: protocol.ToolExecutionMode(d.mode),
		Tags: []protocol.SessionTag{{Name: sessions.DraftTag}}}
	res, err := a.ctl.InitializeSession(a.ctx, p)
	if err != nil {
		return
	}
	s.mu.Lock()
	d.id = res.SessionID
	current := s.draft == d
	s.mu.Unlock()
	if !current {
		a.closeDraft(res.SessionID)
		return
	}
	a.readDraftItems(d)
}

// readDraftItems asks the Daemon for the draft's commands and skills; a
// failed read is tried again 5 seconds later.
func (a *App) readDraftItems(d *draftSession) {
	s := &a.newPage
	s.mu.Lock()
	id := d.id
	s.mu.Unlock()
	failed := func() {
		s.mu.Lock()
		d.slashAt = time.Now().Add(5*time.Second - time.Minute)
		s.mu.Unlock()
	}
	cl, err := a.ctl.Client()
	if err != nil {
		failed()
		return
	}
	cmds, err1 := cl.ListCommands(a.ctx, protocol.ListCommandsParams{SessionID: id})
	sk, err2 := cl.ListSkills(a.ctx, protocol.ListSkillsParams{SessionID: id})
	if err1 != nil || err2 != nil {
		failed()
		return
	}
	s.mu.Lock()
	d.slash = slash.FromDaemon(cmds.Commands, skillsShown(sk.Skills))
	s.mu.Unlock()
	a.redraw()
}

// closeDraft closes a draft no one will take; the Daemon deletes a Session
// nothing was written to.
func (a *App) closeDraft(id string) {
	if id == "" {
		return
	}
	go func() { _ = a.ctl.CloseSession(a.ctx, protocol.CloseSessionParams{SessionID: id}) }()
}

// dropDraft closes the page's draft: the page was left without sending.
func (a *App) dropDraft() {
	s := &a.newPage
	s.mu.Lock()
	d := s.draft
	s.draft = nil
	s.mu.Unlock()
	if d != nil {
		a.closeDraft(d.id)
	}
}

// takeDraft turns the draft into the Session p asks for: it drops the draft's
// tag and applies the page's settings. It returns "" when the draft cannot
// be that Session (another Workspace or tool mode, a worktree, which only a
// new Session gets, or none opened yet); the draft is closed then and the
// caller creates the Session itself.
func (a *App) takeDraft(workspace string, p protocol.InitializeSessionParams) string {
	s := &a.newPage
	s.mu.Lock()
	d := s.draft
	s.draft = nil
	id := ""
	if d != nil {
		id = d.id
	}
	s.mu.Unlock()
	if d == nil {
		return ""
	}
	if id == "" || d.path != workspace || string(d.mode) != string(p.ToolExecutionMode) || p.Worktree != nil ||
		a.ctl.Store().LoadState(id) != session.Loaded {
		a.closeDraft(id)
		return ""
	}
	cl, err := a.ctl.Client()
	if err != nil {
		a.closeDraft(id)
		return ""
	}
	// A map, not UpdateSessionSettingsParams: its tags are omitempty, and the
	// empty list is what drops the draft's tag.
	update := map[string]any{"sessionId": id, "tags": []protocol.SessionTag{}}
	if p.ModelID != "" {
		update["modelId"] = p.ModelID
	}
	if p.ReasoningEffort != "" {
		update["reasoningEffort"] = p.ReasoningEffort
	}
	if p.AutonomyLevel != "" {
		update["autonomyLevel"] = p.AutonomyLevel
	}
	if err := cl.Call(a.ctx, protocol.MethodUpdateSessionSettings, update, &protocol.UpdateSessionSettingsResult{}); err != nil {
		a.closeDraft(id)
		return ""
	}
	return id
}
