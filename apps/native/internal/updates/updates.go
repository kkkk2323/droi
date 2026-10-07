// Package updates is the in-app update: the Desktop Shell's UpdateState
// (check → download → restart), driven by MyGo's updater, which installs
// signed builds, downloading only the delta from the running version when
// a release has one. The new version runs after a relaunch.
package updates

import (
	"context"
	"sync"
)

// Status is where an update is.
type Status string

const (
	Idle        Status = "idle"
	Checking    Status = "checking"
	UpToDate    Status = "up-to-date"
	Available   Status = "available"
	Downloading Status = "downloading"
	Ready       Status = "ready"
	Failed      Status = "error"
)

// State is the Shell's UpdateState.
type State struct {
	Status  Status
	Version string
	// Percent is the download's, 0 to 100.
	Percent int
	// Message says why it failed.
	Message string
}

// Release is a newer version found by a check.
type Release interface {
	Version() string
	// Install downloads, verifies and puts the release in place of the
	// app; progress reports the bytes downloaded of total.
	Install(ctx context.Context, progress func(downloaded, total int64)) error
}

// Source finds releases newer than the running app; nil and no error when
// there is none.
type Source interface {
	Check(ctx context.Context) (Release, error)
}

// Updater walks one update through its states. Its methods are safe from
// any goroutine; OnChange hears every new state.
type Updater struct {
	src Source

	mu        sync.Mutex
	state     State
	release   Release
	listeners []func(State)
}

func New(src Source) *Updater { return &Updater{src: src, state: State{Status: Idle}} }

func (u *Updater) State() State {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.state
}

// OnChange adds a listener for new states.
func (u *Updater) OnChange(fn func(State)) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.listeners = append(u.listeners, fn)
}

func (u *Updater) set(s State) {
	u.mu.Lock()
	u.state = s
	ls := append([]func(State){}, u.listeners...)
	u.mu.Unlock()
	for _, fn := range ls {
		fn(s)
	}
}

// Check looks for a release, unless one is downloading or installed.
func (u *Updater) Check(ctx context.Context) State {
	u.mu.Lock()
	if st := u.state.Status; st == Checking || st == Downloading || st == Ready {
		s := u.state
		u.mu.Unlock()
		return s
	}
	u.mu.Unlock()
	u.set(State{Status: Checking})
	rel, err := u.src.Check(ctx)
	switch {
	case err != nil:
		u.set(State{Status: Failed, Message: "Could not check for updates: " + err.Error()})
	case rel == nil:
		u.set(State{Status: UpToDate})
	default:
		u.mu.Lock()
		u.release = rel
		u.mu.Unlock()
		u.set(State{Status: Available, Version: rel.Version()})
	}
	return u.State()
}

// Install downloads the release Check found and puts it in place; the app
// runs it after a relaunch.
func (u *Updater) Install(ctx context.Context) State {
	u.mu.Lock()
	rel := u.release
	busy := u.state.Status == Downloading || u.state.Status == Ready
	u.mu.Unlock()
	if rel == nil || busy {
		return u.State()
	}
	v := rel.Version()
	u.set(State{Status: Downloading, Version: v})
	last := -1
	err := rel.Install(ctx, func(done, total int64) {
		if total <= 0 {
			return
		}
		if p := int(done * 100 / total); p != last {
			last = p
			u.set(State{Status: Downloading, Version: v, Percent: p})
		}
	})
	if err != nil {
		u.set(State{Status: Failed, Message: "The update did not install: " + err.Error()})
		return u.State()
	}
	u.set(State{Status: Ready, Version: v})
	return u.State()
}

// CheckAndInstall installs the release a check finds, as a background
// check does: the app then restarts into it once idle.
func (u *Updater) CheckAndInstall(ctx context.Context) State {
	if s := u.Check(ctx); s.Status != Available {
		return s
	}
	return u.Install(ctx)
}

// ShouldRecheck is whether the hourly check may run: not while a release
// waits for the user, downloads or is installed.
func ShouldRecheck(s State) bool {
	switch s.Status {
	case Available, Downloading, Ready, Checking:
		return false
	}
	return true
}
