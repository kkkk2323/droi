package kernel

import (
	"strings"
	"testing"
)

func TestReview228R4OtherTrafficMustNotEvictIdleFinalView(t *testing.T) {
	c := &Client{opts: Options{MaxQueuedNotes: 2}, wake: make(chan struct{}, 1)}
	c.enqueue(review228Note(NotifyWarning, `{"kind":"first"}`))
	c.enqueue(review228Note(NotifyConversationView, `{"id":7,"kind":"value","value":{"before":"FINAL"}}`))
	// Conversation 7 now becomes idle; unrelated notifications keep arriving.
	c.enqueue(review228Note(NotifyWarning, `{"kind":"unrelated"}`))
	for _, n := range c.queued {
		if strings.Contains(string(n.Params), "FINAL") {
			return
		}
	}
	t.Fatalf("unrelated traffic evicted idle conversation's final state with no resync: queue=%v lost=%d", c.queued, c.over)
}
