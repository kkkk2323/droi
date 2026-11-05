package gateway

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
)

var (
	// ErrNotPairingLink is text that carries no Pairing Token.
	ErrNotPairingLink = errors.New("this is not a pairing link")
	// ErrNoGatewayAddress is a token without the Gateway's address, which a
	// Client that is not served by the Gateway cannot guess.
	ErrNoGatewayAddress = errors.New("the pairing link has no Gateway address")
)

// PairingLink is what a Client needs to reach a Paired Computer: the
// Gateway's address, the Pairing Token, and the Relay address when the link
// has one (ADR 0034). An address is an origin, or a Relay's with its path.
type PairingLink struct {
	Gateway string
	Token   string
	Relay   string
}

var bareToken = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// ParsePairingLink reads a pairing link pasted by hand as the Phone App does
// (packages/daemon-layer/src/pairing.ts): the whole link, whose fragment
// carries the token and may name the Gateway, else the link's own origin.
// A bare token or a fragment alone has no address and is
// ErrNoGatewayAddress.
func ParsePairingLink(text string) (PairingLink, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return PairingLink{}, ErrNotPairingLink
	}
	_, fragment, hasFragment := strings.Cut(text, "#")
	if !hasFragment {
		if bareToken.MatchString(text) {
			return PairingLink{}, ErrNoGatewayAddress
		}
		return PairingLink{}, ErrNotPairingLink
	}
	values, err := url.ParseQuery(fragment)
	token := values.Get("pair")
	if err != nil || token == "" {
		return PairingLink{}, ErrNotPairingLink
	}
	address := values.Get("gateway")
	if address == "" {
		address = text[:strings.Index(text, "#")]
	}
	base, ok := BaseOf(address)
	if !ok {
		return PairingLink{}, ErrNoGatewayAddress
	}
	link := PairingLink{Gateway: base, Token: token}
	if relay, ok := BaseOf(values.Get(RelayFragment)); ok && relay != base {
		link.Relay = relay
	}
	return link, nil
}

// BaseOf is a Gateway address as Clients keep it: the scheme, the host and
// the path without its trailing slash, which a Relay address has
// (https://relay.example.com/c/<Relay Id>); no query or fragment.
func BaseOf(address string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(address))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return "", false
	}
	return u.Scheme + "://" + u.Host + strings.TrimRight(u.EscapedPath(), "/"), true
}
