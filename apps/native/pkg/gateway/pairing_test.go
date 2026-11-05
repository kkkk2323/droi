package gateway

import (
	"reflect"
	"strings"
	"testing"
)

func TestPairingLinkUsesTheFirstLANAddress(t *testing.T) {
	lan := []string{"192.168.5.123", "10.0.0.2"}
	got := Pairing(lan, 41417, "abc-123", "")
	want := PairingInfo{Link: "http://192.168.5.123:41417/#pair=abc-123", LANAddresses: lan, Port: 41417}
	if !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
}

func TestNoPairingLinkWithoutALANAddressOrARelay(t *testing.T) {
	if got := Pairing(nil, 41417, "t", ""); got.Link != "" || len(got.LANAddresses) != 0 {
		t.Fatal(got)
	}
}

func TestThePairingLinkCarriesTheRelay(t *testing.T) {
	relay := "https://relay.example.com/c/abcdefghijklmnopqrstuvwx"
	got := Pairing([]string{"192.168.5.123"}, 41417, "t", relay)
	if got.Link != "http://192.168.5.123:41417/#pair=t&relay=https%3A%2F%2Frelay.example.com%2Fc%2Fabcdefghijklmnopqrstuvwx" || got.Relay != relay {
		t.Fatal(got)
	}
	// Without a network address the Relay alone.
	if got := Pairing(nil, 41417, "t", relay).Link; got != relay+"/#pair=t" {
		t.Fatal(got)
	}
	for _, link := range []string{got.Link, Pairing(nil, 41417, "t", relay).Link} {
		l, err := ParsePairingLink(link)
		if err != nil || l.Token != "t" {
			t.Fatalf("%q: %+v %v", link, l, err)
		}
		if l.Relay != relay && l.Gateway != relay {
			t.Fatalf("%q: the Relay was lost: %+v", link, l)
		}
	}
}

func TestGatewayPairingFollowsRemoteAccess(t *testing.T) {
	g := startGateway(t, baseOptions(nil))
	if got := g.Pairing(testToken); got.Link != "" || got.Port != g.Port() {
		t.Fatal(got)
	}
	g.SetRemoteAccess(true)
	defer g.SetRemoteAccess(false)
	lan := g.LANAddresses()
	if len(lan) == 0 {
		t.Skip("no LAN address on this computer")
	}
	got := g.Pairing(testToken)
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
