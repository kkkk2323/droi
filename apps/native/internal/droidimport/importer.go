package droidimport

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/kernel"
)

// Kernel is the calls an import makes.
type Kernel interface {
	Import(ctx context.Context, p kernel.ImportParams) (kernel.ImportResult, error)
	UpdateIndex(ctx context.Context, p kernel.IndexUpdateParams) error
	Configure(ctx context.Context, p kernel.ConfigureParams) (kernel.ConfigureResult, error)
}

// maxBatchBytes keeps one call's frame well under the Kernel's message limit (Bun's 16 MB): a session's pictures
// can make a thousand entries far larger.
const maxBatchBytes = 4 << 20

// Write imports conv as one conversation, perEntries entries a call at most (harness.info's
// limits.maxImportEntriesPerCall), and answers it. agent, when set, is what the conversation runs with from its
// next turn.
//
// Every call's key is made of the session's id, the transcript's hash and the batch's place, so a retry after a
// failure part way, or a second import of the same file, writes nothing twice: the Kernel answers what the first
// call wrote. The conversation reads as seen up to its last message once every batch is in.
func Write(ctx context.Context, k Kernel, conv Conversation, perEntries int, agent *kernel.AgentChange) (kernel.ID, error) {
	if perEntries <= 0 {
		perEntries = 1000
	}
	var id kernel.ID
	for n, entries := range batches(conv.Entries, perEntries, maxBatchBytes) {
		p := kernel.ImportParams{ID: id, Key: fmt.Sprintf("droid-import/v1/%s/%s/%d", conv.ID, conv.Hash[:16], n), Entries: entries}
		if n == 0 {
			p.Title, p.Cwd, p.Kind = conv.Title, conv.Cwd, "chat"
			if conv.CreatedAt > 0 {
				created := conv.CreatedAt
				p.CreatedAt = &created
			}
			if conv.Archived {
				archived := true
				p.Archived = &archived
			}
		}
		res, err := k.Import(ctx, p)
		if err != nil {
			return id, fmt.Errorf("batch %d: %w", n+1, err)
		}
		id = res.ConversationID
	}
	// The agent is set apart from the import: it follows Droi's settings, which may change between a failed
	// import and its retry, and the import's keys must stand for the same entries both times.
	if agent != nil {
		if _, err := k.Configure(ctx, kernel.ConfigureParams{ID: id, Change: agent}); err != nil {
			return id, err
		}
	}
	// A batch after the first moves the last activity on but not the seen mark: without this, an imported
	// history would come in unread.
	if conv.LastAt > 0 {
		if err := k.UpdateIndex(ctx, kernel.IndexUpdateParams{ID: id, ViewedAt: kernel.ViewedAtTime(conv.LastAt)}); err != nil {
			return id, err
		}
	}
	return id, nil
}

// batches splits entries into calls of at most perEntries entries and about maxBytes of JSON; an entry larger
// than maxBytes goes alone. Always one batch at least: the first creates the conversation.
func batches(entries []kernel.ImportEntry, perEntries, maxBytes int) [][]kernel.ImportEntry {
	out := [][]kernel.ImportEntry{{}}
	size := 0
	for _, e := range entries {
		n := len(e.Model) + len(e.Data) + len(e.Head) + 64
		last := out[len(out)-1]
		if len(last) > 0 && (len(last) >= perEntries || size+n > maxBytes) {
			out = append(out, nil)
			size = 0
		}
		out[len(out)-1] = append(out[len(out)-1], e)
		size += n
	}
	return out
}

// Import converts src, writes it with the agent agentFor picks for it (nil for none), and keeps the record in
// store.
func Import(ctx context.Context, k Kernel, store *Store, src Source, perEntries int, agentFor func(Conversation) *kernel.AgentChange) (kernel.ID, Conversation, error) {
	info, err := os.Stat(src.Path)
	if err != nil {
		return 0, Conversation{}, err
	}
	conv, err := Convert(src.Path)
	if err != nil {
		return 0, conv, err
	}
	if conv.ID == "" {
		conv.ID = src.ID
	}
	var agent *kernel.AgentChange
	if agentFor != nil {
		agent = agentFor(conv)
	}
	id, err := Write(ctx, k, conv, perEntries, agent)
	if err != nil {
		return id, conv, err
	}
	err = store.Put(src.ID, Record{Conversation: id, Hash: conv.Hash, Size: info.Size(), ModTime: info.ModTime().UnixMilli(), At: time.Now().UnixMilli()})
	return id, conv, err
}
