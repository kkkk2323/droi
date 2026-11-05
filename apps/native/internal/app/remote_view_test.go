package app

import (
	"image/png"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

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
func (r *fakeRemote) Pairing(h, token string) gateway.PairingInfo {
	r.mu.Lock()
	defer r.mu.Unlock()
	return gateway.Pairing(r.lan, 41417, h, token, r.relayAddress)
}
func (r *fakeRemote) Revoke(gateway.TokenKind) { r.revoked++ }

func remoteHarness(t *testing.T, g *fakeRemote) (*harness, *host.Host) {
	t.Helper()
	h0 := testHost(t)
	h := newHarnessWith(t, oneSession(), "", func(cfg *Config) { cfg.Host = h0; cfg.Remote = g })
	// Tall enough for the whole tab, the Relay and the pairing address too.
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

func TestThePairingAddressReplacesTheNetworkAddress(t *testing.T) {
	g := &fakeRemote{lan: []string{"192.168.1.20"}}
	h, h0 := remoteHarness(t, g)
	_ = h0.Settings.Update(func(s *host.Settings) { s.RemoteAccess = true })
	h.settle()
	h.click("Pairing address")
	if !h.tt.Focused("Pairing address") {
		// The row's title shares the name; the field is under it.
		r, _ := h.tt.Find("Save address")
		h.tt.ClickAt(r.X-40, r.Y+r.H/2)
		h.frame()
	}
	if !h.tt.Focused("Pairing address") {
		t.Fatal("the field did not take the focus")
	}
	h.tt.Type("https://laptop.myhome:41417/")
	h.click("Save address")
	h.settle()
	if p := h0.Settings.Get().PairingHost; p == nil || *p != "laptop.myhome" {
		t.Fatalf("saved %v", p)
	}
	found := false
	for _, s := range h.tt.Texts() {
		if strings.HasPrefix(s, "http://laptop.myhome:41417/#pair=") {
			found = true
		}
	}
	if !found || !h.hasText("The pairing link uses laptop.myhome") {
		t.Errorf("%q", h.tt.Texts())
	}
}

func TestTheRelayIsSavedAndItsStateShown(t *testing.T) {
	g := &fakeRemote{lan: []string{"192.168.1.20"}}
	h, h0 := remoteHarness(t, g)
	_ = h0.Settings.Update(func(s *host.Settings) { s.RemoteAccess = true })
	h.settle()
	if !h.hasText("Relay") || !h.hasText("A server this computer connects out to") {
		t.Fatalf("no Relay row: %q", h.tt.Texts())
	}
	h.click("Relay address")
	if !h.tt.Focused("Relay address") {
		t.Fatal("the address field did not take the focus")
	}
	h.tt.Type("relay.example.com/")
	h.click("Registration key")
	h.tt.Type("s3cret")
	h.click("Save relay")
	h.settle()
	s := h0.Settings.Get()
	if s.RelayURL == nil || *s.RelayURL != "https://relay.example.com" || s.RelayKey == nil || *s.RelayKey != "s3cret" {
		t.Fatalf("saved %v %v", s.RelayURL, s.RelayKey)
	}
	eventuallyTrue(t, func() bool { return g.reconcileCount() == 1 })
	if !h.hasText("Connecting to https://relay.example.com…") {
		t.Fatalf("%q", h.tt.Texts())
	}
	if h.hasText("s3cret") {
		t.Error("the registration key is shown")
	}

	g.setRelay(gateway.RelayState{Status: gateway.RelayConnected}, "https://relay.example.com/c/"+s.RelayID)
	h0.Changed()
	h.settle()
	if !h.hasText("Connected to https://relay.example.com. Away from this network, paired phones reach this computer through it.") {
		t.Fatalf("%q", h.tt.Texts())
	}
	want := "http://192.168.1.20:41417/#pair=" + s.PairingToken + "&relay=" + url.QueryEscape("https://relay.example.com/c/"+s.RelayID)
	if !h.hasText(want) {
		t.Fatalf("no link with the relay %q in %q", want, h.tt.Texts())
	}
	if dir := os.Getenv("DROI_NATIVE_SHOTS"); dir != "" {
		f, _ := os.Create(dir + "/remote-access-relay.png")
		_ = png.Encode(f, h.tt.Image())
		f.Close()
	}

	g.setRelay(gateway.RelayState{Status: gateway.RelayRefused, Err: "the relay refused the registration key", Next: time.Now()}, "")
	h0.Changed()
	h.settle()
	found := false
	for _, s := range h.tt.Texts() {
		if strings.HasPrefix(s, "https://relay.example.com refused this computer: the relay refused the registration key.") {
			found = true
		}
	}
	if !found {
		t.Fatalf("%q", h.tt.Texts())
	}
}

func TestARelayAddressThatIsNotOneIsRefused(t *testing.T) {
	g := &fakeRemote{lan: []string{"192.168.1.20"}}
	h, h0 := remoteHarness(t, g)
	_ = h0.Settings.Update(func(s *host.Settings) { s.RemoteAccess = true })
	h.settle()
	h.click("Relay address")
	h.tt.Type("ftp://relay.example.com")
	h.click("Save relay")
	h.settle()
	if h0.Settings.Get().RelayURL != nil || g.reconcileCount() != 0 {
		t.Fatal("saved an address that is not one")
	}
	if !h.hasText(`"ftp://relay.example.com" is not a relay address. Use one such as https://relay.example.com.`) {
		t.Fatalf("%q", h.tt.Texts())
	}
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
