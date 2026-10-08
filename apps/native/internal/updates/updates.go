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
	// Notes are the release's, in Markdown, while it is available,
	// downloads or is installed.
	Notes string
	// Percent is the download's, 0 to 100.
	Percent int
	// Message says why it failed.
	Message string
}

// Release is a newer version found by a check.
type Release interface {
	Version() string
	// Notes say what changed, in Markdown.
	Notes() string
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

	mu      sync.Mutex
	state   State
	release Release
	// skipped is the version background checks do not install.
	skipped   string
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
		u.set(State{Status: Available, Version: rel.Version(), Notes: rel.Notes()})
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
	v, notes := rel.Version(), rel.Notes()
	u.set(State{Status: Downloading, Version: v, Notes: notes})
	last := -1
	err := rel.Install(ctx, func(done, total int64) {
		if total <= 0 {
			return
		}
		if p := int(done * 100 / total); p != last {
			last = p
			u.set(State{Status: Downloading, Version: v, Notes: notes, Percent: p})
		}
	})
	if err != nil {
		u.set(State{Status: Failed, Message: "The update did not install: " + err.Error()})
		return u.State()
	}
	u.set(State{Status: Ready, Version: v, Notes: notes})
	return u.State()
}

// CheckAndInstall installs the release a check finds, as a background
// check does: the app then restarts into it once idle.
func (u *Updater) CheckAndInstall(ctx context.Context) State {
	s := u.Check(ctx)
	if s.Status != Available {
		return s
	}
	u.mu.Lock()
	skipped := s.Version == u.skipped
	u.mu.Unlock()
	if skipped {
		// Idle, so that the next check can find a newer release.
		u.set(State{Status: Idle})
		return u.State()
	}
	return u.Install(ctx)
}

// Skip keeps background checks from installing version, as Skip this
// version asks; a check by hand still offers it.
func (u *Updater) Skip(version string) {
	u.mu.Lock()
	u.skipped = version
	s := u.state
	u.mu.Unlock()
	if version != "" && s.Status == Available && s.Version == version {
		u.set(State{Status: Idle})
	}
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
