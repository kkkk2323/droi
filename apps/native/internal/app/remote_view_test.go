package app

import (
	"image/png"
	"os"
	"strings"
	"testing"

	"github.com/kkkk2323/droi/apps/native/internal/gateway"
	"github.com/kkkk2323/droi/apps/native/internal/host"
)

type fakeRemote struct {
	on      []bool
	revoked int
	lan     []string
}

func (r *fakeRemote) SetRemoteAccess(on bool) { r.on = append(r.on, on) }
func (r *fakeRemote) Pairing(h, token string) gateway.PairingInfo {
	return gateway.Pairing(r.lan, 41417, h, token)
}
func (r *fakeRemote) Revoke(gateway.TokenKind) { r.revoked++ }

func remoteHarness(t *testing.T, g *fakeRemote) (*harness, *host.Host) {
	t.Helper()
	h0 := testHost(t)
	h := newHarnessWith(t, oneSession(), "", func(cfg *Config) { cfg.Host = h0; cfg.Remote = g })
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
