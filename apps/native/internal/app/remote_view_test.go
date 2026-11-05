package app

import (
	"image/png"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/internal/relay"
	"github.com/kkkk2323/droi/apps/native/pkg/gateway"
	"github.com/kkkk2323/droi/apps/native/pkg/host"
)

type fakeRemote struct {
	on      []bool
	revoked int
	lan     []string

	mu           sync.Mutex
	reconciled   int
	relayAddress string
	relayState   gateway.RelayState
}

func (r *fakeRemote) ReconcileRelay() { r.mu.Lock(); r.reconciled++; r.mu.Unlock() }
func (r *fakeRemote) RelayState() gateway.RelayState {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.relayState
}
func (r *fakeRemote) reconcileCount() int { r.mu.Lock(); defer r.mu.Unlock(); return r.reconciled }
func (r *fakeRemote) setRelay(s gateway.RelayState, address string) {
	r.mu.Lock()
	r.relayState, r.relayAddress = s, address
	r.mu.Unlock()
}

func (r *fakeRemote) SetRemoteAccess(on bool) { r.on = append(r.on, on) }
func (r *fakeRemote) Pairing(token string) gateway.PairingInfo {
	r.mu.Lock()
	defer r.mu.Unlock()
	return gateway.Pairing(r.lan, 41417, token, r.relayAddress)
}
func (r *fakeRemote) Revoke(gateway.TokenKind) { r.revoked++ }

func remoteHarness(t *testing.T, g *fakeRemote) (*harness, *host.Host) {
	t.Helper()
	h0 := testHost(t)
	h := newHarnessWith(t, oneSession(), "", func(cfg *Config) { cfg.Host = h0; cfg.Remote = g })
	// Tall enough for the whole tab, the Relay's steps too.
	h.tt.SetSize(refW, 1400)
	h.a.Go(Route{Name: "settings", Tab: "remote"})
	h.settle()
	return h, h0
}

func TestRemoteAccessPairsAPhone(t *testing.T) {
	g := &fakeRemote{lan: []string{"192.168.1.20"}}
	h, h0 := remoteHarness(t, g)
	if !h.hasText("Turn on Remote Access to pair a phone.") {
		t.Fatalf("off: %q", h.tt.Texts())
	}
	clickSwitchBeside(h, []string{"Off: only this window"}, func() bool { return h0.Settings.Get().RemoteAccess })
	h.settle()
	if !h0.Settings.Get().RemoteAccess || len(g.on) != 1 || !g.on[0] {
		t.Fatal("the switch did not turn Remote Access on")
	}
	token := h0.Settings.Get().PairingToken
	link := "http://192.168.1.20:41417/#pair=" + token
	if !h.hasText(link) {
		t.Fatalf("no pairing link %q in %q", link, h.tt.Texts())
	}
	if r, ok := h.tt.Find("Pairing QR code"); !ok || r.W < 150 {
		t.Fatalf("no QR code: %+v", r)
	}
	if dir := os.Getenv("DROI_NATIVE_SHOTS"); dir != "" {
		f, _ := os.Create(dir + "/remote-access.png")
		_ = png.Encode(f, h.tt.Image())
		f.Close()
	}
	h.click("Copy link")
	if h.tt.Clipboard() != link || !h.hasText("Copied") {
		t.Error("Copy link did not copy the link")
	}
	h.click("Reset pairing token")
	h.settle()
	if h0.Settings.Get().PairingToken == token || g.revoked != 1 {
		t.Fatal("Reset did not make a new token and revoke the phones")
	}
	if h.hasText(token) {
		t.Error("the old link is still shown")
	}
}

func TestRemoteAccessWithoutANetwork(t *testing.T) {
	g := &fakeRemote{}
	h, h0 := remoteHarness(t, g)
	_ = h0.Settings.Update(func(s *host.Settings) { s.RemoteAccess = true })
	h.settle()
	if !h.hasText("No network address found.") {
		t.Fatalf("%q", h.tt.Texts())
	}
}

func TestARelayIsSetUpStepByStep(t *testing.T) {
	g := &fakeRemote{lan: []string{"192.168.1.20"}}
	h, h0 := remoteHarness(t, g)
	if !hasPrefix(h, "Lets a phone on another network reach this computer.") {
		t.Fatalf("no Relay row: %q", h.tt.Texts())
	}
	h.click("Set up relay")
	h.settle()
	if !h.hasText("Get a server and a domain name") || !h.hasText("Paste the relay address") {
		t.Fatalf("no steps: %q", h.tt.Texts())
	}
	h.click("Domain name")
	h.tt.Type("relay.example.org")
	h.frame()
	command := "curl -fsSL https://github.com/kkkk2323/droi/releases/latest/download/install-relay.sh | sudo sh -s -- --domain relay.example.org"
	if !h.hasText(command) {
		t.Fatalf("the command does not name the domain: %q", h.tt.Texts())
	}
	if dir := os.Getenv("DROI_NATIVE_SHOTS"); dir != "" {
		f, _ := os.Create(dir + "/remote-access-relay-setup.png")
		_ = png.Encode(f, h.tt.Image())
		f.Close()
	}
	h.click("Copy command")
	if h.tt.Clipboard() != command {
		t.Fatalf("copied %q", h.tt.Clipboard())
	}
	h.click("Relay address")
	h.tt.Type("https://relay.example.org/#key=s3cret")
	h.click("Connect relay")
	h.settle()
	s := h0.Settings.Get()
	if s.RelayURL == nil || *s.RelayURL != "https://relay.example.org" || s.RelayKey == nil || *s.RelayKey != "s3cret" {
		t.Fatalf("saved %v %v", s.RelayURL, s.RelayKey)
	}
	// The Relay needs Remote Access, which was off.
	if !s.RemoteAccess || len(g.on) != 1 || !g.on[0] {
		t.Fatalf("Remote Access was not turned on: %v %v", s.RemoteAccess, g.on)
	}
	if h.hasText("Paste the relay address") || !h.hasText("Connecting to relay.example.org…") {
		t.Fatalf("%q", h.tt.Texts())
	}
	for _, text := range h.tt.Texts() {
		if strings.Contains(text, "s3cret") {
			t.Errorf("the key is shown: %q", text)
		}
	}

	g.setRelay(gateway.RelayState{Status: gateway.RelayConnected}, "https://relay.example.org/c/"+s.RelayID)
	h0.Changed()
	h.settle()
	if !h.hasText("Connected to relay.example.org. Away from this network, paired phones reach this computer through it.") {
		t.Fatalf("%q", h.tt.Texts())
	}
	want := "http://192.168.1.20:41417/#pair=" + s.PairingToken + "&relay=" + url.QueryEscape("https://relay.example.org/c/"+s.RelayID)
	if !h.hasText(want) {
		t.Fatalf("no link with the relay %q in %q", want, h.tt.Texts())
	}
	if strings.Contains(want, "s3cret") {
		t.Fatal("the pairing link carries the relay's key")
	}
	if dir := os.Getenv("DROI_NATIVE_SHOTS"); dir != "" {
		f, _ := os.Create(dir + "/remote-access-relay.png")
		_ = png.Encode(f, h.tt.Image())
		f.Close()
	}

	g.setRelay(gateway.RelayState{Status: gateway.RelayRefused, Code: relay.CloseBadKey, Err: "Invalid registration key", Next: time.Now()}, "")
	h0.Changed()
	h.settle()
	if !h.hasText("relay.example.org refused this computer: the key in the relay address is wrong or missing. On the server, run sudo droi-relay address, then click Change relay and paste the whole address.") {
		t.Fatalf("%q", h.tt.Texts())
	}
	g.setRelay(gateway.RelayState{Status: gateway.RelayRetrying, Err: "tls: failed to verify certificate: x509: certificate has expired or is not yet valid", Next: time.Now()}, "")
	h0.Changed()
	h.settle()
	if !hasPrefix(h, "Cannot reach relay.example.org: its HTTPS certificate has expired.") {
		t.Fatalf("%q", h.tt.Texts())
	}
	g.setRelay(gateway.RelayState{Status: gateway.RelayRetrying, Err: "dial tcp: i/o timeout", Next: time.Now()}, "")
	h0.Changed()
	h.settle()
	if !hasPrefix(h, "Cannot reach relay.example.org (dial tcp: i/o timeout). Check that the relay runs on the server") {
		t.Fatalf("%q", h.tt.Texts())
	}

	h.click("Change relay")
	h.settle()
	if !h.hasText("Paste the relay address") {
		t.Fatalf("Change relay did not show the steps: %q", h.tt.Texts())
	}
	h.click("Cancel")
	h.settle()
	n := g.reconcileCount()
	h.click("Remove relay")
	h.settle()
	if s := h0.Settings.Get(); s.RelayURL != nil || s.RelayKey != nil {
		t.Fatalf("Remove relay kept %v %v", s.RelayURL, s.RelayKey)
	}
	eventuallyTrue(t, func() bool { return g.reconcileCount() == n+1 })
	if !h.hasText("Set up relay") {
		t.Fatalf("%q", h.tt.Texts())
	}
}

func TestARelayWaitsForRemoteAccess(t *testing.T) {
	g := &fakeRemote{lan: []string{"192.168.1.20"}}
	h, h0 := remoteHarness(t, g)
	_ = h0.Settings.Update(func(s *host.Settings) { s.RelayURL = host.OptString("https://relay.example.org") })
	h0.Changed()
	h.settle()
	if !h.hasText("Paused") || !h.hasText("Remote Access is off, so this computer does not connect to the relay. Turn on Remote Access above and it connects.") {
		t.Fatalf("%q", h.tt.Texts())
	}
}

func TestARelayAddressThatIsNotOneIsRefused(t *testing.T) {
	g := &fakeRemote{lan: []string{"192.168.1.20"}}
	h, h0 := remoteHarness(t, g)
	h.click("Set up relay")
	h.settle()
	h.click("Relay address")
	h.tt.Type("ftp://relay.example.com")
	h.click("Connect relay")
	h.settle()
	if h0.Settings.Get().RelayURL != nil || g.reconcileCount() != 0 || len(g.on) != 0 {
		t.Fatal("saved an address that is not one")
	}
	if !h.hasText("A relay address starts with https://. Copy the whole address the server printed.") {
		t.Fatalf("%q", h.tt.Texts())
	}
}

func hasPrefix(h *harness, prefix string) bool {
	for _, s := range h.tt.Texts() {
		if strings.HasPrefix(s, prefix) {
			return true
		}
	}
	return false
}

func eventuallyTrue(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
