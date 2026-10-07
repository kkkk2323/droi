package updates

import (
	"context"
	"time"

	"github.com/egoist/mygo"
)

// MyGo is the Source of builds signed by `mygo build`: the update manifest
// of this platform, from where mygo.json's updates point.
type MyGo struct{}

func (MyGo) Check(ctx context.Context) (Release, error) {
	up, err := mygo.Updater.Check(ctx)
	if err != nil || up == nil {
		return nil, err
	}
	return myGoRelease{up}, nil
}

type myGoRelease struct{ up *mygo.Update }

func (r myGoRelease) Version() string { return r.up.Version }

func (r myGoRelease) Install(ctx context.Context, progress func(int64, int64)) error {
	return r.up.Install(ctx, progress)
}

// The Desktop Shell's schedule: a check a while after launch, then hourly.
const (
	FirstCheck   = 15 * time.Second
	RecheckEvery = time.Hour
)

// Schedule checks on that schedule until ctx ends, installing what it finds.
func (u *Updater) Schedule(ctx context.Context) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(FirstCheck):
	}
	u.CheckAndInstall(ctx)
	t := time.NewTicker(RecheckEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if ShouldRecheck(u.State()) {
				u.CheckAndInstall(ctx)
			}
		}
	}
}
