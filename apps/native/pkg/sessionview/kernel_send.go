package sessionview

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/kkkk2323/droid-sdk-go/protocol"
	"github.com/kkkk2323/droid-sdk-go/session"

	"github.com/kkkk2323/droi/apps/native/pkg/attachments"
	"github.com/kkkk2323/droi/apps/native/pkg/kernel"
	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
)

// The composer's half of the Kernel path: a message becomes conversation.submit, Stop becomes conversation.abort,
// and the queue the shelf draws is pi.inbox. The shelf keeps drawing the SDK's QueuedMessage, so a message queued on
// Kernel and one queued on the Daemon look and behave the same there.

// kernelQueuePrefix marks a queued message's RequestID as a Kernel submission: "kernel:<submissionId>".
const kernelQueuePrefix = "kernel:"

// submitInput is conversation.submit's input: the text alone as a string, or content blocks when there are
// pictures, as the contract spells them.
func submitInput(text string, images []attachments.Image) any {
	if len(images) == 0 {
		return text
	}
	blocks := make([]map[string]any, 0, len(images)+1)
	if text != "" {
		blocks = append(blocks, map[string]any{"type": "text", "text": text})
	}
	for _, img := range images {
		blocks = append(blocks, map[string]any{"type": "image", "data": img.Data, "mimeType": img.MediaType})
	}
	return blocks
}

// whenBusyOf is what a placement asks of a run in progress. ⌘↩ hands the message to the running turn (end of
// turn: steer); Enter queues it for after the run (end of loop: follow-up); an idle send names none, and Kernel's
// default, a follow-up, keeps one that races a run starting.
func whenBusyOf(placement protocol.QueuePlacement) string {
	switch placement {
	case protocol.QueuePlacementEndOfTurn:
		return kernel.WhenBusySteer
	case protocol.QueuePlacementEndOfLoop:
		return kernel.WhenBusyFollowUp
	}
	return ""
}

// submit sends a message to the conversation Kernel drives. Each send has its own request id, so a retry of the
// same call is answered as the first one rather than queued twice. The submission is recorded once Kernel answers,
// so a Stop before the view that queues it arrives still finds it.
func (v *View) submit(id kernel.ID, text string, images []attachments.Image, placement protocol.QueuePlacement) {
	p := kernel.SubmitParams{ID: id, Input: submitInput(text, images), WhenBusy: whenBusyOf(placement), RequestID: uuid.NewString()}
	go func() {
		res, err := v.env.Kernel.Submit(v.env.Ctx, p)
		if err != nil {
			v.Fail(err)
			return
		}
		content, _ := json.Marshal(p.Input)
		mode := p.WhenBusy
		if mode == "" {
			mode = kernel.WhenBusyFollowUp
		}
		v.env.Sessions.submitted(id, InboxItem{ID: int64(res.SubmissionID), Mode: mode, Content: content})
	}()
}

// interruptKernel stops the run. Kernel withdraws the queued inputs with it, which the Daemon's path kept on this
// computer as paused messages; so do these: what the user queued is not thrown away by Stop. Only the inputs Kernel
// reports withdrawn are kept — one that entered the transcript before the abort landed went out, and editing it
// again would send it twice. One whose fate cannot be read is kept too, and the error said: the words are only here.
func (v *View) interruptKernel(id kernel.ID) {
	store := v.env.Sessions
	queued := store.queuedInputs(id)
	go func() {
		if err := v.env.Kernel.Abort(v.env.Ctx, kernel.AbortParams{ID: id}); err != nil {
			v.Fail(err)
			return
		}
		var kept []session.QueuedMessage
		var decided []int64
		var unread error
		for _, item := range queued {
			rec, err := v.env.Kernel.SubmissionGet(v.env.Ctx, kernel.AbortSubmissionParams{ID: id, SubmissionID: kernel.ID(item.ID)})
			if err == nil {
				decided = append(decided, item.ID)
				if rec.Status != "unanswered" || rec.Entry != nil {
					continue
				}
			} else if unread == nil {
				unread = err
			}
			if q, ok := queuedOf(item); ok {
				q.Kind = session.KindLocalPausedAfterEsc
				q.RequestID = pausedID(item.ID)
				kept = append(kept, q)
			}
		}
		store.settled(id, decided)
		store.pause(id, kept)
		if unread != nil {
			v.Fail(fmt.Errorf("%s: %w", l10n.L("Stop could not tell whether a queued message was sent; it is kept above the composer"), unread))
		}
		v.redraw()
	}()
}

// kernelQueued is the shelf's queue for a conversation Kernel drives: what Stop kept, then what pi.inbox holds, in
// the order it will be placed.
func (v *View) kernelQueued(state *State) []session.QueuedMessage {
	out := v.env.Sessions.Paused(state.ID())
	for _, item := range inputsOf(state.InboxItems()) {
		if q, ok := queuedOf(item); ok {
			out = append(out, q)
		}
	}
	return out
}

// inputsOf are the inbox's inputs: a write a client asked to append is not a message the user queued.
func inputsOf(items []InboxItem) []InboxItem {
	var out []InboxItem
	for _, item := range items {
		if item.Mode == kernel.WhenBusySteer || item.Mode == kernel.WhenBusyFollowUp {
			out = append(out, item)
		}
	}
	return out
}

// queuedOf is an inbox input as the shelf draws a queued message: its text and pictures as the Daemon's content
// blocks, and the kind that says when it goes out — a steer joins the running turn ("Next"), a follow-up waits for
// the run to end.
func queuedOf(item InboxItem) (session.QueuedMessage, bool) {
	q := session.QueuedMessage{RequestID: kernelQueuePrefix + strconv.FormatInt(item.ID, 10), Kind: session.KindDaemonQueuedEndOfLoop}
	if item.Mode == kernel.WhenBusySteer {
		q.Kind = session.KindDaemonQueuedDiscardable
	}
	var text string
	if json.Unmarshal(item.Content, &text) == nil {
		if b, err := protocol.NewContentBlock(map[string]any{"type": "text", "text": text}); err == nil {
			q.Content = append(q.Content, b)
		}
		return q, len(q.Content) > 0
	}
	var blocks []struct {
		Type     string `json:"type"`
		Text     string `json:"text"`
		Data     string `json:"data"`
		MimeType string `json:"mimeType"`
	}
	if json.Unmarshal(item.Content, &blocks) != nil {
		return q, false
	}
	for _, blk := range blocks {
		var raw map[string]any
		switch blk.Type {
		case "text":
			raw = map[string]any{"type": "text", "text": blk.Text}
		case "image":
			raw = map[string]any{"type": "image", "source": protocol.Base64ImageSource{
				Type: "base64", Data: blk.Data, MediaType: protocol.Base64ImageSourceMediaType(blk.MimeType)}}
		default:
			continue
		}
		if b, err := protocol.NewContentBlock(raw); err == nil {
			q.Content = append(q.Content, b)
		}
	}
	return q, len(q.Content) > 0
}

// Withdraw takes a queued message out of the queue: one kept on this computer is dropped here; one Kernel or the
// Daemon holds is withdrawn there first, and an error says it could not be — it may already have gone out, and
// the composer must not get it back to send twice. It makes a round trip, so call it off the UI thread.
func (v *View) Withdraw(q session.QueuedMessage) error {
	if id, ok := v.kernelID(); ok {
		return v.withdrawKernel(id, q)
	}
	ctl := v.env.Controller
	if ctl == nil {
		return errors.New("not connected")
	}
	clear := func() {
		if s := ctl.Store().Session(v.id); s != nil {
			s.ClearQueuedMessage(q.RequestID)
		}
	}
	if !q.Kind.DaemonBacked() {
		clear()
		return nil
	}
	cl, err := ctl.Client()
	if err != nil {
		return err
	}
	raw, _ := json.Marshal(map[string]any{"sessionId": v.id, "requestId": q.RequestID, "action": "delete"})
	if _, err := cl.ResolveQueuedUserMessage(v.env.Ctx, raw); err != nil {
		return err
	}
	// The Daemon sends no notification for a delete; the Client drops it.
	clear()
	return nil
}

func (v *View) withdrawKernel(id kernel.ID, q session.QueuedMessage) error {
	if q.Kind == session.KindLocalPausedAfterEsc {
		v.env.Sessions.unpause(id, q.RequestID)
		v.redraw()
		return nil
	}
	n, err := strconv.ParseInt(strings.TrimPrefix(q.RequestID, kernelQueuePrefix), 10, 64)
	if !strings.HasPrefix(q.RequestID, kernelQueuePrefix) || err != nil {
		return fmt.Errorf("%q is not a queued Kernel submission", q.RequestID)
	}
	_, err = v.env.Kernel.AbortSubmission(v.env.Ctx, kernel.AbortSubmissionParams{ID: id, SubmissionID: kernel.ID(n)})
	return err
}
