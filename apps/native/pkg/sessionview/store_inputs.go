package sessionview

import (
	"strconv"

	"github.com/kkkk2323/droid-sdk-go/session"

	"github.com/kkkk2323/droi/apps/native/pkg/kernel"
)

// The inputs the window holds on Kernel's behalf, per conversation: what Stop took back from Kernel and kept here as
// paused messages, and what this window submitted that Kernel may still hold queued. They live in the Store rather
// than in a View or a State because both of those are dropped when the user moves on to other Sessions, and these
// are the user's words, which nothing else keeps.

// pausedPrefix marks a paused message's RequestID: "paused:<submissionId>", the submission Stop took it back from,
// so a second Stop that finds the same submission keeps it once.
const pausedPrefix = "paused:"

// Paused are the messages Stop kept for conversation id, in the order they were kept.
func (st *Store) Paused(id kernel.ID) []session.QueuedMessage {
	st.mu.Lock()
	defer st.mu.Unlock()
	return append([]session.QueuedMessage(nil), st.paused[id]...)
}

// pause keeps messages Stop took back, once each: one already kept under its RequestID is not kept again.
func (st *Store) pause(id kernel.ID, msgs []session.QueuedMessage) {
	st.mu.Lock()
	st.ready()
	added := false
	for _, m := range msgs {
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

// unpause drops a paused message the user edited or removed.
func (st *Store) unpause(id kernel.ID, requestID string) {
	st.mu.Lock()
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
	st.mu.Unlock()
	st.notify(id)
}

// submitted records an input this window submitted: Kernel answered its submission id before the view that lists
// it in pi.inbox arrives, and a Stop in between must still find it.
func (st *Store) submitted(id kernel.ID, item InboxItem) {
	st.mu.Lock()
	st.ready()
	if st.sent[id] == nil {
		st.sent[id] = map[int64]InboxItem{}
	}
	st.sent[id][item.ID] = item
	st.mu.Unlock()
}

// queuedInputs are the inputs Kernel may hold for conversation id: the ones its last view lists in pi.inbox, and the
// ones this window submitted since the conversation was last idle that the run in progress has not placed, in
// submission order.
func (st *Store) queuedInputs(id kernel.ID) []InboxItem {
	st.mu.Lock()
	state := st.states[id]
	sent := make([]InboxItem, 0, len(st.sent[id]))
	for _, item := range st.sent[id] {
		sent = append(sent, item)
	}
	st.mu.Unlock()
	seen := map[int64]bool{}
	var out []InboxItem
	if state != nil {
		for _, sub := range state.RunInputs() {
			seen[sub] = true
		}
		for _, item := range inputsOf(state.InboxItems()) {
			out = append(out, item)
			seen[item.ID] = true
		}
	}
	for _, item := range sent {
		if !seen[item.ID] {
			out = append(out, item)
		}
	}
	sortInputs(out)
	return out
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

// idle forgets what this window submitted to a conversation that is no longer busy: Kernel holds nothing queued for
// a conversation with no run, so each of them went out or was withdrawn.
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

func pausedID(sub int64) string { return pausedPrefix + strconv.FormatInt(sub, 10) }
