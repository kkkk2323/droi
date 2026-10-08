package updates

import (
	"context"
	"errors"
	"testing"
)

type fakeRelease struct {
	v   string
	err error
}

func (r fakeRelease) Version() string { return r.v }
func (r fakeRelease) Notes() string   { return "- Faster" }
func (r fakeRelease) Install(_ context.Context, progress func(int64, int64)) error {
	progress(50, 100)
	progress(100, 100)
	return r.err
}

type fakeSource struct {
	rel Release
	err error
}

func (s fakeSource) Check(context.Context) (Release, error) { return s.rel, s.err }

func TestUpdaterWalksCheckDownloadReady(t *testing.T) {
	u := New(fakeSource{rel: fakeRelease{v: "1.34.0"}})
	var seen []Status
	var percents []int
	u.OnChange(func(s State) {
		seen = append(seen, s.Status)
		if s.Status == Downloading {
			percents = append(percents, s.Percent)
		}
	})
	if s := u.Check(context.Background()); s.Status != Available || s.Version != "1.34.0" {
		t.Fatalf("check: %+v", s)
	}
	if s := u.Install(context.Background()); s.Status != Ready {
		t.Fatalf("install: %+v", s)
	}
	want := []Status{Checking, Available, Downloading, Downloading, Downloading, Ready}
	if len(seen) != len(want) {
		t.Fatalf("states %v, want %v", seen, want)
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Fatalf("states %v, want %v", seen, want)
		}
	}
	if len(percents) != 3 || percents[2] != 100 {
		t.Errorf("percents %v", percents)
	}
	// An installed release is not checked over.
	if s := u.Check(context.Background()); s.Status != Ready {
		t.Errorf("check after ready: %+v", s)
	}
	if ShouldRecheck(u.State()) {
		t.Error("no recheck once ready")
	}
}

func TestABackgroundCheckInstallsWhatItFinds(t *testing.T) {
	if s := New(fakeSource{rel: fakeRelease{v: "1.34.0"}}).CheckAndInstall(context.Background()); s.Status != Ready || s.Version != "1.34.0" {
		t.Errorf("found: %+v", s)
	}
	if s := New(fakeSource{}).CheckAndInstall(context.Background()); s.Status != UpToDate {
		t.Errorf("none: %+v", s)
	}
}

func TestUpdaterSaysWhatFailed(t *testing.T) {
	if s := New(fakeSource{}).Check(context.Background()); s.Status != UpToDate {
		t.Errorf("no release: %+v", s)
	}
	if s := New(fakeSource{err: errors.New("offline")}).Check(context.Background()); s.Status != Failed || s.Message == "" {
		t.Errorf("check error: %+v", s)
	}
	u := New(fakeSource{rel: fakeRelease{v: "2", err: errors.New("bad signature")}})
	u.Check(context.Background())
	if s := u.Install(context.Background()); s.Status != Failed || !ShouldRecheck(s) {
		t.Errorf("install error: %+v", s)
	}
}

// A skipped version is not installed by the background check, which can
// then find a newer one; a check by hand still offers it.
func TestSkippedVersion(t *testing.T) {
	u := New(fakeSource{rel: fakeRelease{v: "1.34.0"}})
	u.Skip("1.34.0")
	if s := u.CheckAndInstall(context.Background()); s.Status != Idle || !ShouldRecheck(s) {
		t.Fatalf("the background check went to %+v", s)
	}
	if s := u.Check(context.Background()); s.Status != Available || s.Version != "1.34.0" || s.Notes != "- Faster" {
		t.Fatalf("a check by hand found %+v", s)
	}
	u.Skip("1.34.0")
	if s := u.State(); s.Status != Idle {
		t.Fatalf("skipping the available version left %+v", s)
	}
	u.Skip("1.33.0")
	if s := u.CheckAndInstall(context.Background()); s.Status != Ready || s.Notes != "- Faster" {
		t.Fatalf("another skipped version kept 1.34.0 from installing: %+v", s)
	}
}
