package droidimport

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"time"

	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/l10n"
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
// call wrote. The keyed calls carry only what the transcript says; what may change between a failure and its retry
// (the note's language, the agent, droid's archive) is set after them.
func Write(ctx context.Context, k Kernel, conv Conversation, perEntries int, agent *kernel.AgentChange) (kernel.ID, error) {
	if perEntries <= 0 {
		perEntries = 1000
	}
	key := fmt.Sprintf("droid-import/v1/%s/%s", conv.ID, conv.Hash[:16])
	var id kernel.ID
	ids := make([]kernel.ID, 0, len(conv.Entries))
	for n, span := range batches(conv.Entries, conv.Heads, perEntries, maxBatchBytes) {
		entries := slices.Clone(conv.Entries[span[0]:span[1]])
		for i := range entries {
			if head, ok := conv.Heads[span[0]+i]; ok {
				entries[i].Head = json.RawMessage(ids[head].String())
			}
		}
		p := kernel.ImportParams{ID: id, Key: fmt.Sprintf("%s/%d", key, n), Entries: entries}
		if n == 0 {
			p.Title, p.Cwd, p.Kind = conv.Title, conv.Cwd, "chat"
			if conv.CreatedAt > 0 {
				created := conv.CreatedAt
				p.CreatedAt = &created
			}
		}
		res, err := k.Import(ctx, p)
		if err != nil {
			return id, fmt.Errorf("batch %d: %w", n+1, err)
		}
		if len(res.EntryIDs) != len(entries) {
			return id, fmt.Errorf("batch %d: %d entries written of %d", n+1, len(res.EntryIDs), len(entries))
		}
		id = res.ConversationID
		ids = append(ids, res.EntryIDs...)
	}
	// The note's key names its language: a retry in another language adds its own line rather than failing.
	note := kernel.ImportParams{ID: id, Key: fmt.Sprintf("%s/note-%s", key, l10n.Code()), Entries: []kernel.ImportEntry{conv.note()}}
	if _, err := k.Import(ctx, note); err != nil {
		return id, fmt.Errorf("the note: %w", err)
	}
	if agent != nil {
		if _, err := k.Configure(ctx, kernel.ConfigureParams{ID: id, Change: agent}); err != nil {
			return id, err
		}
	}
	// A batch after the first moves the last activity on but not the seen mark: without this, an imported
	// history would come in unread.
	update := kernel.IndexUpdateParams{ID: id}
	if conv.LastAt > 0 {
		update.ViewedAt = kernel.ViewedAtTime(conv.LastAt)
	}
	if conv.Archived {
		archived := true
		update.Archived = &archived
	}
	if update.ViewedAt != nil || update.Archived != nil {
		if err := k.UpdateIndex(ctx, update); err != nil {
			return id, err
		}
	}
	return id, nil
}

// batches splits entries into calls of at most perEntries entries and about maxBytes of JSON, as [from, to)
// spans; an entry larger than maxBytes goes alone. A compaction whose head is in the call it would join starts the
// next one instead: the Kernel takes a head only among entries it already holds. Always one span at least: the
// first creates the conversation.
func batches(entries []kernel.ImportEntry, heads map[int]int, perEntries, maxBytes int) [][2]int {
	out := [][2]int{{0, 0}}
	size := 0
	for i, e := range entries {
		n := len(e.Model) + len(e.Data) + len(e.Head) + 64
		last := &out[len(out)-1]
		head, isHead := heads[i]
		if last[1] > last[0] && (last[1]-last[0] >= perEntries || size+n > maxBytes || (isHead && head >= last[0])) {
			out = append(out, [2]int{i, i})
			last = &out[len(out)-1]
			size = 0
		}
		last[1] = i + 1
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
