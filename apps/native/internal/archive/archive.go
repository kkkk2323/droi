// Package archive is archiving from the session list. A Session in a
// Workspace is archived on its own. A Scratch Session is a conversation of its
// own (ADR 0008): its whole compaction chain goes, and its folder with it to
// the Trash; unarchiving brings the folder back first, so the Sessions open
// again (a port of archive.ts).
package archive

import (
	"context"

	"github.com/kkkk2323/droi/apps/native/internal/sessions"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/protocol"
)

// Controller is the part of the Daemon client archiving uses.
type Controller interface {
	ArchiveSession(ctx context.Context, params protocol.ArchiveSessionParams) (*protocol.ArchiveSessionResult, error)
	UnarchiveSession(ctx context.Context, params protocol.UnarchiveSessionParams) (*protocol.UnarchiveSessionResult, error)
}

// Scratch is the Desktop Shell's side of Scratch Workspace folders.
type Scratch interface {
	// Trash moves the folder to the Trash; an empty one is simply removed.
	Trash(ctx context.Context, path string) error
	// Restore recreates the folder if it is gone.
	Restore(ctx context.Context, path string) error
}

func Conversation(ctx context.Context, c Controller, scratch Scratch, listed []sessions.Summary, session sessions.Summary) error {
	if !sessions.IsScratchSession(session) {
		_, err := c.ArchiveSession(ctx, protocol.ArchiveSessionParams{SessionID: session.SessionID})
		return err
	}
	for _, each := range append([]sessions.Summary{session}, sessions.ContinuationChain(listed, session)...) {
		if each.ArchivedAt != "" {
			continue
		}
		if _, err := c.ArchiveSession(ctx, protocol.ArchiveSessionParams{SessionID: each.SessionID}); err != nil {
			return err
		}
	}
	if session.Cwd != "" {
		return scratch.Trash(ctx, session.Cwd)
	}
	return nil
}

func Unarchive(ctx context.Context, c Controller, scratch Scratch, listed []sessions.Summary, session sessions.Summary) error {
	if !sessions.IsScratchSession(session) {
		_, err := c.UnarchiveSession(ctx, protocol.UnarchiveSessionParams{SessionID: session.SessionID})
		return err
	}
	if session.Cwd != "" {
		if err := scratch.Restore(ctx, session.Cwd); err != nil {
			return err
		}
	}
	for _, each := range append([]sessions.Summary{session}, sessions.ContinuationChain(listed, session)...) {
		if _, err := c.UnarchiveSession(ctx, protocol.UnarchiveSessionParams{SessionID: each.SessionID}); err != nil {
			return err
		}
	}
	return nil
}
