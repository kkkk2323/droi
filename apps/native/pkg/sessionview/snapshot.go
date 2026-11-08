package sessionview

import (
	"github.com/kkkk2323/droid-sdk-go/protocol"
	"github.com/kkkk2323/droid-sdk-go/session"

	"github.com/kkkk2323/droi/apps/native/pkg/kernel"
)

// Snapshot is what one frame draws of a conversation besides its rows: whether it loaded, whether a run is in
// progress, whether older pages exist, and the Daemon session behind it while that path is still in use.
//
// It exists so the drawing does not ask where the conversation came from. A row does not care whether the entries
// were folded out of the Daemon's messages or handed over by Kernel's view; both become the same snapshot, and the
// switch from one to the other moves one function.
type Snapshot struct {
	// Loaded says there is something to draw: Kernel answered a view, or the Daemon's session is loaded.
	Loaded bool
	// LoadErr is why there is not, as the app shows it.
	LoadErr string
	// Running says a turn is in progress.
	Running bool
	// HasOlder says pages of older entries exist above this window.
	HasOlder bool
	// Title and Cwd are the conversation's own, when it has them.
	Title, Cwd string
	// Session is the Daemon's session, while the Daemon path is in use; nil on the Kernel path, where nothing
	// here has a counterpart yet.
	Session *session.Session
	// State is the Kernel state, when the Kernel path is in use: the view, the run, the usage.
	State *State
	// queued is the Kernel path's queue, as the shelf draws it.
	queued []session.QueuedMessage
}

// LLMRetry is the model request the Daemon is retrying, nil when it is not. The Kernel reports a retry inside
// pi.live's generation, which the activity row reads from State once the Daemon path is gone.
func (s *Snapshot) LLMRetry() *session.LLMRetry {
	if s.Session == nil {
		return nil
	}
	return s.Session.LLMRetry()
}

// Settings are the conversation's: the model, the thinking level, the tags. The Daemon's session answers them
// today; on the Kernel path they come from the view's pi.agent once the settings move.
func (s *Snapshot) Settings() session.Settings {
	if s.Session == nil {
		return session.Settings{}
	}
	return s.Session.Settings()
}

// Stats are the conversation's measured speeds. Only the Daemon has them; the Kernel measures usage instead, which
// the composer reads from State.
func (s *Snapshot) Stats() session.Stats {
	if s.Session == nil {
		return session.Stats{}
	}
	return s.Session.Stats()
}

// QueuedMessages are the messages waiting for the turn in progress: the Daemon's session's, or on the Kernel path
// what Stop kept and the view's pi.inbox.
func (s *Snapshot) QueuedMessages() []session.QueuedMessage {
	if s.Session == nil {
		return s.queued
	}
	return s.Session.QueuedMessages()
}

// Usage is the conversation's last measured usage, for the meter under the composer. Only the Daemon records it per
// call; on the Kernel path the meter reads State.UsageContext instead.
func (s *Snapshot) Usage() session.Usage {
	if s.Session == nil {
		return session.Usage{}
	}
	return s.Session.Usage()
}

// ClearQueuedMessage removes a queued message the Daemon accepted a decision about.
func (s *Snapshot) ClearQueuedMessage(id string) {
	if s.Session != nil {
		s.Session.ClearQueuedMessage(id)
	}
}

// AvailableModels are the models the Daemon offers for this conversation. The Kernel path reads the picker's list
// from harness.info instead.
func (s *Snapshot) AvailableModels() []protocol.ModelMetadata {
	if s.Session == nil {
		return nil
	}
	return s.Session.AvailableModels()
}

// Todos are the conversation's todo list. Only the Daemon has them: the Kernel has no todo turn.
func (s *Snapshot) Todos() []session.TodoItem {
	if s.Session == nil {
		return nil
	}
	return s.Session.Todos()
}

// WorkingState is the Daemon's working-state string, which its own views read. The Kernel path answers idle or
// streaming from the run, since it has no finer state to tell.
func (s *Snapshot) WorkingState() protocol.DroidWorkingState {
	if s.Session != nil {
		return s.Session.WorkingState()
	}
	if s.Running {
		return protocol.DroidWorkingStateStreamingAssistantMessage
	}
	return protocol.DroidWorkingStateIdle
}

// kernelSnapshot is the snapshot of a conversation Kernel is driving: its rows are the view's, and a run is in
// progress exactly while the state names one.
func kernelSnapshot(state *State) *Snapshot {
	return &Snapshot{
		Loaded:   state.Opened(),
		LoadErr:  state.LoadErr(),
		Running:  state.Busy(),
		HasOlder: state.HasOlder(),
		State:    state,
	}
}

// daemonSnapshot is the snapshot of a conversation the Daemon is driving: the rows come from its session, and
// everything else is the session's own. It is nil while the Store has no session, as Frame's answer was.
func daemonSnapshot(sess *session.Session) *Snapshot {
	if sess == nil {
		return nil
	}
	return &Snapshot{
		Loaded:   sess.LoadState() == session.Loaded,
		Running:  sess.WorkingState() != protocol.DroidWorkingStateIdle,
		HasOlder: sess.HasOlderMessages(),
		Title:    sess.Title(),
		Cwd:      sess.Cwd(),
		Session:  sess,
	}
}

// kernelID is the conversation's Kernel id when the Kernel path is in use, and whether it is.
func (v *View) kernelID() (kernel.ID, bool) {
	if v.env.Kernel == nil || v.env.Sessions == nil {
		return 0, false
	}
	return v.convID()
}

// kernelState is the conversation's Kernel state, nil when the Kernel path is not in use or nothing is open.
func (v *View) kernelState() *State {
	id, ok := v.kernelID()
	if !ok {
		return nil
	}
	return v.env.Sessions.State(id)
}
