// Package keepawake keeps the computer from sleeping when idle while
// something holds it, such as a running Automation.
package keepawake

import "sync"

// Keeper counts holds: the first starts keeping the computer awake, the
// release of the last stops it.
type Keeper struct {
	start func() (stop func())

	mu   sync.Mutex
	n    int
	stop func()
}

// New is a Keeper that calls start on the first hold and the stop it
// returned on the release of the last.
func New(start func() (stop func())) *Keeper { return &Keeper{start: start} }

// Hold keeps the computer awake until release is called; calling release
// again does nothing.
func (k *Keeper) Hold() (release func()) {
	k.mu.Lock()
	k.n++
	if k.n == 1 {
		k.stop = k.start()
	}
	k.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			k.mu.Lock()
			defer k.mu.Unlock()
			k.n--
			if k.n == 0 && k.stop != nil {
				k.stop()
				k.stop = nil
			}
		})
	}
}

var std = New(start)

// Hold keeps this computer awake until release is called.
func Hold() (release func()) { return std.Hold() }
