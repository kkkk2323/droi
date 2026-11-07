package kernel

import (
	"strings"
	"testing"
)

func TestReview228R4OtherTrafficMustNotEvictIdleFinalView(t *testing.T) {
	c := &Client{queue: newNotifQueue(), wake: make(chan struct{}, 1)}
	c.enqueue(review228Note(NotifyWarning, `{"kind":"first"}`))
	c.enqueue(review228Note(NotifyConversationView, `{"id":7,"kind":"value","value":{"before":"FINAL"}}`))
	// Conversation 7 now becomes idle; unrelated notifications keep arriving.
	for i := 0; i < 10_000; i++ {
		c.enqueue(review228Note(NotifyWarning, `{"kind":"unrelated"}`))
	}
	for _, n := range review228Drain(c) {
		if strings.Contains(string(n.Params), "FINAL") {
			return
		}
	}
	t.Fatal("unrelated traffic evicted an idle conversation's final state")
}
