package gateway_test

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	droid "github.com/kkkk2323/droi/packages/droid-sdk-go"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/internal/gateway"
)

// TestLiveGateway puts the Gateway in front of a real Daemon and connects
// as the Phone App does: the Pairing Token in the URL and the placeholder
// key, which the Gateway swaps for the real credential. Only with
// FACTORY_API_KEY set.
func TestLiveGateway(t *testing.T) {
	key := os.Getenv("FACTORY_API_KEY")
	if key == "" {
		t.Skip("FACTORY_API_KEY is not set")
	}
	home, _ := os.UserHomeDir()
	droidPath := filepath.Join(home, ".local", "bin", "droid")
	if p, err := exec.LookPath("droid"); err == nil {
		droidPath = p
	}
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	fake := t.TempDir()
	cmd := exec.Command(droidPath, "daemon", "--host", "127.0.0.1", "--port", fmt.Sprint(port), "--parent-pid", fmt.Sprint(os.Getpid()))
	cmd.Env = append(os.Environ(), "HOME="+fake, "FACTORY_API_KEY="+key)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	daemonURL := fmt.Sprintf("ws://127.0.0.1:%d", port)
	gw, err := gateway.Start(gateway.Options{
		DaemonURL:          func() string { return daemonURL },
		PairingToken:       func() string { return "pair-token" },
		Credential:         func(context.Context) (*droid.Credential, error) { return &droid.Credential{APIKey: key}, nil },
		AppendSystemPrompt: func() string { return "Always answer in lowercase." },
		Version:            "live",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer gw.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	url := strings.Replace(gw.URL(), "http", "ws", 1) + gateway.DaemonPath + "?" + gateway.TokenQuery + "=pair-token"
	var c *droid.Client
	for i := 0; i < 100 && c == nil; i++ {
		c, err = droid.Dial(ctx, droid.Options{URL: url, Credential: &droid.Credential{APIKey: gateway.APIKeyPlaceholder}})
		if c == nil {
			time.Sleep(200 * time.Millisecond)
		}
	}
	if c == nil {
		t.Fatalf("no connection through the Gateway: %v", err)
	}
	defer c.Close()
	init, err := c.InitializeSession(ctx, protocol.InitializeSessionParams{MachineID: "local", Cwd: t.TempDir()})
	if err != nil {
		t.Fatalf("initialize_session through the Gateway: %v", err)
	}
	if init.SessionID == "" {
		t.Fatal("no Session")
	}
	if _, err := droid.Dial(ctx, droid.Options{URL: strings.Replace(url, "pair-token", "wrong", 1)}); err == nil {
		t.Error("a wrong Pairing Token connected")
	}
}
