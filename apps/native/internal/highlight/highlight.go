// Package highlight colors code as Shiki does in the web Client, through
// nuri (a Go port of Shiki) with its contrast correction off, so tokens and
// colors match. Tokenizing is slow (about 220 ms for the first block), so it
// runs in the background and a block shows plain until its colors are ready.
package highlight

import (
	"context"
	_ "embed"
	"hash/maphash"
	"strings"
	"sync"

	"github.com/egoist/mygo/ui"
	"github.com/frostybee/nuri"
	"github.com/frostybee/nuri/bundle/full"
	"github.com/frostybee/nuri/theme"
)

//go:embed themes/solarized-light-plus.json
var solarizedLightPlus []byte

// Theme is the Shiki theme each of Droi's themes paints code in.
func Theme(droiTheme string) string {
	switch droiTheme {
	case "dark":
		return "github-dark"
	case "solarized-light":
		return "solarized-light-plus"
	}
	return "github-light"
}

type key uint64

var (
	seed = maphash.MakeSeed()

	once    sync.Once
	h       *nuri.Highlighter
	initErr error

	mu      sync.Mutex
	cache   = map[key][]ui.Span{}
	pending = map[key]bool{}
	// Ready is called, on a background goroutine, when a block's colors
	// come in; the window redraws.
	Ready func()
)

const cacheMax = 4000

func highlighter() (*nuri.Highlighter, error) {
	once.Do(func() {
		h, initErr = nuri.New(context.Background(),
			nuri.WithFS(full.FS()),
			nuri.WithTheme("solarized-light-plus", solarizedLightPlus),
			nuri.WithMinContrast(0),
			nuri.WithPoolSize(2),
		)
	})
	return h, initErr
}

func keyOf(lang, code, themeName string) key {
	var m maphash.Hash
	m.SetSeed(seed)
	m.WriteString(lang)
	m.WriteByte(0)
	m.WriteString(themeName)
	m.WriteByte(0)
	m.WriteString(code)
	return key(m.Sum64())
}

// Spans is code colored for its language in a Shiki theme, or nil while the
// colors are still being worked out (Ready is called once they are).
func Spans(lang, code, themeName string) []ui.Span {
	k := keyOf(lang, code, themeName)
	mu.Lock()
	if s, ok := cache[k]; ok {
		mu.Unlock()
		return s
	}
	if pending[k] {
		mu.Unlock()
		return nil
	}
	pending[k] = true
	mu.Unlock()
	go func() {
		s := Sync(lang, code, themeName)
		mu.Lock()
		if len(cache) >= cacheMax {
			clear(cache)
		}
		cache[k] = s
		delete(pending, k)
		mu.Unlock()
		if Ready != nil {
			Ready()
		}
	}()
	return nil
}

// Sync colors code on the calling goroutine.
func Sync(lang, code, themeName string) []ui.Span {
	hl, err := highlighter()
	if err != nil || lang == "" {
		return []ui.Span{{Text: code}}
	}
	res, err := hl.CodeToTokens(context.Background(), code, nuri.CodeToTokensOptions{Lang: strings.ToLower(lang), Theme: themeName})
	if err != nil {
		return []ui.Span{{Text: code}}
	}
	fg := res.FG
	var out []ui.Span
	for i, line := range res.Tokens {
		if i > 0 {
			out = appendSpan(out, ui.Span{Text: "\n"})
		}
		for _, tok := range line {
			s := ui.Span{Text: tok.Content}
			if tok.Color != "" && !strings.EqualFold(tok.Color, fg) {
				s.Color = ui.Hex(tok.Color)
			}
			if tok.FontStyle > 0 {
				s.Italic = tok.FontStyle.Has(theme.FontStyleItalic)
				if tok.FontStyle.Has(theme.FontStyleBold) {
					s.Weight = 700
				}
				s.Underline = tok.FontStyle.Has(theme.FontStyleUnderline)
			}
			out = appendSpan(out, s)
		}
	}
	return out
}

// Foreground is a Shiki theme's text color, which unstyled tokens take.
func Foreground(themeName string) ui.Color {
	hl, err := highlighter()
	if err != nil {
		return ui.Color{}
	}
	res, err := hl.CodeToTokens(context.Background(), " ", nuri.CodeToTokensOptions{Lang: "text", Theme: themeName})
	if err != nil {
		return ui.Color{}
	}
	return ui.Hex(res.FG)
}

func appendSpan(out []ui.Span, s ui.Span) []ui.Span {
	if n := len(out) - 1; n >= 0 {
		last := out[n]
		if last.Color == s.Color && last.Weight == s.Weight && last.Italic == s.Italic && last.Underline == s.Underline || s.Text == "\n" {
			out[n].Text += s.Text
			return out
		}
	}
	return append(out, s)
}
