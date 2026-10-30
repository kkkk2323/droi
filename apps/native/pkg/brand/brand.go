// Package brand is what names the app: its name, the short id its folders
// take, the Gateway's port and its mark. Droi is the default; an app built
// on Droi's packages sets its own once, before anything is drawn or started.
package brand

import (
	"path/filepath"
	"sync/atomic"

	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
)

// Brand names the app.
type Brand struct {
	// Name is what the window, the menus and every text call the app.
	Name string
	// Short, optional, is the name the sidebar's top row shows beside the
	// mark, where a long Name reads as clutter; Name when empty.
	Short string
	// ID is the short, lowercase name of its folders: ~/.<ID>/chats for
	// Scratch Workspaces and, for `<app> host` on Linux, $XDG_CONFIG_HOME/<ID>.
	ID string
	// Port is the Gateway's preferred port. Two apps on one computer need
	// two ports: each refuses to start a Host while another answers on its.
	Port int
	// Mark is the app's mark as SVG, drawn on the New session page in the
	// text color where it has none of its own; nil draws Droi's.
	Mark []byte
}

// Droi is Droi's own brand.
var Droi = Brand{Name: "Droi", ID: "droi", Port: 41417}

var current atomic.Pointer[Brand]

func init() {
	b := Droi
	current.Store(&b)
}

// Set makes b the app's brand; a field it leaves empty is Droi's. It also
// names the app in l10n's texts.
func Set(b Brand) {
	if b.Name == "" {
		b.Name = Droi.Name
	}
	if b.ID == "" {
		b.ID = Droi.ID
	}
	if b.Port == 0 {
		b.Port = Droi.Port
	}
	current.Store(&b)
	l10n.SetAppName(b.Name)
}

// Current is the app's brand.
func Current() Brand { return *current.Load() }

// Name is the app's name.
func Name() string { return current.Load().Name }

// ShortName is the name the sidebar's top row shows: Short, or Name.
func ShortName() string {
	b := current.Load()
	if b.Short != "" {
		return b.Short
	}
	return b.Name
}

// ScratchFolder is where Scratch Workspaces go under home unless Settings
// names a folder: ~/.<ID>/chats.
func ScratchFolder(home string) string {
	return filepath.Join(home, "."+current.Load().ID, "chats")
}
