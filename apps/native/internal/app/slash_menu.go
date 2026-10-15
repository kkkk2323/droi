package app

import (
	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/internal/slash"
)

// slashMenu is a composer's "/" suggestions: the builtins that run in that
// composer and the Daemon's commands and skills, narrowed to the "/word"
// typed, moved through with the arrows, picked with Tab or Enter and closed
// with Escape. The Session's composer and the New session page's both
// offer theirs through it, so that one cannot lose what the other has.
type slashMenu struct {
	highlight int
	// dismissed is the text Escape closed the suggestions for.
	dismissed string
	// shown are what text offers, after items; active the highlighted one.
	shown  []slash.Item
	active int
}

// items is what a composer offers, builtins first (slash.Builtins in a
// Session, slash.Starters on the New session page) and then fromDaemon;
// it also narrows them to what text offers, for key and view.
func (m *slashMenu) items(text string, builtins, fromDaemon []slash.Item) []slash.Item {
	items := slash.Merge(builtins, fromDaemon)
	m.shown = nil
	if q, ok := slash.Query(text, len(text)); ok && m.dismissed != text {
		m.shown = slash.Filter(items, q, slash.DefaultLimit)
	}
	m.active = min(m.highlight, max(len(m.shown)-1, 0))
	return items
}

// key takes a key of the text area holding text while suggestions show,
// and reports whether it did.
func (m *slashMenu) key(key ui.TextKey, text string, pick func(slash.Item)) bool {
	n := len(m.shown)
	if n == 0 {
		return false
	}
	switch {
	case key.Key == ui.KeyDown:
		m.highlight = (m.active + 1) % n
	case key.Key == ui.KeyUp:
		m.highlight = (m.active - 1 + n) % n
	case key.Key == ui.KeyTab || key.Key == ui.KeyEnter && key.Mods&ui.Shift == 0:
		m.highlight = 0
		pick(m.shown[m.active])
	case key.Key == ui.KeyEscape:
		m.dismissed = text
	default:
		return false
	}
	return true
}

// view shows the suggestions over the composer's card.
func (m *slashMenu) view(c *ui.Context, a *App, card ui.Element, pick func(slash.Item)) {
	if len(m.shown) == 0 {
		return
	}
	a.suggestionList(c, card, m.shown, m.active, &m.highlight, func(item slash.Item) {
		m.highlight = 0
		pick(item)
	})
}
