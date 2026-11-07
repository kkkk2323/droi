// Command fakekernel is the sidecar a test launches through the production
// spawn path. It records how it was started — argv, the token, its working
// directory — and then behaves like Kernel: it prints a handshake and answers
// GET /health until it is asked to stop.
package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
)

func main() {
	token := os.Getenv("DROI_KERNEL_TOKEN")
	record := os.Getenv("FAKE_KERNEL_RECORD")
	port := 0
	data := ""
	args := []string{}
	for i, arg := range os.Args {
		if i+1 < len(os.Args) {
			switch arg {
			case "--port":
				fmt.Sscanf(os.Args[i+1], "%d", &port)
			case "--data":
				data = os.Args[i+1]
			}
		}
		args = append(args, arg)
	}
	cwd, _ := os.Getwd()
	report := map[string]any{
		"argv":          args,
		"tokenInArgv":   strings.Contains(strings.Join(args, " "), token),
		"tokenInEnv":    token != "",
		"cwd":           cwd,
		"dataDir":       data,
		"dataDirInArgv": strings.Contains(strings.Join(args, " "), data),
	}
	if record != "" {
		raw, _ := json.Marshal(report)
		_ = os.WriteFile(record, raw, 0o600)
	}
	if token == "" {
		fmt.Fprintln(os.Stderr, "kernel: DROI_KERNEL_TOKEN is not set")
		os.Exit(2)
	}
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		fmt.Fprintf(os.Stderr, "fakekernel: listen: %v\n", err)
		os.Exit(1)
	}
	actual := listener.Addr().(*net.TCPAddr).Port
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"version":"fake-kernel-1","pi":"1.5.0"}`))
	})
	go func() { _ = http.Serve(listener, mux) }()
	handshake, _ := json.Marshal(map[string]any{"port": actual, "pid": os.Getpid(), "version": "fake-kernel-1"})
	fmt.Println(string(handshake))
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	<-signals
	os.Exit(0)
}
