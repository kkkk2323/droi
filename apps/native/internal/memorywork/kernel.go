package memorywork

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"time"

	"github.com/kkkk2323/droi/apps/native/v2/internal/memory"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/l10n"
)

// Memory on the Kernel (ADR 0042): the conversations to summarize come from
// conversation.list, a transcript from conversation.entries, and a summary from
// model.complete, which opens no conversation and writes no entry.

// Kernel is what Memory needs of the Kernel. *kernel.Client is one.
type Kernel interface {
	Harness(ctx context.Context) (kernel.HarnessInfo, error)
	List(ctx context.Context, p kernel.ListParams) (kernel.ListResult, error)
	Entries(ctx context.Context, p kernel.EntriesParams) (kernel.EntriesResult, error)
	ModelComplete(ctx context.Context, p kernel.ModelCompleteParams) (kernel.ModelCompleteResult, error)
}

// ErrKernelGone is a Memory update's failure when the connection to the Kernel
// ended during it, as when the Host starts the Kernel again: nothing was wrong
// with what it was given.
var ErrKernelGone error = kernelGone{}

type kernelGone struct{}

func (kernelGone) Error() string { return l10n.L("Memory update failed: the Kernel went away") }

// skippedKinds are the kinds of conversation no user worked in: an Automation
// run, a Workstream run and Memory's own. A subagent's or a Workstream agent's
// conversation has an owner, and the list leaves those out.
var skippedKinds = []string{"automation", "workstream", "memory"}

// pageSize is how many rows or entries one call asks for.
const pageSize = 200

// NewKernelRunner answers a Memory request with one model.complete call: the
// prompt as the system text, the material as the input, and the schema the
// answer must satisfy.
func NewKernelRunner(k Kernel) Run {
	return func(ctx context.Context, r Request) (Reply, error) {
		info, err := k.Harness(ctx)
		if err != nil {
			return Reply{}, kernelError(ctx, err)
		}
		ref, ok := modelRef(info, r.ModelID)
		if !ok {
			// Not this Session's fault: the model is not offered now, as before a key is added.
			return Reply{}, errors.New(l10n.L("No provider offers the model %s.", r.ModelID))
		}
		schema, err := json.Marshal(r.Schema)
		if err != nil {
			return Reply{}, err
		}
		effort := string(r.ReasoningEffort)
		if effort == "" {
			effort = "low"
		}
		res, err := k.ModelComplete(ctx, kernel.ModelCompleteParams{
			Model:         ref,
			System:        r.Prompt,
			Input:         r.Input,
			Schema:        schema,
			ThinkingLevel: effort,
			TimeoutMs:     int(SessionTimeout / time.Millisecond),
		})
		if err != nil {
			if err := kernelError(ctx, err); errors.Is(err, ErrKernelGone) {
				return Reply{}, err
			}
			return Reply{Model: ref.ModelID, Started: true}, err
		}
		return Reply{JSON: res.JSON, Model: res.Model.ModelID, Started: true}, nil
	}
}

// kernelError is err, or ErrKernelGone when the call failed because the
// connection did, not because the Kernel answered with an error or the
// model ran out of time.
func kernelError(ctx context.Context, err error) error {
	var rpc *kernel.RPCError
	if ctx.Err() == nil && !errors.As(err, &rpc) && !errors.Is(err, context.DeadlineExceeded) {
		return ErrKernelGone
	}
	return err
}

// modelRef is the provider and id of a model named by id alone: the first
// provider that offers it.
func modelRef(info kernel.HarnessInfo, id string) (kernel.ModelRef, bool) {
	for _, m := range info.Models {
		if m.ID == id {
			return kernel.ModelRef{Provider: m.Provider, ModelID: m.ID}, true
		}
	}
	return kernel.ModelRef{}, false
}

// gatherKernel brings the Kernel conversations of the last MaxSessionAge into
// the state, with their titles and last activity, and forgets the others. A
// list that fails leaves the state as it was.
func (p *pass) gatherKernel(ctx context.Context, now time.Time) {
	k := p.w.Kernel
	if k == nil {
		return
	}
	seen := map[string]*memory.SessionRecord{}
	for _, archived := range []bool{false, true} {
		var cursor json.RawMessage
		for {
			page, err := k.List(ctx, kernel.ListParams{Archived: &archived, Cursor: cursor, Limit: pageSize})
			if err != nil {
				p.w.log("listing the conversations failed: " + err.Error())
				return
			}
			// The list is newest first, so the first one too old ends it.
			old := false
			for _, s := range page.Items {
				if now.Sub(time.UnixMilli(s.LastActivityAt)) > MaxSessionAge {
					old = true
					break
				}
				if slices.Contains(skippedKinds, s.Kind) || s.OwnerID != nil {
					continue
				}
				seen[strconv.FormatInt(int64(s.ID), 10)] = &memory.SessionRecord{
					Conversation: int64(s.ID),
					Cwd:          s.Workspace,
					StartedAt:    iso(time.UnixMilli(s.CreatedAt)),
					Title:        s.Title,
					ActivityAt:   s.LastActivityAt,
				}
			}
			if old || len(page.Cursor) == 0 || string(page.Cursor) == "null" {
				break
			}
			cursor = page.Cursor
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for key, rec := range p.st.Sessions {
		if rec.Conversation != 0 && seen[key] == nil {
			delete(p.st.Sessions, key)
		}
	}
	for key, fresh := range seen {
		rec := p.st.Sessions[key]
		if rec == nil {
			p.st.Sessions[key] = fresh
			continue
		}
		rec.Title, rec.ActivityAt = fresh.Title, fresh.ActivityAt
	}
	p.save()
}

// readKernel reads a conversation's transcript within budget characters.
func readKernel(ctx context.Context, k Kernel, id kernel.ID, budget int) (memory.Transcript, error) {
	var entries []kernel.Entry
	var cursor json.RawMessage
	for {
		page, err := k.Entries(ctx, kernel.EntriesParams{ID: id, Query: json.RawMessage(`{"order":"ascending"}`), Limit: pageSize, Cursor: cursor})
		if err != nil {
			return memory.Transcript{}, err
		}
		entries = append(entries, page.Items...)
		if len(page.Cursor) == 0 || string(page.Cursor) == "null" || len(page.Items) == 0 {
			break
		}
		cursor = page.Cursor
	}
	return memory.KernelTranscript(id, entries, budget), nil
}
