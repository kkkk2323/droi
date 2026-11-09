package fakekernel

import (
	"testing"

	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel"
)

// TestObserverForwardsEveryMethodOfTheContract is the Observer's own guard: the hand-written forwarding
// wrappers and the contract's method list are the same list, in the same order. A method added to
// pkg/kernel and not to the Observer fails here, before a test that expects to observe it fails with a
// confusing "no call" instead.
func TestObserverForwardsEveryMethodOfTheContract(t *testing.T) {
	o := &Observer{}
	got := o.methods()
	if len(got) != len(kernel.Methods) {
		t.Fatalf("the Observer forwards %d methods, the contract has %d", len(got), len(kernel.Methods))
	}
	for i, method := range kernel.Methods {
		if got[i] != method {
			t.Errorf("method %d is %q in the Observer and %q in the contract", i, got[i], method)
		}
	}
}
