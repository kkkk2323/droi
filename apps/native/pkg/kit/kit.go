// Package kit holds the Client's controls as MyGo elements: the shadcn
// Button variants, the Base UI menus, selects and dialogs, the spinner and
// the setting rows, each sized and colored from the web Client's Tailwind
// classes and the design tokens of package theme.
package kit

import (
	"math"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/pkg/icons"
	"github.com/kkkk2323/droi/apps/native/pkg/theme"
)

// Kit is what the controls draw with: the tokens of the theme shown and
// the font families chosen.
type Kit struct {
	T    *theme.Tokens
	Sans string
	Mono string
	// Rem is the Client's rem in DIPs: 16 at the default text size.
	Rem float32
	// Compact is Settings → Density's Compact: smaller text and controls.
	Compact bool
}

// New makes the kit of a theme, font choice and text size.
func New(name theme.Name, font theme.FontChoice, size theme.TextSize, zoom float64) *Kit {
	sans, mono := theme.Families(font)
	return &Kit{T: theme.Of(name), Sans: sans, Mono: mono, Rem: 16 * size.Scale() * float32(zoom)}
}

// Px scales a CSS length of the default text size, in px, to this kit's
// text size: every Tailwind size is in rem.
func (k *Kit) Px(px float32) float32 { return px * k.Rem / 16 }

// Dense is a length for the density shown, in px of the default text size:
// compact when the kit is Compact, comfortable otherwise.
func (k *Kit) Dense(compact, comfortable float32) float32 {
	if k.Compact {
		return compact
	}
	return comfortable
}

// UITheme is the MyGo theme the window starts from: the Client's background,
// text, borders and fonts, so MyGo's own widgets fit in.
func (k *Kit) UITheme(base *ui.Theme) *ui.Theme {
	t := *base
	t.Dark = k.T.Dark
	t.Background = k.T.Sidebar
	t.Text = k.T.Foreground
	t.TextMuted = k.T.MutedForeground
	t.Border = k.T.Border
	t.Surface = k.T.Background
	t.SurfaceHover = k.T.Muted
	t.SurfacePressed = k.T.Accent
	t.Accent = k.T.Primary
	t.AccentHover = k.T.Primary.Alpha(0.8)
	t.AccentPressed = k.T.Primary
	t.AccentText = k.T.PrimaryForeground
	t.Danger = k.T.Destructive
	t.Success = k.T.Success
	t.Warning = k.T.Attention
	t.Selection = k.T.Selection
	t.Focus = k.T.Ring.Alpha(0.5)
	t.Inverse = k.T.Foreground
	t.InverseText = k.T.Background
	t.Scrollbar = k.T.Foreground.Alpha(0.2)
	t.ScrollbarWidth = 6
	t.Radius = k.Px(8)
	t.FontSize = k.Px(k.Dense(13, 14))
	t.Font = k.Sans
	return &t
}

// Icon is a Lucide icon of size DIPs in a color.
func (k *Kit) Icon(c *ui.Context, name string, size float32, color ui.Color) ui.Element {
	return k.IconStroke(c, name, 2, size, color)
}

// IconStroke is Icon with another stroke width (lucide's strokeWidth).
func (k *Kit) IconStroke(c *ui.Context, name string, stroke, size float32, color ui.Color) ui.Element {
	return ui.Icon(c, icons.Stroked(name, stroke)).Size(k.Px(size), k.Px(size)).Shrink(0).TextColor(color)
}

// Text is text at a CSS size and line height (both px), Tailwind's
// text-[13px] leading-[19.5px]; the line height is that of the font size
// times 1.5 when lh is 0, as Tailwind's text-sm and friends leave it.
func (k *Kit) Text(c *ui.Context, s string, size, lh float32) ui.Element {
	if lh == 0 {
		lh = size * 1.5
	}
	return ui.Text(c, s).FontSize(k.Px(size)).FixedLineHeight(k.Px(lh))
}

// Variant is a shadcn Button variant.
type Variant uint8

const (
	Primary Variant = iota
	Outline
	Secondary
	Ghost
	Destructive
)

// Button is a shadcn Button as the reference draws one: rounded-lg (8px),
// text-ui (13px, medium), h-8 by default, its children laid out in a row.
// size is its height (and width when square); Tailwind's h-9 is 36.
func (k *Kit) Button(c *ui.Context, v Variant, size float32, square bool, label string) ui.Element {
	t := k.T
	b := ui.ButtonBase(c).Label(label).Height(k.Px(size)).Radius(k.Px(8)).Gap(k.Px(6)).
		FontSize(k.Px(k.Dense(13, 14))).FontWeight(500).Cursor(ui.CursorPointer)
	if square {
		b.Width(k.Px(size))
	} else {
		b.PaddingX(k.Px(11))
	}
	hover := b.Hovered()
	switch v {
	case Primary:
		bg := t.Primary
		if hover {
			bg = bg.Alpha(0.9)
		}
		b.Background(bg).TextColor(t.PrimaryForeground)
	case Outline:
		// Transparent over the surface it sits on, the hairline and the ink
		// doing the work: a filled default state would read heavier than the
		// rest of the window.
		bg := ui.Color{}
		if hover {
			bg = t.Muted
		}
		b.Background(bg).Border(1, t.Border).TextColor(t.Foreground)
	case Secondary:
		bg := t.Secondary
		if hover {
			bg = bg.Alpha(0.8)
		}
		b.Background(bg).TextColor(t.SecondaryForeground)
	case Ghost:
		if hover {
			bg := t.Muted
			if t.Dark {
				bg = bg.Alpha(0.5)
			}
			b.Background(bg).TextColor(t.Foreground)
		}
	case Destructive:
		a := float32(0.1)
		if t.Dark {
			a = 0.2
		}
		if hover {
			a += 0.1
		}
		b.Background(t.Destructive.Alpha(a)).TextColor(t.Destructive)
	}
	return b
}

// IconButton is a ghost icon button, shadcn's size="icon-sm" (32) or
// "icon-xs" (24, rounded 8, icon 12), muted until hovered.
func (k *Kit) IconButton(c *ui.Context, icon, label string, size float32) ui.Element {
	b := k.Button(c, Ghost, size, true, label)
	color := k.T.MutedForeground
	if b.Hovered() {
		color = k.T.Foreground
	}
	iconSize := float32(16)
	if size <= 24 {
		iconSize = 12
	}
	b.Children(func() { k.Icon(c, icon, iconSize, color) })
	return b
}

// pulseRate is how many frames a second a pulse asks for: a 2 s wave looks
// as smooth at 30 as at the display's 120, and on macOS the display then
// refreshes less often too.
const pulseRate = 30

// Spinner is loading.dev's Arc, one open stroke turning, for work under way.
func (k *Kit) Spinner(c *ui.Context, size float32, color ui.Color) ui.Element {
	return Arc(c, k.Px(size), color)
}

// arcCycle is the time of one turn of Arc, as loading.dev's.
const arcCycle = 800 * time.Millisecond

type arcKey struct{}

// Arc is loading.dev's Arc, size DIPs high. Its angle comes from the clock,
// not from when it appeared as loading.Arc's does, so that the spinners on
// screen turn together.
func Arc(c *ui.Context, size float32, color ui.Color) ui.Element {
	still := c.Preferences().ReduceMotion
	e := ui.Box(c).Size(size, size).Shrink(0).Role(ui.RoleProgress).Range(0, 1, -1)
	path := ui.Local(e, arcKey{}, func() ui.Path { return ui.Path{} })
	return e.Draw(func(p *ui.Painter, r ui.Rect) {
		s := min(r.W, r.H)
		cx, cy := r.X+r.W/2, r.Y+r.H/2
		angle := float32(0)
		if !still {
			at := p.Now().UnixNano() % int64(arcCycle)
			angle = 360 * float32(at) / float32(arcCycle)
			p.AnimationFrame()
		}
		// Drawn at rest, twice as large, and turned and halved by the
		// renderer: the same path every frame keeps its mask, as loading's.
		path.Reset().Circle(cx, cy, 2*s*10/24).Trim(0, 1.8/(2*math.Pi))
		p.Transform(ui.Scaling(0.5, 0.5).Then(ui.Rotation(angle)), cx, cy, func() {
			p.StrokePath(path, 2*s*2.5/24, color)
		})
	})
}

// Pulse paints e's box, rounded by radius, in the color color gives for
// the wave of Tailwind's animate-pulse: 1 at rest, 0 halfway through each
// 2 s.
func Pulse(e ui.Element, radius float32, color func(wave float32) ui.Color) ui.Element {
	return e.Draw(func(p *ui.Painter, r ui.Rect) {
		p.AnimationFrameRate(pulseRate)
		x := float32(p.Now().UnixMilli()%2000) / 1000
		if x > 1 {
			x = 2 - x
		}
		p.Fill(r, color(1-x), radius)
	})
}

// PulseDot is a Dot whose color pulses as Pulse's does.
func PulseDot(c *ui.Context, size float32, color func(wave float32) ui.Color) ui.Element {
	return Pulse(ui.Box(c).Size(size, size).Shrink(0), size/2, color)
}

// Selected marks e selected for assistive technology without MyGo's look
// for it (the accent background and text), which the caller paints itself
// afterwards.
func Selected(e ui.Element, on bool) ui.Element {
	return e.Selected(on).Background(ui.Color{}).TextColor(ui.Color{})
}

// Dot is a filled circle, as a status or unread mark.
func Dot(c *ui.Context, size float32, color ui.Color) ui.Element {
	return ui.Box(c).Size(size, size).Radius(size / 2).Background(color).Shrink(0)
}
