// Command phonegateway is the Phone App's end-to-end Gateway: the real Gateway in front of a Kernel the suite
// started (tests/mobile/fake-kernel.ts), with this computer's folders and git, and a Scratch folder and a worktree
// folder of the suite's own. It is a test tool; no release carries it.
//
// It prints {"url": …} on its first line once it listens, and runs until its stdin closes. Lines on stdin change
// it as a Host would: `token <t>` resets the Pairing Token (and drops the phones that used the old one), `kernel
// <url>` points it at another Kernel ("" while none runs), and `list <json>` sets the window's Session list order,
// which it serves only with -window, as a Gateway with a window does. With -daemon-only it is the Gateway of a
// Host in Daemon mode, which has no Kernel to pass a phone to.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/kkkk2323/droi/apps/native/pkg/gateway"
	"github.com/kkkk2323/droi/apps/native/pkg/host"
)

type state struct {
	mu        sync.Mutex
	token     string
	kernelURL string
	list      *gateway.SessionListOrder
}

func (s *state) get(read func(*state) string) func() string {
	return func() string {
		s.mu.Lock()
		defer s.mu.Unlock()
		return read(s)
	}
}

// sessionList is the window's list order, as `list` set it; 404 until it does.
type sessionList struct{ s *state }

func (l sessionList) Order() gateway.SessionListOrder {
	l.s.mu.Lock()
	defer l.s.mu.Unlock()
	if l.s.list == nil {
		return gateway.SessionListOrder{}
	}
	return *l.s.list
}

func (l sessionList) SetPinned(kind, key string, pinned bool) error {
	l.s.mu.Lock()
	defer l.s.mu.Unlock()
	if l.s.list == nil {
		l.s.list = &gateway.SessionListOrder{}
	}
	list := &l.s.list.PinnedSessions
	if kind == gateway.PinWorkspace {
		list = &l.s.list.PinnedWorkspaces
	}
	kept := (*list)[:0]
	for _, k := range *list {
		if k != key {
			kept = append(kept, k)
		}
	}
	if pinned {
		kept = append(kept, key)
	}
	*list = kept
	return nil
}

func main() {
	port := flag.Int("port", 0, "the port to listen on; 0 picks one")
	token := flag.String("token", "", "the Pairing Token")
	kernelURL := flag.String("kernel-url", "", "the Kernel's /rpc address")
	version := flag.String("version", "0.0.0", "the version /meta answers")
	name := flag.String("name", "Test Mac", "the computer's name")
	computerID := flag.String("computer-id", "c0ffee00-0000-4000-8000-000000000000", "the computer's id")
	scratch := flag.String("scratch", "", "the Scratch folder")
	worktrees := flag.String("worktrees", "", "the folder worktrees go in")
	window := flag.Bool("window", false, "serve the window's Session list order")
	daemonOnly := flag.Bool("daemon-only", false, "be a Gateway whose Host runs no Kernel, as one in Daemon mode")
	flag.Parse()

	s := &state{token: *token, kernelURL: *kernelURL}
	kernelToken := os.Getenv("DROI_KERNEL_TOKEN")
	trash := filepath.Join(*scratch, ".trash")
	opts := gateway.Options{
		Port:         *port,
		DaemonURL:    func() string { return "" },
		KernelURL:    s.get(func(s *state) string { return s.kernelURL }),
		KernelToken:  func() string { return kernelToken },
		PairingToken: s.get(func(s *state) string { return s.token }),
		Version:      *version,
		ComputerName: *name,
		ComputerID:   *computerID,
		Workspaces:   host.NewWorkspaces(func() string { return *worktrees }),
		Scratch: &gateway.ScratchFolders{
			Root: func() string { return *scratch },
			MoveToTrash: func(path string) error {
				if err := os.MkdirAll(trash, 0o755); err != nil {
					return err
				}
				return os.Rename(path, filepath.Join(trash, filepath.Base(path)))
			},
		},
	}
	if *window {
		opts.SessionList = sessionList{s}
	}
	if *daemonOnly {
		opts.KernelURL, opts.KernelToken = nil, nil
	}
	g, err := gateway.Start(opts)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer func() { _ = g.Close() }()
	ready, _ := json.Marshal(map[string]string{"url": g.URL()})
	fmt.Println(string(ready))

	lines := bufio.NewScanner(os.Stdin)
	lines.Buffer(make([]byte, 1<<20), 1<<20)
	for lines.Scan() {
		command, arg, _ := strings.Cut(lines.Text(), " ")
		switch command {
		case "token":
			s.mu.Lock()
			s.token = arg
			s.mu.Unlock()
			g.Revoke(gateway.TokenPairing)
		case "kernel":
			s.mu.Lock()
			s.kernelURL = arg
			s.mu.Unlock()
		case "list":
			var order gateway.SessionListOrder
			if err := json.Unmarshal([]byte(arg), &order); err != nil {
				fmt.Fprintln(os.Stderr, "list:", err)
				continue
			}
			s.mu.Lock()
			s.list = &order
			s.mu.Unlock()
		}
		fmt.Println(`{"ok":true}`)
	}
}
