// Package session keeps the state of the Daemon's Sessions as one Client
// sees it: each Session's transcript, working state, settings, token usage,
// task list, queued and optimistic user messages, and how subagent Sessions
// hang under their parents. It is the Go port of the state layer of
// Factory's TypeScript SDK (MultiSessionStateManager, SessionStateManager,
// SessionStore); store.go lists what it leaves out.
//
// The package does no I/O. A controller owns the connection (droid.Client)
// and feeds the Store:
//
//	store := session.NewStore()
//	client.Subscribe(func(n droid.Notification) {
//		if n.Method != protocol.NotificationSessionNotification {
//			return
//		}
//		var p protocol.SessionNotificationParams
//		if json.Unmarshal(n.Params, &p) == nil {
//			store.HandleNotification(p, session.HandleOptions{})
//		}
//	})
//
//	tok := store.BeginLoad(id)               // marks it Loading
//	res, err := client.LoadSession(ctx, params)
//	if err != nil { store.MarkNotLoaded(id) } // or MarkNotFound on -32004
//	out := store.ApplyLoadResult(tok, res, limit)
//	// restore prompts, resubmit out.Resubmit, then:
//	store.ApplyLoadedWorkingState(tok, out.ReportedWorkingState)
//
// For a new Session call ApplyInitializeResult with the
// daemon.initialize_session result. Older pages from
// daemon.get_session_messages go to PrependOlderMessages, with
// Session.OlderMessagesCursor as the cursor.
//
// # Reading state
//
// Store.Session returns a handle whose getters (DisplayMessages,
// WorkingState, Settings, Todos, QueuedMessages, ...) copy what they
// return, so the result can be read without any lock. Subscribe delivers an
// Event per kind of change and Session after each mutation, outside the
// lock, on the goroutine that made the change (for notifications, the
// Client's read goroutine), so subscribers must not block. Events flagged
// Streaming come with every text delta; a UI throttles them.
//
// Content blocks stay protocol.ContentBlock. Blocks built from deltas carry
// an "isStreaming" field (IsStreaming reads it) and thinking blocks
// "startedAtMs"/"durationMs", as in the TypeScript SDK.
//
// # What HandleNotification covers
//
// Besides what the TypeScript SessionStateManager applies, HandleNotification
// applies the state-only effects the TypeScript controller has: token usage
// (session_token_usage_changed), the turn's completion reason and subagent
// summary (agent_turn_completed, error), the working directory, the
// inactive flag (session_inactivity, session_process_exited), removing the
// Session on session_closed, and registering a child on
// child_session_available. It confirms an OptimisticSubmit when a
// create_message echoes its key as requestId.
//
// The controller still owns: permission and AskUser prompts (and setting
// HandleOptions.PreserveWorkingStateOnInactive, or rewriting an idle
// working state to waiting, while one is pending); MarkActive after a load;
// hydrating a child Session with load_session after
// child_session_available; resending LoadOutcome.Resubmit; and, after
// add_user_message while the agent works, Session.QueueUserMessages with
// KindForPlacement (or SetWorkingState(streaming) when it was idle).
package session
