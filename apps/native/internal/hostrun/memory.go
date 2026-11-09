package hostrun

import (
	"context"
	"log"
	"sync"

	droid "github.com/kkkk2323/droid-sdk-go"

	"github.com/kkkk2323/droi/apps/native/v2/internal/memorywork"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/host"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel"
)

// StartMemory is the Host's side of Memory (ADR 0031): Settings → Memory,
// and the passes that summarize Sessions and keep the Memory Summary, run
// only while Memory is on. With a Kernel (ADR 0042) the passes read its
// conversations and call model.complete on a connection of their own;
// without one, Memory Sessions dial the Daemon directly with the Host's
// credential, as this window does, until the Daemon is removed.
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
	var kernelOf func() memorywork.Kernel
	var link *host.KernelLink
	var poke func()
	if h.KernelEnabled() {
		// Memory on the Kernel (ADR 0042): its own connection, as the Host's. The link's own current connection
		// is the one used, so a connection it retired is never kept here.
		link = host.NewKernelLink(host.KernelLinkOptions{
			Client: kernel.ClientHost,
			Token:  h.KernelToken,
			// The pass that waited for the Kernel.
			OnClient: func(*kernel.Client, int) { poke() },
			Log:      func(s string) { log.Println("[memory] kernel:", s) },
		})
		kernelOf = func() memorywork.Kernel {
			if c, _ := link.Current(); c != nil {
				return c
			}
			return nil
		}
	}
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
		Kernel:   kernelOf,
		OnChange: changed,
		Log:      func(s string) { log.Println("[memory]", s) },
		Home:     home,
	})
	if link != nil {
		poke = m.Poke
		h.OnKernelState(link.Use)
	}
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
