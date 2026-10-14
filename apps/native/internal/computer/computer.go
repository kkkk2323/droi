// Package computer is the Client preference that says which Paired Computer
// the window follows: nothing for this computer, or one other computer
// added with its pairing link (ADR 0019).
package computer

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/kkkk2323/droi/apps/native/internal/gateway"
	"github.com/kkkk2323/droi/apps/native/internal/prefs"
)

const prefsKey = "droi.computer"

// Paired is a Paired Computer: the Gateway's origin, the Pairing Token, and
// the name and id the Gateway's /meta reported when it was added.
type Paired struct {
	Gateway string `json:"gateway"`
	Token   string `json:"token"`
	Name    string `json:"name"`
	ID      string `json:"id"`
}

// SocketURL is the Daemon socket the window connects to.
func (p Paired) SocketURL() string {
	u, err := gateway.SocketURL(p.Gateway, p.Token)
	if err != nil {
		return ""
	}
	return u
}

// Load is the computer the window follows, nil for this computer: when
// nothing is saved, and when what is saved cannot be used.
func Load(s *prefs.Store) *Paired {
	raw, ok := s.Get(prefsKey)
	if !ok {
		return nil
	}
	var p Paired
	if json.Unmarshal([]byte(raw), &p) != nil || p.Gateway == "" || p.Token == "" {
		return nil
	}
	return &p
}

// Save makes p the computer the window follows from the next launch.
func Save(s *prefs.Store, p Paired) {
	b, _ := json.Marshal(p)
	s.Set(prefsKey, string(b))
}

// Clear goes back to this computer from the next launch.
func Clear(s *prefs.Store) { s.Remove(prefsKey) }

var (
	// ErrNoComputerID is a Gateway whose /meta names no computer, so the
	// Preferences kept for it could not be told from this computer's.
	ErrNoComputerID = errors.New("the Gateway does not say which computer it belongs to")
	// ErrThisComputer is a link to this computer's own Gateway.
	ErrThisComputer = errors.New("the link points at this computer")
)

// Verify reads a pasted pairing link and checks that it reaches a Droi
// Gateway that accepts its Pairing Token, as the Phone App's pairing does,
// and that it belongs to another computer than thisComputerID. The errors
// are the gateway package's, this package's, or the network's when nothing
// answers.
func Verify(ctx context.Context, link, thisComputerID string) (Paired, error) {
	l, err := gateway.ParsePairingLink(link)
	if err != nil {
		return Paired{}, err
	}
	meta, err := gateway.FetchMeta(ctx, l.Gateway)
	if err != nil {
		return Paired{}, err
	}
	if meta.ComputerID == "" {
		return Paired{}, ErrNoComputerID
	}
	if meta.ComputerID == thisComputerID {
		return Paired{}, ErrThisComputer
	}
	if err := gateway.CheckToken(ctx, l.Gateway, l.Token); err != nil {
		return Paired{}, err
	}
	return Paired{Gateway: l.Gateway, Token: l.Token, Name: meta.Name, ID: meta.ComputerID}, nil
}
