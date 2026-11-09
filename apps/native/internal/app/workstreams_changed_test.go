package app

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/kkkk2323/droi/apps/native/v2/pkg/gateway"
)

// countingWorkstreams counts the times the window asks for the list.
type countingWorkstreams struct {
	*fakeWorkstreams
	lists atomic.Int32
}

func (c *countingWorkstreams) List(ctx context.Context) ([]gateway.Workstream, error) {
	c.lists.Add(1)
	return c.fakeWorkstreams.List(ctx)
}

// The Host tells the window when its Workstreams change, so that the window
// reads them at once instead of at its next read, half a minute on.
func TestTheWindowReadsTheWorkstreamsAgainWhenTheHostSaysTheyChanged(t *testing.T) {
	fake := &countingWorkstreams{fakeWorkstreams: &fakeWorkstreams{list: []gateway.Workstream{paymentWorkstream()}}}
	var changed func()
	var unsubscribed atomic.Bool
	h := startHarnessGateway(t, workstreamsScenario(), "", "", func(o *gateway.Options) { o.Workstreams = fake }, func(cfg *Config) {
		cfg.WorkstreamsChanged = func(fn func()) func() { changed = fn; return func() { unsubscribed.Store(true) } }
	})
	closeTransport(t, h)
	h.until("the Session list", func() bool { return h.hasText("Payment migration lead") })
	if changed == nil {
		t.Fatal("the window does not hear the Host")
	}
	h.until("the first read", func() bool { return fake.lists.Load() > 0 })
	h.settle()
	before := fake.lists.Load()
	changed()
	h.until("a read after the change", func() bool { return fake.lists.Load() > before })
	h.a.Close()
	if !unsubscribed.Load() {
		t.Error("the window still hears the Host after it closed")
	}
}
