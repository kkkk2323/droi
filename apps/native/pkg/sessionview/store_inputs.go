package sessionview

import (
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/kkkk2323/droid-sdk-go/session"

	"github.com/kkkk2323/droi/apps/native/pkg/kernel"
)

// The inputs the window holds on Kernel's behalf, per conversation: what Stop took back from Kernel and kept here as
// paused messages, and what this window submitted that Kernel may still hold queued. They live in the Store rather
// than in a View or a State because both of those are dropped when the user moves on to other Sessions, and these
// are the user's words, which nothing else keeps.
//
// The ledger of what this window submitted has three parts. A send is in flight from before conversation.submit
// goes out until it answers, so a Stop in between can wait for its id. Once answered it is sent, until the
// conversation shows it placed or goes idle. And a submission the user disposed of — withdrew from the queue, or
// removed or edited once Stop had kept it — is disposed, so a Stop that answers late cannot bring it back.
//
// "Idle" needs care, because a pushed view can be older than a submit's answer (pushes are coalesced): an idle view
// says nothing of a submission answered after it was read. So only three things settle a sent input: a run that
// lists it as placed, which is final whenever it is read; conversation.busy going idle, which arrives in commit
// order; and an idle answer to a conversation.open asked after the submission was recorded. A placement seen while
// a send is still in flight is kept until it answers, since the run that placed it may be over by then.

// pausedPrefix marks a paused message's RequestID: "paused:<submissionId>", the submission Stop took it back from.
const pausedPrefix = "paused:"

func pausedID(sub int64) string { return pausedPrefix + strconv.FormatInt(sub, 10) }

// submissionOfPaused is the submission a paused message's RequestID names.
func submissionOfPaused(requestID string) (int64, bool) {
	n, err := strconv.ParseInt(strings.TrimPrefix(requestID, pausedPrefix), 10, 64)
	return n, err == nil && strings.HasPrefix(requestID, pausedPrefix)
}

// sendPrefix marks a paused message Stop kept from a send Kernel had not answered: "paused:send:<local id>". The
// local id is the send's own, so every Stop that gives up on it keeps one copy, and removing that copy disposes of
// the submission when its id comes.
const sendPrefix = pausedPrefix + "send:"

// inflight is one conversation.submit not answered yet. id and ok are set before done closes.
type inflight struct {
	item  InboxItem
	local string
	done  chan struct{}
	id    int64
	ok    bool
}

// pausedID is the RequestID of the copy a Stop keeps while the send is unanswered.
func (f *inflight) pausedID() string { return sendPrefix + f.local }

// Paused are the messages Stop kept for conversation id, in the order they were kept.
func (st *Store) Paused(id kernel.ID) []session.QueuedMessage {
	st.mu.Lock()
	defer st.mu.Unlock()
	return append([]session.QueuedMessage(nil), st.paused[id]...)
}

// pause keeps messages Stop took back, once each: one already kept, or one the user has since disposed of, is not
// kept again.
func (st *Store) pause(id kernel.ID, msgs []session.QueuedMessage) {
	st.mu.Lock()
	st.ready()
	added := false
	for _, m := range msgs {
		if sub, ok := submissionOfPaused(m.RequestID); ok && st.disposed[id][sub] {
			continue
		}
		held := false
		for _, p := range st.paused[id] {
			held = held || p.RequestID == m.RequestID
		}
		if !held {
			st.paused[id] = append(st.paused[id], m)
			added = true
		}
	}
	st.mu.Unlock()
	if added {
		st.notify(id)
	}
}

// unpause drops a paused message the user edited or removed, for good.
func (st *Store) unpause(id kernel.ID, requestID string) {
	st.mu.Lock()
	st.ready()
	list := st.paused[id]
	for i, p := range list {
		if p.RequestID == requestID {
			st.paused[id] = append(list[:i:i], list[i+1:]...)
			break
		}
	}
	if len(st.paused[id]) == 0 {
		delete(st.paused, id)
	}
	if sub, ok := submissionOfPaused(requestID); ok {
		st.disposeLocked(id, sub)
	}
	if local, ok := strings.CutPrefix(requestID, sendPrefix); ok {
		st.removedSends[local] = true
	}
	st.mu.Unlock()
	st.notify(id)
}

// withdrawn records that the user took submission sub back from Kernel's queue.
func (st *Store) withdrawn(id kernel.ID, sub int64) {
	st.mu.Lock()
	st.ready()
	st.disposeLocked(id, sub)
	st.mu.Unlock()
}

func (st *Store) disposeLocked(id kernel.ID, sub int64) {
	if st.disposed[id] == nil {
		st.disposed[id] = map[int64]bool{}
	}
	st.disposed[id][sub] = true
	delete(st.sent[id], sub)
}

// sending records a submission about to go out, before Kernel can have it: a Stop from now on waits for its id.
func (st *Store) sending(id kernel.ID, item InboxItem) *inflight {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.ready()
	f := &inflight{item: item, local: uuid.NewString(), done: make(chan struct{})}
	st.inflight[id] = append(st.inflight[id], f)
	return f
}

// submitted records what conversation.submit answered for f: the submission's id, or that it failed and Kernel has
// nothing. A conversation already idle, or a run that already placed it, holds nothing of it queued.
func (st *Store) submitted(id kernel.ID, f *inflight, sub kernel.ID, ok bool) {
	st.mu.Lock()
	st.ready()
	list := st.inflight[id]
	for i, each := range list {
		if each == f {
			st.inflight[id] = append(list[:i:i], list[i+1:]...)
			break
		}
	}
	f.id, f.ok = int64(sub), ok
	placed := st.placed[id][f.id]
	if len(st.inflight[id]) == 0 {
		delete(st.inflight, id)
		delete(st.placed, id)
	}
	if st.removedSends[f.local] {
		delete(st.removedSends, f.local)
		if ok {
			st.disposeLocked(id, f.id)
		}
	}
	changed := false
	if ok {
		// The copy a Stop kept while this was unanswered is now this submission's.
		for i, p := range st.paused[id] {
			if p.RequestID == f.pausedID() {
				st.paused[id][i].RequestID = pausedID(f.id)
				changed = true
			}
		}
	}
	if ok && !placed && !st.disposed[id][f.id] {
		if st.sent[id] == nil {
			st.sent[id] = map[int64]sentInput{}
		}
		item := f.item
		item.ID = f.id
		st.seq++
		st.sent[id][f.id] = sentInput{item: item, at: st.seq}
		st.settleLocked(id, 0)
	}
	st.mu.Unlock()
	close(f.done)
	if changed {
		st.notify(id)
	}
}

// queuedInputs are the inputs Kernel may hold for conversation id, in submission order: the ones its last view
// lists in pi.inbox and the ones this window submitted that it has not seen placed. waiting are this window's sends
// that Kernel has not answered yet; their ids come later.
func (st *Store) queuedInputs(id kernel.ID) (inputs []InboxItem, waiting []*inflight) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.ready()
	st.settleLocked(id, 0)
	seen := map[int64]bool{}
	if state := st.states[id]; state != nil {
		for _, item := range inputsOf(state.InboxItems()) {
			if !st.disposed[id][item.ID] {
				inputs = append(inputs, item)
			}
			seen[item.ID] = true
		}
	}
	for _, sent := range st.sent[id] {
		if !seen[sent.item.ID] {
			inputs = append(inputs, sent.item)
		}
	}
	sortInputs(inputs)
	return inputs, append([]*inflight(nil), st.inflight[id]...)
}

// settled forgets submissions Stop has decided about.
func (st *Store) settled(id kernel.ID, subs []int64) {
	st.mu.Lock()
	for _, sub := range subs {
		delete(st.sent[id], sub)
	}
	if len(st.sent[id]) == 0 {
		delete(st.sent, id)
	}
	st.mu.Unlock()
}

// sentInput is a submission this window recorded, and when: the store's count of recorded submissions at the time.
type sentInput struct {
	item InboxItem
	at   uint64
}

// settleLocked forgets what this window submitted that the conversation's state shows is no longer queued: the
// inputs its run placed and, when the state is an answer to a conversation.open asked once through, every
// submission recorded before then if it is idle. through is 0 for a pushed view, whose idle proves nothing. It is
// called with the store's lock held wherever a state changes, so a placement seen once is not forgotten when the run
// moves on.
func (st *Store) settleLocked(id kernel.ID, through uint64) {
	if len(st.sent[id]) == 0 && len(st.inflight[id]) == 0 {
		return
	}
	state := st.states[id]
	if state == nil || !state.Opened() {
		return
	}
	for _, sub := range state.RunInputs() {
		delete(st.sent[id], sub)
		if len(st.inflight[id]) > 0 {
			if st.placed[id] == nil {
				st.placed[id] = map[int64]bool{}
			}
			st.placed[id][sub] = true
		}
	}
	if through > 0 && !state.Busy() {
		for sub, sent := range st.sent[id] {
			if sent.at <= through {
				delete(st.sent[id], sub)
			}
		}
	}
	if len(st.sent[id]) == 0 {
		delete(st.sent, id)
	}
}

// idleLocked forgets what this window submitted to a conversation that is no longer busy: Kernel holds nothing
// queued for a conversation with no run, so each of them went out or was withdrawn.
func (st *Store) idleLocked(id kernel.ID) {
	delete(st.sent, id)
}

func sortInputs(items []InboxItem) {
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && items[j].ID < items[j-1].ID; j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
}
