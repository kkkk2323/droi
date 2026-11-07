package kernel

import (
	"encoding/json"
	"sync"
)

// The queue's per-kind bounds. They are constants rather than settings: the
// point of a bound is that a reader that stops reading costs a known amount of
// memory, and a bound that can be raised is one that will be.
const (
	// queueMaxRequests bounds queued approval/ask request notifications, and
	// the resolutions of requests already answered. An approval that is dropped
	// leaves a session waiting forever, so this bucket does not drop: it ends
	// the connection, and the Client that reconnects opens its conversations
	// again.
	queueMaxRequests = 256
	// queueConversations is the room the three buckets that hold one state per
	// conversation — the whole view, the busy flag, the index — have. It is a
	// floor rather than a count of what this Client opened, because a Kernel
	// pushes the state of a conversation it has just closed, and because a Host
	// that keeps a list open watches more than it has opened in this connection.
	queueConversations = 256
	// queueMaxSteps bounds the notifications that name a task or a process:
	// only the newest one per id matters, and there can be thousands of ids.
	queueMaxSteps = 4096
	// queueMaxViewSteps bounds conversation.view pushes that are not whole
	// views: a delta subscriber's ops, a failed view, a retired watch. They are
	// the one kind that cannot be folded — a delta reader applies each push's ops
	// to the state it keeps, in order, and a folded one leaves it wrong — so this
	// bucket ends the connection at its bound instead. The Client that reconnects
	// opens its conversations again and starts from a whole view.
	queueMaxViewSteps = 4096
	// queueMaxWarnings bounds the warnings kept. A warning is advice, not a
	// step: the newest ones are what a reader needs, and how many it never saw
	// travels on the next one, in Notification.Dropped.
	queueMaxWarnings = 256
	// queueMaxUnknown bounds notifications of a method this Client does not
	// know: forwarded so nothing is silently swallowed, bounded so a newer
	// Kernel cannot grow the queue without limit.
	queueMaxUnknown = 16
)

// notifQueue is what Kernel sent and the reader has not taken yet, folded by
// kind and bounded per kind.
//
// Folding, not dropping, is what makes a reader that fell behind survive: a
// conversation's whole view is its state, so the newest one is the one that
// matters, and the same is true of one task's or one process's news. What
// cannot be folded — an approval, an ask — is never dropped either: the queue
// ends the connection instead, because a session waiting for an approval that
// will never arrive is worse than a reconnect. A request still queued when its
// resolution arrives is taken out, since no one has to answer it any more.
//
// A taken notification gives its slot back at once: its payload is cleared and
// the slot counts toward compaction, so a reader that keeps up holds nothing.
//
// Order is kept where it carries meaning. A notification that folds replaces
// the one it supersedes *in place* — when nothing that must be applied in
// order arrived in between — and otherwise takes its own place at the end and
// the superseded one is dropped as stale. A reader therefore applies what it
// gets in the order Kernel produced it.
//
// The total bound is 3·256 + 2·256 + 2·4096 + 4096 + 256 + 16 + 1 = 14,353
// live notifications, whatever their size and however long the reader is stuck.
// Two buckets end the connection at their bound rather than drop what a reader
// cannot rebuild: the approvals and asks, and a delta reader's steps.
type notifQueue struct {
	mu sync.Mutex

	// entries is the arrival order. A folded replacement rewrites its slot, and
	// a superseded slot is marked dead; head is how far a reader has got, and
	// the dead slots before it are skipped.
	entries []slot
	head    int
	// dead counts every non-live slot, including the consumed prefix and
	// priority deliveries ahead of head. Only compaction resets this count.
	dead int

	approvals ints
	viewSteps ints
	warnings  ints
	unknown   ints

	views     keyedIndex
	busy      keyedIndex
	index     keyedIndex
	resolved  keyedIndex
	tasks     keyedIndex
	processes keyedIndex
	// mcp is the index of the newest mcp.changed, -1 when there is none: there
	// is one MCP configuration, so one entry.
	mcp int

	// dropped travels on the next retained notification of the same bucket.
	dropped [bucketUnknown + 1]int
}

// slot is one queued notification, and where it belongs.
type slot struct {
	note Notification
	// key is its key in a keyed bucket, "" for the others.
	key string
	// kind is its bucket, so taking it takes it out of that bucket.
	kind bucket
	// live is false once it was superseded: the slot stays so the indices of
	// later entries do not move, and is skipped when read.
	live bool
}

// ints is a FIFO of entry indices.
type ints struct{ items []int }

func (q *ints) push(i int) { q.items = append(q.items, i) }

func (q *ints) pop() (int, bool) {
	if len(q.items) == 0 {
		return 0, false
	}
	i := q.items[0]
	q.items = q.items[1:]
	return i, true
}

// remove takes entry index i out of the FIFO. A reader takes entries in
// arrival order, so i is almost always the front.
func (q *ints) remove(i int) {
	if len(q.items) > 0 && q.items[0] == i {
		q.items = q.items[1:]
		return
	}
	for j, have := range q.items {
		if have == i {
			q.items = append(q.items[:j], q.items[j+1:]...)
			return
		}
	}
}

// keyedIndex is one entry per key: the newest notification of that key, at the
// position the oldest one had.
type keyedIndex struct {
	max   int
	at    map[string]int
	order []string
}

func newKeyedIndex(max int) keyedIndex {
	return keyedIndex{max: max, at: map[string]int{}}
}

// newNotifQueue is an empty queue.
func newNotifQueue() *notifQueue {
	return &notifQueue{
		views:     newKeyedIndex(queueConversations),
		busy:      newKeyedIndex(queueConversations),
		index:     newKeyedIndex(queueConversations),
		resolved:  newKeyedIndex(queueMaxRequests),
		tasks:     newKeyedIndex(queueMaxSteps),
		processes: newKeyedIndex(queueMaxSteps),
		mcp:       -1,
	}
}

// put queues n, and answers true when the queue cannot hold it and the
// connection has to end instead.
func (q *notifQueue) put(n Notification) (overflow bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	kind, key := classify(n)
	switch kind {
	case bucketApproval:
		if len(q.approvals.items) >= queueMaxRequests {
			return true
		}
		q.approvals.push(q.append(n, "", kind))
	case bucketResolved:
		q.removeResolvedRequests(n, key)
		q.fold(&q.resolved, key, n, kind)
	case bucketView:
		q.fold(&q.views, key, n, kind)
	case bucketBusy:
		q.fold(&q.busy, key, n, kind)
	case bucketIndex:
		q.fold(&q.index, key, n, kind)
	case bucketTask:
		q.fold(&q.tasks, key, n, kind)
	case bucketProcess:
		q.fold(&q.processes, key, n, kind)
	case bucketMCP:
		if q.mcp >= 0 {
			n.Folded = q.entries[q.mcp].note.Folded + 1
			q.entries[q.mcp].note = n
			return false
		}
		q.mcp = q.append(n, "mcp", kind)
	case bucketViewStep:
		if len(q.viewSteps.items) >= queueMaxViewSteps {
			// A delta reader this far behind cannot be handed ops it can apply:
			// one is missing, and the state it would build from the rest is
			// wrong. Reopening starts it from a whole view.
			return true
		}
		q.viewSteps.push(q.append(n, "", kind))
	case bucketWarning:
		q.pushBounded(&q.warnings, n, kind, queueMaxWarnings)
	case bucketUnknown:
		q.pushBounded(&q.unknown, n, kind, queueMaxUnknown)
	}
	q.compactIfWorthIt()
	return false
}

// pushBounded appends n to a FIFO bucket, evicting its oldest when full.
func (q *notifQueue) pushBounded(fifo *ints, n Notification, kind bucket, max int) {
	if len(fifo.items) >= max {
		if oldest, ok := fifo.pop(); ok {
			q.evict(oldest)
		}
	}
	fifo.push(q.append(n, "", kind))
}

// append adds n at the end and answers its index.
func (q *notifQueue) append(n Notification, key string, kind bucket) int {
	q.entries = append(q.entries, slot{note: n, key: key, kind: kind, live: true})
	return len(q.entries) - 1
}

// evict marks the entry at i as superseded, so the reader never sees it.
func (q *notifQueue) evict(i int) {
	if i >= 0 && i < len(q.entries) && q.entries[i].live {
		q.dropped[q.entries[i].kind]++
		q.release(i)
	}
}

// release clears the payload immediately; the empty slot is reclaimed later.
func (q *notifQueue) release(i int) {
	q.entries[i] = slot{}
	q.dead++
}

// removeResolvedRequests returns capacity for requests answered by any Client.
// Duplicate requested notifications are retained until their resolution.
func (q *notifQueue) removeResolvedRequests(n Notification, key string) {
	method := NotifyApprovalRequested
	if n.Method == NotifyAskResolved {
		method = NotifyAskRequested
	}
	kept := q.approvals.items[:0]
	for _, i := range q.approvals.items {
		request := q.entries[i].note
		if request.Method == method && fieldKey(request, "requestId") == key {
			q.release(i)
		} else {
			kept = append(kept, i)
		}
	}
	q.approvals.items = kept
}

// fold stores n as the state of key in b, in the place the previous one had
// when nothing that must be applied in order came after it; otherwise it takes
// its own place and the previous one is dropped as stale.
func (q *notifQueue) fold(b *keyedIndex, key string, n Notification, kind bucket) {
	if i, ok := b.at[key]; ok {
		if i >= len(q.entries)-1 && q.entries[i].live {
			// The newest entry of the whole queue: nothing has to be applied
			// between the two states, so the reader may skip the first.
			n.Folded = q.entries[i].note.Folded + 1
			q.entries[i].note = n
			return
		}
		// A step was queued after it, and that step must be applied before this
		// state. The older state is stale either way.
		delete(b.at, key)
		for j, have := range b.order {
			if have == key {
				b.order = append(b.order[:j], b.order[j+1:]...)
				break
			}
		}
		n.Folded = q.entries[i].note.Folded + 1
		q.release(i)
	}
	if len(b.order) >= b.max {
		oldest := b.order[0]
		b.order = b.order[1:]
		if i, ok := b.at[oldest]; ok {
			delete(b.at, oldest)
			q.evict(i)
		}
	}
	b.at[key] = q.append(n, key, kind)
	b.order = append(b.order, key)
}

// take answers the next notification to hand the reader: a question that needs
// an answer first, then the rest in the order Kernel sent them.
func (q *notifQueue) take() (Notification, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.entries == nil {
		return Notification{}, false
	}
	if i, ok := q.approvals.pop(); ok {
		return q.deliverAt(i), true
	}
	for q.head < len(q.entries) {
		i := q.head
		q.head++
		if q.entries[i].live {
			return q.deliverAt(i), true
		}
	}
	return Notification{}, false
}

// deliverAt attaches this bucket's drops and returns the entry's capacity.
func (q *notifQueue) deliverAt(i int) Notification {
	s := &q.entries[i]
	n := s.note
	n.Dropped += q.dropped[s.kind]
	q.dropped[s.kind] = 0
	q.forget(i, s)
	q.release(i)
	q.compactIfWorthIt()
	return n
}

// forget removes a delivered entry from the bucket that holds it, so a later
// notification of the same key starts a new slot rather than reviving this one.
func (q *notifQueue) forget(i int, s *slot) {
	switch s.kind {
	case bucketViewStep:
		q.viewSteps.remove(i)
	case bucketWarning:
		q.warnings.remove(i)
	case bucketUnknown:
		q.unknown.remove(i)
	case bucketResolved, bucketView, bucketBusy, bucketIndex, bucketTask, bucketProcess:
		b := q.bucketOf(s.kind)
		if at, ok := b.at[s.key]; ok && at == i {
			delete(b.at, s.key)
			for j, key := range b.order {
				if key == s.key {
					b.order = append(b.order[:j], b.order[j+1:]...)
					break
				}
			}
		}
	case bucketMCP:
		if q.mcp == i {
			q.mcp = -1
		}
	}
}

// bucketOf is the keyed bucket of a kind.
func (q *notifQueue) bucketOf(kind bucket) *keyedIndex {
	switch kind {
	case bucketResolved:
		return &q.resolved
	case bucketView:
		return &q.views
	case bucketBusy:
		return &q.busy
	case bucketIndex:
		return &q.index
	case bucketTask:
		return &q.tasks
	case bucketProcess:
		return &q.processes
	}
	return nil
}

// compactIfWorthIt drops the slots nobody will read, once they are more than
// half of what the queue holds: a long flood into one bucket must not make the
// queue's memory grow with the flood.
func (q *notifQueue) compactIfWorthIt() {
	if q.dead < 1024 || q.dead*2 < len(q.entries) {
		return
	}
	keep := make([]slot, 0, len(q.entries)-q.dead)
	shift := make([]int, len(q.entries))
	for i, s := range q.entries {
		shift[i] = -1
		if i >= q.head && s.live {
			shift[i] = len(keep)
			keep = append(keep, s)
		}
	}
	rebase := func(i int) int {
		if i < 0 || i >= len(shift) {
			return -1
		}
		return shift[i]
	}
	for _, b := range []*keyedIndex{&q.views, &q.busy, &q.index, &q.resolved, &q.tasks, &q.processes} {
		for key, i := range b.at {
			if at := rebase(i); at >= 0 {
				b.at[key] = at
			} else {
				delete(b.at, key)
			}
		}
	}
	rebaseFIFO := func(fifo *ints) {
		kept := fifo.items[:0]
		for _, i := range fifo.items {
			if at := rebase(i); at >= 0 {
				kept = append(kept, at)
			}
		}
		fifo.items = kept
	}
	rebaseFIFO(&q.approvals)
	rebaseFIFO(&q.viewSteps)
	rebaseFIFO(&q.warnings)
	rebaseFIFO(&q.unknown)
	if q.mcp >= 0 {
		q.mcp = rebase(q.mcp)
	}
	q.entries = keep
	q.head = 0
	q.dead = 0
}

// live is how many notifications the queue would hand a reader now.
func (q *notifQueue) live() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	n := len(q.approvals.items)
	for i := q.head; i < len(q.entries); i++ {
		if q.entries[i].live && q.entries[i].kind != bucketApproval {
			n++
		}
	}
	return n
}

// bucket is a kind of notification, which is also where it waits.
type bucket int

const (
	bucketApproval bucket = iota
	bucketResolved
	bucketView
	bucketViewStep
	bucketBusy
	bucketIndex
	bucketTask
	bucketProcess
	bucketMCP
	bucketWarning
	bucketUnknown
)

// classify names the bucket of n and its key there. A conversation.view that
// carries a whole view is the conversation's state; any other view push is a
// step this Client does not subscribe to, and is kept in arrival order.
func classify(n Notification) (bucket, string) {
	switch n.Method {
	case NotifyApprovalRequested, NotifyAskRequested:
		return bucketApproval, ""
	case NotifyApprovalResolved, NotifyAskResolved:
		return bucketResolved, fieldKey(n, "requestId")
	case NotifyConversationView:
		if _, whole := wholeView(n); whole {
			return bucketView, idKey(n)
		}
		return bucketViewStep, ""
	case NotifyConversationBusy:
		return bucketBusy, idKey(n)
	case NotifyIndexChanged:
		return bucketIndex, idKey(n)
	case NotifyTaskChanged:
		return bucketTask, fieldKey(n, "taskId")
	case NotifyProcessChanged:
		return bucketProcess, idKey(n) + "/" + fieldKey(n, "processId")
	case NotifyMCPChanged:
		return bucketMCP, ""
	case NotifyWarning:
		return bucketWarning, ""
	default:
		return bucketUnknown, ""
	}
}

// idKey is the conversation a notification is about, as a bucket key.
func idKey(n Notification) string {
	id, _ := notificationID(n)
	return id.String()
}

// fieldKey is one string or number field of a notification's params, as a
// bucket key: "" when it is not there, which folds those into one entry rather
// than one entry per notification.
func fieldKey(n Notification, field string) string {
	var p map[string]json.RawMessage
	if json.Unmarshal(n.Params, &p) != nil {
		return ""
	}
	raw, ok := p[field]
	if !ok {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var num ID
	if json.Unmarshal(raw, &num) == nil {
		return num.String()
	}
	return string(raw)
}
