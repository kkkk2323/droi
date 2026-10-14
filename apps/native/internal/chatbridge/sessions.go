package chatbridge

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/kkkk2323/droi/packages/droid-sdk-go/controller"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/internal/host"
	"github.com/kkkk2323/droi/apps/native/internal/sessions"
)

const listPageSize = 100

func directThread(user string) *thread {
	return &thread{key: threadKindDirect + ":" + user, kind: threadKindDirect, userID: user}
}

// threadFromTags is the Chat Thread a Session's droi.chat tag names, nil
// for a Session that is not a Chat Session.
func threadFromTags(tags []protocol.SessionTag) *thread {
	for _, t := range tags {
		if t.Name != sessions.ChatTag {
			continue
		}
		switch t.Metadata["kind"] {
		case threadKindDirect:
			if user := t.Metadata["userId"]; user != "" {
				return directThread(user)
			}
		}
	}
	return nil
}

// sync rebuilds the Chat Thread map from the Daemon's Sessions, once per
// connection to the Daemon: their droi.chat tags are the only record of
// which Session belongs to which Chat Thread. A Chat Thread's newest Session
// that is not archived is its Chat Session; a Chat Thread whose Sessions
// are all archived starts a new one, told so.
func (b *Bridge) sync(ctx context.Context) error {
	b.syncMu.Lock()
	defer b.syncMu.Unlock()
	b.mu.Lock()
	done := b.synced
	b.mu.Unlock()
	if done {
		return nil
	}
	cl, err := b.ctl.Client()
	if err != nil {
		return err
	}
	type found struct {
		th       *thread
		live     *protocol.DaemonAvailableSessionInfo
		archived bool
	}
	byKey := map[string]*found{}
	// listed tells, for each Chat Session listed, whether it is archived.
	listed := map[string]bool{}
	limit, yes := float64(listPageSize), true
	p := protocol.ListAvailableSessionsParams{Limit: &limit, IncludeArchived: &yes}
	for {
		res, err := cl.ListAvailableSessions(ctx, p)
		if err != nil {
			return err
		}
		for i := range res.Sessions {
			s := &res.Sessions[i]
			th := threadFromTags(s.Tags)
			if th == nil {
				continue
			}
			listed[s.SessionID] = s.ArchivedAt != ""
			f := byKey[th.key]
			if f == nil {
				f = &found{th: th}
				byKey[th.key] = f
			}
			if s.ArchivedAt != "" {
				f.archived = true
			} else if f.live == nil || s.UpdatedAt > f.live.UpdatedAt {
				f.live = s
			}
		}
		if !res.HasMore || res.NextCursor == nil {
			break
		}
		p.EndBefore = res.NextCursor
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for key, f := range byKey {
		th := b.threads[key]
		if th == nil {
			th = f.th
			b.threads[key] = th
		}
		if th.sessionID != "" {
			// A Chat Session not listed may be one just started, which the
			// Daemon lists once it has a message.
			if archived, ok := listed[th.sessionID]; !ok || !archived {
				continue
			}
			b.unlink(th)
		}
		if f.live != nil {
			th.sessionID, th.archived = f.live.SessionID, false
		} else {
			th.archived = f.archived
		}
	}
	b.synced = true
	return nil
}

// unlink ends a Chat Thread's link to its Chat Session; b.mu is held.
func (b *Bridge) unlink(th *thread) {
	if th.sessionID != "" {
		b.ctl.Forget(th.sessionID)
	}
	b.useToken(th, "")
	th.sessionID, th.loaded = "", false
}

// issueToken makes a droi-chat token for a Chat Session of th, and the
// server entry that carries it. The token works at once: the Daemon
// connects to droi-chat while it starts or loads the Session.
func (b *Bridge) issueToken(th *thread) (string, json.RawMessage, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.tools == nil {
		return "", nil, errors.New("the droi-chat tools are not listening")
	}
	token := newToken()
	server, err := json.Marshal(map[string]any{
		"type":    "http",
		"name":    host.ChatServerName,
		"url":     b.tools.url,
		"oauth":   false,
		"headers": []map[string]string{{"name": "Authorization", "value": "Bearer " + token}},
	})
	if err != nil {
		return "", nil, err
	}
	b.tokens[token] = th
	return token, server, nil
}

// useToken makes token th's, revoking the one it had; b.mu is held.
func (b *Bridge) useToken(th *thread, token string) {
	if th.token != "" && th.token != token {
		delete(b.tokens, th.token)
	}
	th.token = token
}

// loadSession loads a Chat Session this Chat Bridge did not start, or
// closed, with a new droi-chat token: the Daemon does not keep a Session's
// MCP servers.
func (b *Bridge) loadSession(ctx context.Context, th *thread, id string) error {
	token, server, err := b.issueToken(th)
	if err != nil {
		return err
	}
	_, err = b.ctl.LoadSession(ctx, protocol.LoadSessionParams{SessionID: id, MCPServers: []json.RawMessage{server}})
	b.mu.Lock()
	defer b.mu.Unlock()
	if err != nil {
		delete(b.tokens, token)
		return err
	}
	b.useToken(th, token)
	th.loaded = true
	return nil
}

// run queues op on th's operations, which run one at a time on a goroutine
// of th's own.
func (b *Bridge) run(th *thread, op func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.stopped {
		return
	}
	th.ops = append(th.ops, op)
	if th.running {
		return
	}
	th.running = true
	b.threadOps.Add(1)
	go func() {
		defer b.threadOps.Done()
		for {
			b.mu.Lock()
			if len(th.ops) == 0 || b.stopped {
				th.ops, th.running = nil, false
				b.mu.Unlock()
				return
			}
			op := th.ops[0]
			th.ops = th.ops[1:]
			b.mu.Unlock()
			op()
		}
	}()
}

// sessionThread is the Chat Thread whose Chat Session id is; b.mu is held.
func (b *Bridge) sessionThread(id string) *thread {
	for _, th := range b.threads {
		if th.sessionID == id {
			return th
		}
	}
	return nil
}

// onEvent follows the Chat Sessions on the Daemon.
func (b *Bridge) onEvent(e controller.Event) {
	switch e := e.(type) {
	case controller.StatusChanged:
		if !e.Status.Connected {
			// The Daemon may come back with Sessions archived meanwhile.
			b.mu.Lock()
			b.synced = false
			b.mu.Unlock()
		}
	case controller.SessionNotification:
		b.mu.Lock()
		defer b.mu.Unlock()
		th := b.sessionThread(e.SessionID)
		if th == nil {
			return
		}
		switch v := e.Value.(type) {
		case *protocol.DroidWorkingStateChangedNotification:
			if th.idle != nil {
				th.idle.Stop()
			}
			if v.NewState == protocol.DroidWorkingStateIdle {
				epoch := th.epoch
				th.idle = time.AfterFunc(b.o.IdleTimeout, func() { b.run(th, func() { b.closeIfIdle(th, epoch) }) })
			}
		case *protocol.SessionClosedNotification:
			th.loaded = false
		}
	case controller.DaemonNotification:
		if e.Method != protocol.NotificationSessionArchiveStateChanged {
			return
		}
		var p protocol.SessionArchiveStateChangedParams
		if json.Unmarshal(e.Params, &p) != nil || p.ArchivedAt == "" {
			return
		}
		b.mu.Lock()
		th := b.sessionThread(p.SessionID)
		b.mu.Unlock()
		if th != nil {
			b.run(th, func() { b.archived(th, p.SessionID) })
		}
	}
}

// archive archives th's Chat Session id and ends its link.
func (b *Bridge) archive(ctx context.Context, th *thread, id string) {
	b.mu.Lock()
	loaded := th.loaded
	b.unlink(th)
	b.mu.Unlock()
	cl, err := b.ctl.Client()
	if err == nil {
		force := true
		_, err = cl.ArchiveSession(ctx, protocol.ArchiveSessionParams{SessionID: id, Force: &force})
	}
	if err != nil {
		b.o.Log("cannot archive the Chat Session: " + err.Error())
	}
	if loaded {
		if err := b.ctl.CloseSession(ctx, protocol.CloseSessionParams{SessionID: id}); err != nil {
			b.o.Log("cannot close an archived Chat Session: " + err.Error())
		}
	}
}

// archived ends the link of a Chat Session archived in a Client: the Chat
// Thread's next message starts a new one.
func (b *Bridge) archived(th *thread, id string) {
	b.mu.Lock()
	if th.sessionID != id {
		b.mu.Unlock()
		return
	}
	loaded := th.loaded
	if th.idle != nil {
		th.idle.Stop()
	}
	b.unlink(th)
	th.archived = true
	b.mu.Unlock()
	if !loaded {
		return
	}
	ctx, cancel := b.opContext()
	defer cancel()
	if err := b.ctl.CloseSession(ctx, protocol.CloseSessionParams{SessionID: id}); err != nil {
		b.o.Log("cannot close an archived Chat Session: " + err.Error())
	}
}

// closeIfIdle closes th's Chat Session when no message came since epoch,
// to free the Daemon's worker; a turn starting stops the timer that calls
// it.
func (b *Bridge) closeIfIdle(th *thread, epoch int) {
	b.mu.Lock()
	id := th.sessionID
	idle := th.epoch == epoch && id != "" && th.loaded
	b.mu.Unlock()
	if !idle {
		return
	}
	ctx, cancel := b.opContext()
	defer cancel()
	if err := b.ctl.CloseSession(ctx, protocol.CloseSessionParams{SessionID: id}); err != nil {
		b.o.Log("cannot close an idle Chat Session: " + err.Error())
		return
	}
	b.mu.Lock()
	if th.sessionID == id {
		th.loaded = false
	}
	b.mu.Unlock()
}

// opContext bounds an operation on the Daemon and ends it when the Chat
// Bridge stops.
func (b *Bridge) opContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(context.Background(), deliverTimeout)
	go func() {
		select {
		case <-b.stop:
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}
