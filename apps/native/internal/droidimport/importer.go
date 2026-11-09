package droidimport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/l10n"
)

// Kernel is the calls an import makes.
type Kernel interface {
	Import(ctx context.Context, p kernel.ImportParams) (kernel.ImportResult, error)
	UpdateIndex(ctx context.Context, p kernel.IndexUpdateParams) error
	Configure(ctx context.Context, p kernel.ConfigureParams) (kernel.ConfigureResult, error)
	Entries(ctx context.Context, p kernel.EntriesParams) (kernel.EntriesResult, error)
}

// maxBatchBytes keeps one call's frame well under the Kernel's message limit (Bun's 16 MB): a session's pictures
// can make a thousand entries far larger.
const maxBatchBytes = 4 << 20

// Write imports conv as one new conversation, perEntries entries a call at most (harness.info's
// limits.maxImportEntriesPerCall), and answers it. agent, when set, is what the conversation runs with from its
// next turn.
//
// Every call's key is made of the session's id, the transcript's hash and the entries the call carries, so a retry
// after a failure part way, or a second import of the same file, writes nothing twice: the Kernel answers what the
// first call wrote. The keyed calls carry only what the transcript says; what may change between a failure and its
// retry (the note's language, the agent, droid's archive) is set after them.
func Write(ctx context.Context, k Kernel, conv Conversation, perEntries int, agent *kernel.AgentChange) (kernel.ID, error) {
	return write(ctx, k, conv, perEntries, agent, nil)
}

func write(ctx context.Context, k Kernel, conv Conversation, perEntries int, agent *kernel.AgentChange, created func(kernel.ID)) (kernel.ID, error) {
	id, err := writeFrom(ctx, k, conv, perEntries, 0, nil, created)
	if err != nil {
		return id, err
	}
	return id, finish(ctx, k, conv, id, agent, true)
}

// writeFrom writes the entries of conv that ids does not hold yet into conversation id, or into a new one when id
// is 0, and answers the conversation. ids are the Kernel's IDs of conv's first entries, which a compaction's head
// may name. created hears the conversation's id once the first call made it.
func writeFrom(ctx context.Context, k Kernel, conv Conversation, perEntries int, id kernel.ID, ids []kernel.ID, created func(kernel.ID)) (kernel.ID, error) {
	if perEntries <= 0 {
		perEntries = 1000
	}
	key := importKey(conv)
	off := len(ids)
	rest := conv.Entries[off:]
	if id != 0 && len(rest) == 0 {
		return id, nil
	}
	heads := map[int]int{}
	for at, head := range conv.Heads {
		if at >= off {
			heads[at-off] = head - off
		}
	}
	ids = slices.Clone(ids)
	for n, span := range batches(rest, heads, perEntries, maxBatchBytes) {
		from, to := off+span[0], off+span[1]
		// Never null: a session with nothing in it yet is still made, with an empty batch.
		entries := append([]kernel.ImportEntry{}, conv.Entries[from:to]...)
		for i := range entries {
			if head, ok := conv.Heads[from+i]; ok {
				entries[i].Head = json.RawMessage(ids[head].String())
			}
		}
		// A key names the entries a call carries, not its place in a run: a later build that splits the calls
		// otherwise asks for other entries under other keys, rather than for others under a key already used.
		p := kernel.ImportParams{ID: id, Key: fmt.Sprintf("%s/%d-%d", key, from, to), Entries: entries}
		if id == 0 {
			p.Title, p.Cwd, p.Kind = conv.Title, conv.Cwd, "chat"
			if conv.Scratch {
				p.Kind = "scratch"
			}
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
		if id == 0 && created != nil {
			created(res.ConversationID)
		}
		id = res.ConversationID
		ids = append(ids, res.EntryIDs...)
	}
	return id, nil
}

// finish sets what the keyed calls leave out: the note that says where the history came from and the agent, for
// a conversation the import made (first), and the seen mark and droid's archive.
func finish(ctx context.Context, k Kernel, conv Conversation, id kernel.ID, agent *kernel.AgentChange, first bool) error {
	if first {
		// The note's key names its language: a retry in another language adds its own line rather than failing.
		note := kernel.ImportParams{ID: id, Key: fmt.Sprintf("%s/note-%s", importKey(conv), l10n.Code()), Entries: []kernel.ImportEntry{conv.note()}}
		if _, err := k.Import(ctx, note); err != nil {
			return fmt.Errorf("the note: %w", err)
		}
		if agent != nil {
			if _, err := k.Configure(ctx, kernel.ConfigureParams{ID: id, Change: agent}); err != nil {
				return err
			}
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
			return err
		}
	}
	return nil
}

// importKey names one transcript of one session: the session's id and the file's hash.
func importKey(conv Conversation) string {
	return fmt.Sprintf("droid-import/v1/%s/%s", conv.ID, conv.Hash[:16])
}

// held is what conversation id holds of conv: the Kernel's IDs of conv's first entries, when the conversation
// holds those and nothing else. ok is false when it holds anything conv does not begin with — droid's session was
// rewritten, or the conversation went on in Droi — and then conv is no continuation of it.
func held(ctx context.Context, k Kernel, conv Conversation, id kernel.ID) (ids []kernel.ID, ok bool, err error) {
	var cursor json.RawMessage
	for {
		page, err := k.Entries(ctx, kernel.EntriesParams{ID: id, Query: ascending, Limit: entriesPage, Cursor: cursor})
		if err != nil {
			return nil, false, err
		}
		for _, e := range page.Items {
			// Droi's bookkeeping (the meta, the import memos, the note) is no part of droid's history.
			if strings.HasPrefix(e.Kind, "droi.") {
				continue
			}
			if len(ids) >= len(conv.Entries) || !sameEntry(e, conv.Entries[len(ids)]) {
				return nil, false, nil
			}
			ids = append(ids, e.ID)
		}
		if len(page.Items) == 0 || len(page.Cursor) == 0 || string(page.Cursor) == "null" {
			return ids, true, nil
		}
		cursor = page.Cursor
	}
}

// entriesPage is how many entries one conversation.entries call asks for.
const entriesPage = 200

// ascending reads a conversation from its first entry: conversation.entries reads the newest first otherwise.
var ascending = json.RawMessage(`{"order":"ascending"}`)

// sameEntry says the Kernel's entry is the one the import sent: the same kind, and a message and data that hold
// everything sent. The Kernel fills in what a message left out (a usage of zeros), so what it holds may say more;
// a head is not compared, since the import sends it as an ID it learns while it writes.
func sameEntry(e kernel.Entry, sent kernel.ImportEntry) bool {
	return e.Kind == sent.Kind && holdsJSON(e.Model, sent.Model) && holdsJSON(e.Data, sent.Data)
}

// holdsJSON says stored holds everything sent holds: the same values, with more object fields allowed.
func holdsJSON(stored, sent json.RawMessage) bool {
	empty := func(r json.RawMessage) bool {
		s := bytes.TrimSpace(r)
		return len(s) == 0 || string(s) == "null"
	}
	if empty(sent) {
		return true
	}
	if empty(stored) {
		return false
	}
	var x, y any
	if json.Unmarshal(stored, &x) != nil || json.Unmarshal(sent, &y) != nil {
		return false
	}
	return holds(x, y)
}

func holds(stored, sent any) bool {
	switch s := sent.(type) {
	case map[string]any:
		o, ok := stored.(map[string]any)
		if !ok {
			return false
		}
		for k, v := range s {
			if w, ok := o[k]; !ok || !holds(w, v) {
				return false
			}
		}
		return true
	case []any:
		o, ok := stored.([]any)
		if !ok || len(o) != len(s) {
			return false
		}
		for i := range s {
			if !holds(o[i], s[i]) {
				return false
			}
		}
		return true
	}
	return reflect.DeepEqual(stored, sent)
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
//
// A session imported before goes on in the conversation the record names, when that conversation holds the start
// of the transcript and nothing else: droid went on with the session since, or the import before stopped part
// way. Otherwise — droid's file was rewritten, or the conversation went on in Droi — it comes over as a new one.
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
	done := Record{Hash: conv.Hash, Size: info.Size(), ModTime: info.ModTime().UnixMilli()}
	if rec, ok := store.Get(src.ID); ok && rec.Conversation != 0 {
		ids, follows, err := held(ctx, k, conv, rec.Conversation)
		if err != nil {
			return rec.Conversation, conv, err
		}
		if follows {
			id, err := writeFrom(ctx, k, conv, perEntries, rec.Conversation, ids, nil)
			if err == nil {
				err = finish(ctx, k, conv, id, agent, rec.Partial)
			}
			if err != nil {
				return id, conv, err
			}
			done.Conversation, done.At = id, time.Now().UnixMilli()
			return id, conv, store.Put(src.ID, done)
		}
	}
	// The record names the conversation as soon as the first call made it, so an import that stops part way is
	// gone on with next time, by this build or a later one that splits the calls otherwise.
	id, err := write(ctx, k, conv, perEntries, agent, func(id kernel.ID) {
		_ = store.Put(src.ID, Record{Conversation: id, Partial: true, At: time.Now().UnixMilli()})
	})
	if err != nil {
		return id, conv, err
	}
	done.Conversation, done.At = id, time.Now().UnixMilli()
	return id, conv, store.Put(src.ID, done)
}
