package host

import (
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel"
)

// arrival is one connection the link handed over.
type arrival struct {
	client *kernel.Client
	gen    int
}

// waitArrival is the next connection the link handed over.
func waitArrival(t *testing.T, ch <-chan arrival) arrival {
	t.Helper()
	select {
	case a := <-ch:
		return a
	case <-time.After(90 * time.Second):
		t.Fatal("the link handed over no connection")
		return arrival{}
	}
}
