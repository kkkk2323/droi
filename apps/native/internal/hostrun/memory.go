package hostrun

import (
	"context"
	"log"
	"sync"

	droid "github.com/kkkk2323/droid-sdk-go"

	"github.com/kkkk2323/droi/apps/native/internal/memorywork"
	"github.com/kkkk2323/droi/apps/native/pkg/host"
)

// StartMemory is the Host's side of Memory (ADR 0031): Settings → Memory,
// and the passes that summarize Sessions and keep the Memory Summary, run
// only while Memory is on and the Daemon runs. Memory Sessions dial the
// Daemon directly with the Host's credential, as this window does.
func StartMemory(h *host.Host, home string, changed func()) *memorywork.Controller {
	dial := func(ctx context.Context) (*droid.Client, error) {
		url, err := h.WaitForDaemonURL(ctx)
		if err != nil {
			return nil, err
		}
		cred, err := h.Credential(ctx)
		if err != nil {
			return nil, err
		}
		return droid.Dial(ctx, droid.Options{URL: url, Credential: cred, Caller: "droi-native"})
	}
	run := memorywork.NewRunner(dial)
	m := memorywork.New(memorywork.Options{
		MemoryDir: h.MemoryDir(),
		Runner: func() memorywork.Run {
			if h.Daemon.State().URL() == "" {
				return nil
			}
			return run
		},
		Models: func() memorywork.Models {
			st := h.Settings.Get()
			return memorywork.Models{SessionSummary: st.MemoryModelOrDefault(), MemorySummary: st.MemorySummaryModelOrDefault()}
		},
		OnChange: changed,
		Log:      func(s string) { log.Println("[memory]", s) },
		Home:     home,
	})
	var mu sync.Mutex
	on, up := false, false
	follow := func() {
		nowOn := h.Settings.Get().MemoryEnabled
		nowUp := h.Daemon.State().URL() != ""
		mu.Lock()
		start, stop, back := nowOn && !on, !nowOn && on, nowOn && nowUp && !up
		on, up = nowOn, nowUp
		mu.Unlock()
		switch {
		case stop:
			go m.Stop()
		case start:
			m.Start()
		case back:
			// The pass that waited for the Daemon.
			m.Poke()
		}
	}
	h.OnChange(follow)
	follow()
	return m
}
