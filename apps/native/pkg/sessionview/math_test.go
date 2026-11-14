package sessionview

import (
	"strings"
	"testing"
)

func TestTexText(t *testing.T) {
	for _, c := range []struct{ tex, want string }{
		{`\rightarrow`, "→"},
		{` \Rightarrow `, "⇒"},
		{`A \to B`, "A → B"},
		{`O(n^2)`, "O(n²)"},
		{`O(n \log n)`, "O(n log n)"},
		{`x_i`, "xᵢ"},
		{`x_{max}`, "xₘₐₓ"},
		{`a^{n-1}`, "aⁿ⁻¹"},
		{`90^\circ`, "90°"},
		{`\le 10`, "≤ 10"},
		{`\alpha + \beta = \gamma`, "α + β = γ"},
		{`3 \times 4`, "3 × 4"},
		{`\frac{1}{2}`, "1/2"},
		{`\frac{a+b}{2}`, "(a+b)/2"},
		{`\sqrt{2}`, "√2"},
		{`\not= 0`, "≠ 0"},
		{`\text{if } x`, "if x"},
		{`\{1, 2\}`, "{1, 2}"},
		{`\left( x \right)`, "( x )"},
		{`x-1`, "x−1"},
		{`f'(x)`, "f′(x)"},
		{`n`, "n"},
	} {
		got, ok := texText(c.tex)
		if !ok || got != c.want {
			t.Errorf("texText(%q) = %q, %v; want %q", c.tex, got, ok, c.want)
		}
	}
}

// What Unicode cannot write faithfully stays TeX.
func TestTexTextLeavesTheRestAsTeX(t *testing.T) {
	for _, tex := range []string{
		``, ` `,
		`\sqrt[3]{x}`,
		`x^{\alpha}`,
		`a_{\beta}`,
		`\begin{matrix} a & b \end{matrix}`,
		`\unknowncommand`,
		`\frac{1}`,
		`{x`,
		`x}`,
		`x^`,
		`\`,
	} {
		if got, ok := texText(tex); ok {
			t.Errorf("texText(%q) = %q, want it left as TeX", tex, got)
		}
	}
}

// A reply's $\rightarrow$ shows as an arrow, not as its TeX; money stays
// text, math in code stays code, and math Unicode cannot write shows as its
// TeX.
func TestInlineMathShowsAsUnicode(t *testing.T) {
	reply := strings.Join([]string{
		"你漏说一句话 $\\rightarrow$ 她觉得羞耻 $\\rightarrow$ 冷战。",
		"",
		"It costs $5 and $10, and `$\\rightarrow$` is code.",
		"",
		"Cube root: $\\sqrt[3]{x}$.",
	}, "\n")
	h := newHarness(t, wideTableScenario(reply))
	h.until("the reply", func() bool { return h.hasText("Cube root:") })
	if !h.hasText("你漏说一句话 \uFFFC 她觉得羞耻 \uFFFC 冷战。") {
		t.Errorf("the arrows are not drawn in the text: %q", h.tt.Texts())
	}
	if h.hasText("rightarrow$ 她") || h.hasText("$\\rightarrow$ 她") {
		t.Errorf("the arrow shows as TeX: %q", h.tt.Texts())
	}
	if !h.hasText("It costs $5 and $10, and $\\rightarrow$ is code.") {
		t.Errorf("money or code changed: %q", h.tt.Texts())
	}
	if !h.hasText("Cube root: \\sqrt[3]{x}.") {
		t.Errorf("math Unicode cannot write is not its TeX: %q", h.tt.Texts())
	}
}
