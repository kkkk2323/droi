//go:build !windows

package hostrun

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/v2/pkg/host"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel/fakekernel"
)

// `droi host` runs the Kernel its package puts beside the executable, as Linux's tar.gz and .deb do, when
// DROI_KERNEL asks for it, and stops it with the Host.
func TestHostRunsTheKernelBesideItsExecutable(t *testing.T) {
	bin := fakekernel.Binary()
	if bin == "" {
		if os.Getenv(fakekernel.RequiredEnv) != "" {
			t.Fatal("no Kernel binary to run")
		}
		t.Skip("no Kernel binary: run `pnpm build:kernel`")
	}
	b := newBox(t, "0.240.0")
	b.env[host.KernelEnv] = "1"
	installed := t.TempDir()
	if err := os.Link(bin, filepath.Join(installed, "kernel")); err != nil {
		t.Fatalf("put the Kernel beside the executable: %v", err)
	}
	hosts := make(chan *host.Host, 1)
	running := make(chan host.KernelState, 1)
	r := b.start(func(o *Options) {
		o.Executable = filepath.Join(installed, "droi")
		o.Start = func(_ context.Context, h *host.Host) {
			hosts <- h
			h.OnKernelState(func(s host.KernelState) {
				if s.RPCURL() != "" {
					select {
					case running <- s:
					default:
					}
				}
			})
		}
	})
	h := <-hosts
	var state host.KernelState
	select {
	case state = <-running:
	case <-time.After(60 * time.Second):
		t.Fatalf("the Kernel did not start:\n%s", r.out.String())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c, err := kernel.Dial(ctx, kernel.Options{URL: state.RPCURL(), Token: h.KernelToken(), Client: kernel.ClientHost})
	if err != nil {
		t.Fatal(err)
	}
	info, err := c.Harness(ctx)
	_ = c.Close()
	if err != nil || !strings.HasPrefix(info.Version, "kernel-") {
		t.Fatalf("harness.info %+v, %v", info, err)
	}
	if err := r.stop(); err != nil {
		t.Fatal(err)
	}
	if waitNotAlive(state.PID, 15*time.Second) {
		t.Fatalf("the Kernel (pid %d) outlived the Host", state.PID)
	}
}
