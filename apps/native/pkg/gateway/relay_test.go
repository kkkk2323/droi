package gateway

import (
	"context"
	"errors"
	"net"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/v2/internal/relay"
)

const testRelayID = "abcdefghijklmnopqrstuvwx"

func startRelay(t *testing.T) *httptest.Server {
	t.Helper()
	s := relay.NewServer(relay.ServerOptions{Logf: t.Logf})
	srv := httptest.NewServer(s)
	t.Cleanup(func() {
		s.Close()
		srv.Close()
	})
	return srv
}

func TestAPhoneReachesTheDaemonThroughTheRelay(t *testing.T) {
	relaySrv := startRelay(t)
	var changes atomic.Int32
	opts := baseOptions(startStandInDaemon(t))
	opts.Relay = func() RelayConfig { return RelayConfig{URL: relaySrv.URL + "/", ID: testRelayID} }
	opts.Changed = func() { changes.Add(1) }
	g := startGateway(t, opts)
	if g.RelayState().Status != RelayOff || g.RelayAddress() != "" {
		t.Fatal("the Relay runs while Remote Access is off")
	}
	g.SetRemoteAccess(true)
	eventually(t, func() bool { return g.RelayState().Status == RelayConnected })
	base := g.RelayAddress()
	if base != relaySrv.URL+"/c/"+testRelayID || g.Pairing(testToken).Relay != base {
		t.Fatalf("Relay address %q", base)
	}
	if changes.Load() == 0 {
		t.Error("Changed was not called")
	}

	ctx := context.Background()
	meta, err := FetchMeta(ctx, base)
	// /meta names the Relay, which a phone paired before it follows.
	if err != nil || meta.ComputerID != baseOptions(nil).ComputerID || meta.Relay != base {
		t.Fatalf("meta %+v, %v", meta, err)
	}
	if err := CheckToken(ctx, base, testToken); err != nil {
		t.Fatal(err)
	}
	if err := CheckToken(ctx, base, "wrong"); !errors.Is(err, ErrTokenRejected) {
		t.Fatalf("a wrong token: %v", err)
	}
	socketURL, _ := SocketURL(base, testToken)
	phone := connect(t, socketURL)
	send(t, phone, "through the relay")
	if got := nextMessage(t, phone); got != "echo:through the relay" {
		t.Fatal(got)
	}
	client, err := NewClient(socketURL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.CreateScratch(ctx); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a Gateway without Scratch folders through the relay: %v", err)
	}

	g.Revoke(TokenPairing)
	waitClosed(t, phone)

	g.SetRemoteAccess(false)
	eventually(t, func() bool { return g.RelayState().Status == RelayOff })
	eventually(t, func() bool {
		_, err := FetchMeta(ctx, base)
		return errors.Is(err, ErrNotGateway)
	})
}

func TestTheRelayFollowsItsSettings(t *testing.T) {
	relaySrv := startRelay(t)
	var mu sync.Mutex
	cfg := RelayConfig{}
	opts := baseOptions(startStandInDaemon(t))
	opts.RemoteAccess = true
	opts.Relay = func() RelayConfig { mu.Lock(); defer mu.Unlock(); return cfg }
	g := startGateway(t, opts)
	if g.RelayState().Status != RelayOff {
		t.Fatal("a Relay without an address runs")
	}
	mu.Lock()
	cfg = RelayConfig{URL: relaySrv.URL, ID: testRelayID}
	mu.Unlock()
	g.ReconcileRelay()
	eventually(t, func() bool { return g.RelayState().Status == RelayConnected })
	base := relaySrv.URL + "/c/" + testRelayID
	if link := g.Pairing(testToken).Link; link != base+"/#pair="+testToken && !strings.HasSuffix(link, "&relay="+url.QueryEscape(base)) {
		t.Fatalf("link %q", link)
	}
	mu.Lock()
	cfg = RelayConfig{}
	mu.Unlock()
	g.ReconcileRelay()
	if g.RelayState().Status != RelayOff || g.RelayAddress() != "" {
		t.Fatal("the Relay still runs without an address")
	}
}

func TestTheGatewayFollowsTheNetwork(t *testing.T) {
	real := LANInterfaceAddresses()
	if len(real) == 0 {
		t.Skip("no LAN address on this computer")
	}
	var mu sync.Mutex
	var present []string
	oldAddrs, oldPoll := lanAddresses, lanPoll
	lanAddresses = func() []string { mu.Lock(); defer mu.Unlock(); return slices.Clone(present) }
	lanPoll = 20 * time.Millisecond
	t.Cleanup(func() { lanAddresses, lanPoll = oldAddrs, oldPoll })

	var changes atomic.Int32
	opts := baseOptions(startStandInDaemon(t))
	opts.Changed = func() { changes.Add(1) }
	g := startGateway(t, opts)
	g.SetRemoteAccess(true)
	if len(g.LANAddresses()) != 0 || !g.RemoteAccess() {
		t.Fatal("no network, yet listening, or Remote Access off")
	}
	before := changes.Load()
	mu.Lock()
	present = []string{real[0]}
	mu.Unlock()
	eventually(t, func() bool { return slices.Equal(g.LANAddresses(), []string{real[0]}) })
	eventually(t, func() bool { return changes.Load() > before })
	lanURL := "http://" + net.JoinHostPort(real[0], strconv.Itoa(g.Port()))
	resp, err := freshClient.Get(lanURL + MetaPath)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	mu.Lock()
	present = nil
	mu.Unlock()
	eventually(t, func() bool { return len(g.LANAddresses()) == 0 })
	eventually(t, func() bool {
		resp, err := freshClient.Get(lanURL + MetaPath)
		if err == nil {
			resp.Body.Close()
		}
		return err != nil
	})
	g.SetRemoteAccess(false)
	mu.Lock()
	present = []string{real[0]}
	mu.Unlock()
	time.Sleep(100 * time.Millisecond)
	if len(g.LANAddresses()) != 0 {
		t.Fatal("listening again with Remote Access off")
	}
}

func TestASleepIsToldFromTheWallClock(t *testing.T) {
	at := time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC)
	if slept(at, at.Add(3*time.Second), 3*time.Second) || slept(at, at.Add(10*time.Second), 3*time.Second) {
		t.Fatal("a late tick read as a sleep")
	}
	if !slept(at, at.Add(5*time.Minute), 3*time.Second) {
		t.Fatal("five minutes away did not read as a sleep")
	}
}

// TestLiveRelay runs a Gateway against a Relay already running, such as
// "droi relay" on a server, when DROI_RELAY_URL names it (and
// DROI_RELAY_KEY its registration key): a phone's /meta, pairing check
// and Daemon socket go through it.
func TestLiveRelay(t *testing.T) {
	relayURL := os.Getenv("DROI_RELAY_URL")
	if relayURL == "" {
		t.Skip("set DROI_RELAY_URL to a running Relay")
	}
	opts := baseOptions(startStandInDaemon(t))
	opts.RemoteAccess = true
	id := "live-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	for len(id) < 16 {
		id += "x"
	}
	opts.Relay = func() RelayConfig { return RelayConfig{URL: relayURL, Key: os.Getenv("DROI_RELAY_KEY"), ID: id} }
	g := startGateway(t, opts)
	deadline := time.Now().Add(20 * time.Second)
	for g.RelayState().Status != RelayConnected {
		if time.Now().After(deadline) {
			t.Fatalf("not connected: %+v", g.RelayState())
		}
		time.Sleep(50 * time.Millisecond)
	}
	base := g.RelayAddress()
	ctx := context.Background()
	start := time.Now()
	if _, err := FetchMeta(ctx, base); err != nil {
		t.Fatal(err)
	}
	t.Logf("/meta through the relay in %v", time.Since(start))
	if err := CheckToken(ctx, base, testToken); err != nil {
		t.Fatal(err)
	}
	socketURL, _ := SocketURL(base, testToken)
	phone := connect(t, socketURL)
	start = time.Now()
	for i := range 20 {
		send(t, phone, "ping "+strconv.Itoa(i))
		if got := nextMessage(t, phone); got != "echo:ping "+strconv.Itoa(i) {
			t.Fatal(got)
		}
	}
	t.Logf("20 round trips on the Daemon socket in %v", time.Since(start))
}
