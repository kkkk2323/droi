package gateway

import (
	"context"
	"crypto/rand"
	"encoding/hex"

	droid "github.com/kkkk2323/droi/packages/droid-sdk-go"
)

// NewLocalToken is a token for Options.LocalToken: 32 random bytes, which the
// app makes at each launch and tells no one but its own window.
func NewLocalToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// DaemonSocketURL is the Daemon socket as a Client on this computer reaches
// it, over loopback, presenting token.
func (g *Gateway) DaemonSocketURL(token string) string {
	u, _ := SocketURL(g.URL(), token) // g.URL() is always an http origin
	return u
}

// PlaceholderCredential is what a Client gives the SDK in place of a Factory
// credential: the Gateway swaps in the real one.
func PlaceholderCredential(context.Context) (*droid.Credential, error) {
	return &droid.Credential{APIKey: APIKeyPlaceholder}, nil
}
