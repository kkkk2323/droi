package gateway

import (
	"reflect"
	"strings"
	"testing"
)

func TestPairingLinkUsesTheFirstLANAddressOrThePairingHost(t *testing.T) {
	lan := []string{"192.168.5.123", "10.0.0.2"}
	got := Pairing(lan, 41417, "", "abc-123")
	want := PairingInfo{Link: "http://192.168.5.123:41417/#pair=abc-123", LANAddresses: lan, Port: 41417}
	if !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
	if got := Pairing(lan, 41417, "laptop.myhome", "abc-123").Link; got != "http://laptop.myhome:41417/#pair=abc-123" {
		t.Fatal(got)
	}
}

func TestNoPairingLinkWithoutALANAddressEvenWithAPairingHost(t *testing.T) {
	if got := Pairing(nil, 41417, "laptop.myhome", "t"); got.Link != "" || len(got.LANAddresses) != 0 {
		t.Fatal(got)
	}
}

func TestGatewayPairingFollowsRemoteAccess(t *testing.T) {
	g := startGateway(t, baseOptions(nil))
	if got := g.Pairing("", testToken); got.Link != "" || got.Port != g.Port() {
		t.Fatal(got)
	}
	g.SetRemoteAccess(true)
	defer g.SetRemoteAccess(false)
	lan := g.LANAddresses()
	if len(lan) == 0 {
		t.Skip("no LAN address on this computer")
	}
	got := g.Pairing("", testToken)
	if !strings.HasPrefix(got.Link, "http://"+lan[0]+":") || !strings.HasSuffix(got.Link, "/#pair="+testToken) {
		t.Fatal(got.Link)
	}
}

func TestLANInterfaceAddressesAreIPv4AndNotLoopback(t *testing.T) {
	for _, a := range LANInterfaceAddresses() {
		if strings.Contains(a, ":") || strings.HasPrefix(a, "127.") {
			t.Fatal(a)
		}
	}
}

func TestComputerNameIsNotEmpty(t *testing.T) {
	if name := ComputerName(); name == "" || strings.HasSuffix(name, ".local") {
		t.Fatal(name)
	}
}
