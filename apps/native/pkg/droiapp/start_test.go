package droiapp

import (
	"context"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/droidpatch"
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

// An app's mirror takes the place of Droi's GitHub release and Factory's
// downloads for the Patched Droid; without one they stay.
func TestAnAppsDroidMirror(t *testing.T) {
	recipes, downloads := droidpatch.RecipesURL, droidpatch.DownloadBase
	t.Cleanup(func() { droidpatch.RecipesURL, droidpatch.DownloadBase = recipes, downloads })
	useDroidMirror(Options{})
	if droidpatch.RecipesURL != recipes || droidpatch.DownloadBase != downloads {
		t.Fatalf("no mirror moved them: %q %q", droidpatch.RecipesURL, droidpatch.DownloadBase)
	}
	useDroidMirror(Options{RecipesURL: "https://mirror.example/droi/recipes.json", DroidDownloads: "https://mirror.example/droid"})
	s := droidpatch.NewStore(t.TempDir())
	if s.RecipesURL != "https://mirror.example/droi/recipes.json" || s.DownloadBase != "https://mirror.example/droid" {
		t.Fatalf("store reads %q and %q", s.RecipesURL, s.DownloadBase)
	}
}
