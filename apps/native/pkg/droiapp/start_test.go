package droiapp

import (
	"context"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/droidhook"
	"github.com/kkkk2323/droi/apps/native/pkg/extension"
	"github.com/kkkk2323/droi/apps/native/pkg/host"
)

// Each extension's Start gets the Host on a goroutine of its own, so one
// that runs until the app quits does not hold up the next.
func TestExtensionsStartWithTheHost(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	got := make(chan *host.Host, 2)
	work := func(ctx context.Context, h *host.Host) {
		got <- h
		<-ctx.Done()
	}
	x := newExtensions([]extension.Extension{{Start: work}, {Name: "none"}, {Start: work}})
	h := &host.Host{}
	x.start(ctx, h)
	for range 2 {
		select {
		case g := <-got:
			if g != h {
				t.Fatalf("Start got %p, want the Host %p", g, h)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("an extension's Start did not run")
		}
	}
}

// An app's mirror takes the place of Factory's downloads for Droi's droid,
// LATEST included; without one they stay.
func TestAnAppsDroidMirror(t *testing.T) {
	downloads := droidhook.DownloadBase
	t.Cleanup(func() { droidhook.DownloadBase = downloads })
	useDroidMirror(Options{RecipesURL: "https://mirror.example/droi/recipes.json"})
	if droidhook.DownloadBase != downloads {
		t.Fatalf("no mirror moved it: %q", droidhook.DownloadBase)
	}
	useDroidMirror(Options{DroidDownloads: "https://mirror.example/factory-cli/releases"})
	s := droidhook.NewStore(t.TempDir())
	if s.DownloadBase != "https://mirror.example/factory-cli/releases" || s.LatestURL != "https://mirror.example/factory-cli/LATEST" {
		t.Fatalf("store reads %q and %q", s.DownloadBase, s.LatestURL)
	}
}
