package sessionview

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/egoist/mygo/ui/markdown/parser"
)

// mathExtensions read $…$ as math too, as language models write it: an
// arrow between two steps comes as $\rightarrow$. The parser leaves amounts
// of money ($5, costs $3 and $4) as text.
const mathExtensions = parser.Default | parser.ExtDollarMath

// inlineMath draws math within text that texText can write as Unicode in
// the text's own font and color; other math shows as its TeX.
type inlineMath struct {
	font  string
	color ui.Color
	made  map[mathKey]markdown.InlineObject
}

type mathKey struct {
	tex  string
	size float32
}

// object is Options.InlineMath. It is asked again each frame a paragraph
// streams in, so what it makes is kept.
func (m *inlineMath) object(tex string, size float32) (markdown.InlineObject, bool) {
	key := mathKey{tex, size}
	if o, ok := m.made[key]; ok {
		return o, o.Draw != nil
	}
	if m.made == nil || len(m.made) >= 512 {
		m.made = map[mathKey]markdown.InlineObject{}
	}
	s, ok := texText(tex)
	if !ok {
		m.made[key] = markdown.InlineObject{}
		return markdown.InlineObject{}, false
	}
	l := ui.ShapeText(s, ui.Font{Family: m.font, Size: size}, 0)
	lines := l.Lines()
	w, _ := l.Size()
	if len(lines) == 0 || w <= 0 {
		m.made[key] = markdown.InlineObject{}
		return markdown.InlineObject{}, false
	}
	line, color := lines[0], m.color
	o := markdown.InlineObject{
		Width:  w,
		Height: line.Ascent,
		Depth:  line.Descent,
		Draw:   func(p *ui.Painter, r ui.Rect) { p.TextLayout(l, r.X, r.Y+line.Ascent-line.Baseline, color) },
	}
	m.made[key] = o
	return o, true
}

// texText writes TeX as plain Unicode text, as $A \rightarrow B$ as "A → B"
// and $O(n^2)$ as "O(n²)", and reports false for TeX it cannot write
// faithfully so, as a fraction of fractions or a superscript with no
// Unicode form.
func texText(tex string) (string, bool) {
	p := texParser{s: tex}
	out, ok := p.seq(false)
	if !ok || p.i < len(p.s) {
		return "", false
	}
	out = strings.Join(strings.Fields(out), " ")
	return out, out != ""
}

type texParser struct {
	s string
	i int
}

// seq reads TeX up to the end, or up to the } closing a group when
// inGroup, which it leaves to the caller.
func (p *texParser) seq(inGroup bool) (string, bool) {
	var b strings.Builder
	for p.i < len(p.s) {
		c := p.s[p.i]
		switch {
		case c == '}':
			if !inGroup {
				return "", false
			}
			return b.String(), true
		case c == '{':
			g, ok := p.group()
			if !ok {
				return "", false
			}
			b.WriteString(g)
		case c == '\\':
			s, ok := p.command()
			if !ok {
				return "", false
			}
			b.WriteString(s)
		case c == '^' || c == '_':
			p.i++
			arg, ok := p.arg()
			if !ok {
				return "", false
			}
			s, ok := script(arg, c == '^')
			if !ok {
				return "", false
			}
			b.WriteString(s)
		case c == '&' || c == '#' || c == '%':
			return "", false
		case c == '~':
			p.i++
			b.WriteString("\u00a0")
		default:
			r, n := runeAt(p.s, p.i)
			p.i += n
			switch r {
			case '-':
				r = '−'
			case '\'':
				r = '′'
			}
			b.WriteRune(r)
		}
	}
	if inGroup {
		return "", false
	}
	return b.String(), true
}

// group reads {…}.
func (p *texParser) group() (string, bool) {
	p.i++
	s, ok := p.seq(true)
	if !ok || p.i >= len(p.s) || p.s[p.i] != '}' {
		return "", false
	}
	p.i++
	return s, true
}

// arg reads a command's argument: a group, a command or one character.
func (p *texParser) arg() (string, bool) {
	for p.i < len(p.s) && p.s[p.i] == ' ' {
		p.i++
	}
	if p.i >= len(p.s) {
		return "", false
	}
	switch p.s[p.i] {
	case '{':
		return p.group()
	case '\\':
		return p.command()
	case '}', '^', '_':
		return "", false
	}
	r, n := runeAt(p.s, p.i)
	p.i += n
	if r == '-' {
		r = '−'
	}
	return string(r), true
}

// command reads a command after its backslash.
func (p *texParser) command() (string, bool) {
	p.i++
	start := p.i
	for p.i < len(p.s) && isASCIILetter(p.s[p.i]) {
		p.i++
	}
	if p.i == start {
		if p.i >= len(p.s) {
			return "", false
		}
		c := p.s[p.i]
		p.i++
		switch c {
		case ',', '>':
			return "\u2009", true
		case ':', ';', ' ':
			return " ", true
		case '!':
			return "", true
		case '{', '}', '$', '%', '&', '#', '_', '|':
			if c == '|' {
				return "‖", true
			}
			return string(c), true
		}
		return "", false
	}
	name := p.s[start:p.i]
	if s, ok := texSymbols[name]; ok {
		return s, true
	}
	switch name {
	case "left", "right", "big", "Big", "bigg", "Bigg", "bigl", "bigr", "Bigl", "Bigr",
		"displaystyle", "textstyle", "limits", "nolimits":
		return "", true
	case "quad":
		return "\u2003", true
	case "qquad":
		return "\u2003\u2003", true
	case "log", "ln", "lg", "exp", "sin", "cos", "tan", "cot", "sec", "csc", "arcsin", "arccos", "arctan",
		"sinh", "cosh", "tanh", "max", "min", "sup", "inf", "lim", "det", "gcd", "deg", "dim", "ker", "arg", "Pr":
		return name, true
	case "mod", "bmod":
		return " mod ", true
	case "text", "textrm", "textnormal", "textbf", "textit", "textsf", "texttt", "mbox",
		"mathrm", "mathbf", "mathit", "mathsf", "mathtt", "operatorname", "boldsymbol":
		return p.arg()
	case "frac", "dfrac", "tfrac":
		a, ok := p.arg()
		if !ok {
			return "", false
		}
		b, ok := p.arg()
		if !ok {
			return "", false
		}
		return operand(a) + "/" + operand(b), true
	case "sqrt":
		if p.i < len(p.s) && p.s[p.i] == '[' {
			return "", false
		}
		a, ok := p.arg()
		if !ok {
			return "", false
		}
		return "√" + operand(a), true
	case "not":
		a, ok := p.arg()
		if !ok {
			return "", false
		}
		switch a {
		case "=":
			return "≠", true
		case "∈":
			return "∉", true
		case "⊂":
			return "⊄", true
		case "⊆":
			return "⊈", true
		}
		return a + "\u0338", true
	}
	return "", false
}

// operand wraps a fraction's or a root's part in parentheses unless it is
// a single term.
func operand(s string) string {
	s = strings.TrimSpace(s)
	if len([]rune(s)) <= 1 || !strings.ContainsAny(s, " +−=×⋅/±") {
		return s
	}
	return "(" + s + ")"
}

func script(s string, sup bool) (string, bool) {
	table := subscripts
	if sup {
		table = superscripts
	}
	var b strings.Builder
	for _, r := range s {
		if unicode.IsSpace(r) {
			continue
		}
		m, ok := table[r]
		if !ok {
			return "", false
		}
		b.WriteRune(m)
	}
	return b.String(), b.Len() > 0
}

func runeAt(s string, i int) (rune, int) { return utf8.DecodeRuneInString(s[i:]) }

func isASCIILetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

var superscripts = runeMap(
	"0123456789+−=()niabcdefghjklmoprstuvwxyzABDEGHIJKLMNOPRTUVW∘′",
	"⁰¹²³⁴⁵⁶⁷⁸⁹⁺⁻⁼⁽⁾ⁿⁱᵃᵇᶜᵈᵉᶠᵍʰʲᵏˡᵐᵒᵖʳˢᵗᵘᵛʷˣʸᶻᴬᴮᴰᴱᴳᴴᴵᴶᴷᴸᴹᴺᴼᴾᴿᵀᵁⱽᵂ°′",
)

var subscripts = runeMap(
	"0123456789+−=()aehijklmnoprstuvx",
	"₀₁₂₃₄₅₆₇₈₉₊₋₌₍₎ₐₑₕᵢⱼₖₗₘₙₒₚᵣₛₜᵤᵥₓ",
)

func runeMap(from, to string) map[rune]rune {
	f, t := []rune(from), []rune(to)
	m := make(map[rune]rune, len(f))
	for i, r := range f {
		m[r] = t[i]
	}
	return m
}

// texSymbols are the commands that stand for one symbol.
var texSymbols = map[string]string{
	// Arrows.
	"rightarrow": "→", "to": "→", "leftarrow": "←", "gets": "←", "leftrightarrow": "↔",
	"Rightarrow": "⇒", "Leftarrow": "⇐", "Leftrightarrow": "⇔", "implies": "⟹", "impliedby": "⟸", "iff": "⟺",
	"longrightarrow": "⟶", "longleftarrow": "⟵", "longleftrightarrow": "⟷",
	"Longrightarrow": "⟹", "Longleftarrow": "⟸", "Longleftrightarrow": "⟺",
	"mapsto": "↦", "longmapsto": "⟼", "uparrow": "↑", "downarrow": "↓", "updownarrow": "↕",
	"Uparrow": "⇑", "Downarrow": "⇓", "nearrow": "↗", "searrow": "↘", "swarrow": "↙", "nwarrow": "↖",
	"hookrightarrow": "↪", "hookleftarrow": "↩", "rightleftharpoons": "⇌", "leftrightarrows": "⇆",
	"rightrightarrows": "⇉", "circlearrowright": "↻", "circlearrowleft": "↺",
	// Relations.
	"le": "≤", "leq": "≤", "ge": "≥", "geq": "≥", "ne": "≠", "neq": "≠", "approx": "≈", "sim": "∼",
	"simeq": "≃", "equiv": "≡", "cong": "≅", "propto": "∝", "ll": "≪", "gg": "≫", "leqslant": "⩽", "geqslant": "⩾",
	"in": "∈", "notin": "∉", "ni": "∋", "subset": "⊂", "subseteq": "⊆", "supset": "⊃", "supseteq": "⊇",
	"mid": "∣", "parallel": "∥", "perp": "⊥", "prec": "≺", "succ": "≻", "coloneqq": "≔", "doteq": "≐",
	"vdash": "⊢", "models": "⊨",
	// Operators.
	"times": "×", "div": "÷", "pm": "±", "mp": "∓", "cdot": "⋅", "circ": "∘", "bullet": "•", "ast": "∗",
	"star": "⋆", "oplus": "⊕", "ominus": "⊖", "otimes": "⊗", "odot": "⊙", "cup": "∪", "cap": "∩",
	"setminus": "∖", "land": "∧", "wedge": "∧", "lor": "∨", "vee": "∨", "lnot": "¬", "neg": "¬",
	"forall": "∀", "exists": "∃", "nexists": "∄", "sum": "∑", "prod": "∏", "coprod": "∐", "int": "∫",
	"iint": "∬", "oint": "∮", "bigcup": "⋃", "bigcap": "⋂", "bigoplus": "⨁", "bigotimes": "⨂",
	"partial": "∂", "nabla": "∇", "infty": "∞", "emptyset": "∅", "varnothing": "∅", "aleph": "ℵ",
	"ell": "ℓ", "hbar": "ℏ", "Re": "ℜ", "Im": "ℑ", "wp": "℘", "angle": "∠", "triangle": "△",
	"therefore": "∴", "because": "∵", "prime": "′", "degree": "°", "checkmark": "✓", "dagger": "†",
	"ddagger": "‡", "S": "§", "P": "¶", "top": "⊤", "bot": "⊥", "square": "□", "Box": "□", "Diamond": "◇",
	// Dots and delimiters.
	"ldots": "…", "dots": "…", "cdots": "⋯", "vdots": "⋮", "ddots": "⋱",
	"langle": "⟨", "rangle": "⟩", "lfloor": "⌊", "rfloor": "⌋", "lceil": "⌈", "rceil": "⌉",
	"vert": "|", "lvert": "|", "rvert": "|", "Vert": "‖", "lVert": "‖", "rVert": "‖",
	"lbrace": "{", "rbrace": "}", "backslash": "\\",
	// Greek.
	"alpha": "α", "beta": "β", "gamma": "γ", "delta": "δ", "epsilon": "ϵ", "varepsilon": "ε", "zeta": "ζ",
	"eta": "η", "theta": "θ", "vartheta": "ϑ", "iota": "ι", "kappa": "κ", "lambda": "λ", "mu": "μ",
	"nu": "ν", "xi": "ξ", "pi": "π", "varpi": "ϖ", "rho": "ρ", "varrho": "ϱ", "sigma": "σ",
	"varsigma": "ς", "tau": "τ", "upsilon": "υ", "phi": "ϕ", "varphi": "φ", "chi": "χ", "psi": "ψ",
	"omega": "ω", "Gamma": "Γ", "Delta": "Δ", "Theta": "Θ", "Lambda": "Λ", "Xi": "Ξ", "Pi": "Π",
	"Sigma": "Σ", "Upsilon": "Υ", "Phi": "Φ", "Psi": "Ψ", "Omega": "Ω",
}
