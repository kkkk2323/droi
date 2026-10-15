package gateway

import (
	"errors"
	"testing"
)

// The cases of packages/daemon-layer/src/pairing.test.ts, which the Phone
// App's parsing is held to.
func TestParsePairingLinkReadsTheGatewayAndTokenFromAPastedLink(t *testing.T) {
	for _, tc := range []struct {
		in      string
		gateway string
		token   string
	}{
		{" http://192.168.5.123:41417/#pair=abc-123 ", "http://192.168.5.123:41417", "abc-123"},
		{"http://a:1/#pair=t&gateway=http://b:2", "http://b:2", "t"},
		{"https://box.tail1234.ts.net:41417/#pair=t", "https://box.tail1234.ts.net:41417", "t"},
		{"http://[fd00::5]:41417/#pair=t", "http://[fd00::5]:41417", "t"},
		{"#pair=t&gateway=http://b:2/", "http://b:2", "t"},
	} {
		got, err := ParsePairingLink(tc.in)
		if err != nil || got.Gateway != tc.gateway || got.Token != tc.token {
			t.Errorf("%q: %+v, %v; want %s and %s", tc.in, got, err, tc.gateway, tc.token)
		}
	}
}

func TestParsePairingLinkNeedsTheGatewayAddress(t *testing.T) {
	for _, in := range []string{"#pair=t", "t0k_en", "http://a:1/#pair=t&gateway=ftp://b:2", "http://a:1/#pair=t&gateway=nonsense"} {
		if _, err := ParsePairingLink(in); !errors.Is(err, ErrNoGatewayAddress) {
			t.Errorf("%q: %v, want ErrNoGatewayAddress", in, err)
		}
	}
}

func TestParsePairingLinkRefusesWhatIsNotALink(t *testing.T) {
	for _, in := range []string{"", "   ", "http://a:1/#/settings", "hello world", "http://a:1/"} {
		if _, err := ParsePairingLink(in); !errors.Is(err, ErrNotPairingLink) {
			t.Errorf("%q: %v, want ErrNotPairingLink", in, err)
		}
	}
}
