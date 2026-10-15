// Package computer is the Client preference that keeps the Paired Computers
// the window has followed, each added with its pairing link, and which one
// it follows: none for this computer (ADR 0019).
package computer

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	"github.com/kkkk2323/droi/apps/native/internal/gateway"
	"github.com/kkkk2323/droi/apps/native/pkg/prefs"
)

const prefsKey = "droi.computers"

// oldKey is where versions that kept one computer kept it, and only while
// the window followed it.
const oldKey = "droi.computer"

// record is what prefsKey holds.
type record struct {
	// Followed is the id of the computer the window follows, "" for this one.
	Followed  string   `json:"followed"`
	Computers []Paired `json:"computers"`
}

func usable(p Paired) bool { return p.Gateway != "" && p.Token != "" && p.ID != "" }

// read is the record, the one computer of oldKey moved into it the first
// time.
func read(s *prefs.Store) record {
	var r record
	if raw, ok := s.Get(prefsKey); ok {
		_ = json.Unmarshal([]byte(raw), &r)
	} else if raw, ok := s.Get(oldKey); ok {
		var p Paired
		if json.Unmarshal([]byte(raw), &p) == nil && usable(p) {
			r = record{Followed: p.ID, Computers: []Paired{p}}
			write(s, r)
		}
		s.Remove(oldKey)
	}
	r.Computers = slices.DeleteFunc(r.Computers, func(p Paired) bool { return !usable(p) })
	if !slices.ContainsFunc(r.Computers, func(p Paired) bool { return p.ID == r.Followed }) {
		r.Followed = ""
	}
	return r
}

func write(s *prefs.Store, r record) {
	if r.Computers == nil {
		r.Computers = []Paired{}
	}
	b, _ := json.Marshal(r)
	s.Set(prefsKey, string(b))
}

// Load is the computer the window follows, nil for this computer: when
// none is followed, and when what is kept cannot be used.
func Load(s *prefs.Store) *Paired {
	r := read(s)
	for _, p := range r.Computers {
		if p.ID == r.Followed {
			return &p
		}
	}
	return nil
}

// Remembered are the Paired Computers the window has followed, in the order
// they were first added.
func Remembered(s *prefs.Store) []Paired { return read(s).Computers }

// Follow remembers p, in place of what was kept for the same computer, and
// makes it the one the window follows.
func Follow(s *prefs.Store, p Paired) {
	r := read(s)
	if i := slices.IndexFunc(r.Computers, func(q Paired) bool { return q.ID == p.ID }); i >= 0 {
		r.Computers[i] = p
	} else {
		r.Computers = append(r.Computers, p)
	}
	r.Followed = p.ID
	write(s, r)
}

// UseThisComputer makes the window follow this computer; the others stay
// remembered.
func UseThisComputer(s *prefs.Store) {
	r := read(s)
	r.Followed = ""
	write(s, r)
}

// Forget drops the computer of that id; the window follows this computer if
// it followed that one.
func Forget(s *prefs.Store, id string) {
	r := read(s)
	r.Computers = slices.DeleteFunc(r.Computers, func(p Paired) bool { return p.ID == id })
	if r.Followed == id {
		r.Followed = ""
	}
	write(s, r)
}

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
