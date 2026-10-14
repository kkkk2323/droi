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
// Gateway's origin and the Pairing Token.
type PairingLink struct {
	Gateway string
	Token   string
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
	origin, ok := originOf(address)
	if !ok {
		return PairingLink{}, ErrNoGatewayAddress
	}
	return PairingLink{Gateway: origin, Token: token}, nil
}

func originOf(address string) (string, bool) {
	u, err := url.Parse(address)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return "", false
	}
	return u.Scheme + "://" + u.Host, true
}
