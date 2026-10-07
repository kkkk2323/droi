package main

import (
	"context"
	"log"
	"sync"

	droid "github.com/kkkk2323/droi/packages/droid-sdk-go"

	"github.com/kkkk2323/droi/apps/native/internal/host"
	"github.com/kkkk2323/droi/apps/native/internal/memorywork"
)

// startMemory is the Host's side of Memory (ADR 0011): Settings → Memory,
// consolidation, and the extractions the hook asks for, handled only while
// Memory is on and the Daemon runs. Memory Sessions dial the Daemon
// directly with the Host's credential, as this window does.
func startMemory(h *host.Host, home string, changed func()) *memorywork.Controller {
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
		ModelID:  func() string { return h.Settings.Get().MemoryModelOrDefault() },
		OnChange: changed,
		Log:      func(s string) { log.Println("[memory]", s) },
		Home:     home,
	})
	var mu sync.Mutex
	watching, running := false, false
	follow := func() {
		on := h.Settings.Get().MemoryEnabled
		up := h.Daemon.State().URL() != ""
		mu.Lock()
		start, stop := on && !watching, !on && watching
		back := on && up && !running
		watching, running = on, up
		mu.Unlock()
		switch {
		case stop:
			m.Stop()
		case start:
			m.WatchRequests()
		}
		// Requests that waited while the Daemon was down.
		if back && !start {
			go m.ProcessRequests()
		}
	}
	h.OnChange(follow)
	follow()
	return m
}
