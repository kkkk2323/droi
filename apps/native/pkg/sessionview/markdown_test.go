package sessionview

import (
	"strings"
	"testing"

	"github.com/kkkk2323/droid-sdk-go/fakedaemon"
)

// wideTable is a reply with a table whose columns ask for more than the
// reading column, a narrow table, and a wide table in a list.
var wideTable = strings.Join([]string{
	"Prose above the tables.",
	"",
	"| Problem | Evidence | Impact |",
	"| --- | --- | --- |",
	"| The first query fails and the second one works | Without the cache the endpoint takes 2.8 to 4.9 seconds, and with it 0.5 seconds; the log shows each ship asked for twice | The caller gives up after 3 seconds, so its first query always times out |",
	"| Lengths are in the wrong unit | The endpoint answers lengths in decimetres, so 1158 is 115.8 metres, and only the beam is divided | Ships show as 1158 metres long |",
	"",
	"| File | Change |",
	"| --- | --- |",
	"| `login.ts` | await the refresh |",
	"",
	"- An item",
	"",
	"  | Listed | Evidence | Effect |",
	"  | --- | --- | --- |",
	"  | The first query fails and the second one works | Without the cache the endpoint takes 2.8 to 4.9 seconds, and with it 0.5 seconds | The caller gives up after 3 seconds |",
	"",
	"Prose under the tables.",
}, "\n")

func wideTableScenario(reply string) fakedaemon.Scenario {
	return fakedaemon.Scenario{Sessions: []fakedaemon.SessionSpec{{Title: "Tables", Cwd: "/Users/dev/acme-web", Messages: []fakedaemon.Message{
		{Role: "user", Text: "What broke?"},
		{Role: "assistant", Text: reply},
	}}}}
}

func (h *harness) box(text string) (x, right float32) {
	h.t.Helper()
	r, ok := h.tt.Find(text)
	if !ok {
		h.t.Fatalf("no %q in %q", text, h.tt.Texts())
	}
	return r.X, r.X + r.W
}

// A top-level table wider than the reading column grows past it, centered
// on it, while the prose, a narrow table and a table in a list stay in it.
func TestAWideTableGrowsPastTheReadingColumn(t *testing.T) {
	h := newHarness(t, wideTableScenario(wideTable))
	h.tt.SetSize(1400, 900)
	h.until("the reply", func() bool { return h.hasText("Prose under the tables.") })
	h.until("the table past the column", func() bool {
		prose, _ := h.box("Prose above the tables.")
		problem, _ := h.box("Problem")
		return problem < prose-40
	})
	prose, _ := h.box("Prose above the tables.")
	under, _ := h.box("Prose under the tables.")
	if under != prose {
		t.Errorf("the prose moved: %v above the tables, %v under them", prose, under)
	}
	problem, _ := h.box("Problem")
	_, impact := h.box("Impact")
	center := float32(1400) / 2
	if impact > 1400 {
		t.Errorf("the table runs out of the window, to %v", impact)
	}
	if left, right := center-problem, impact-center; left < right {
		t.Errorf("the table is not centered: %v left of the middle, %v right of it", left, right)
	}
	if file, _ := h.box("File"); file < prose || file > prose+16 {
		t.Errorf("the narrow table left the column: at %v, the prose at %v", file, prose)
	}
	if listed, _ := h.box("Listed"); listed < prose {
		t.Errorf("the table in a list left the column: at %v, the prose at %v", listed, prose)
	}
}

// Where the room is short of what its columns ask, a wide table wraps its
// widest columns sooner, to show whole.
func TestAWideTableWrapsItsColumnsToFit(t *testing.T) {
	h := newHarness(t, wideTableScenario(wideTable))
	h.until("the reply", func() bool { return h.hasText("Prose under the tables.") })
	h.until("the table past the column", func() bool {
		prose, _ := h.box("Prose above the tables.")
		problem, _ := h.box("Problem")
		return problem < prose-20
	})
	if _, impact := h.box("Impact"); impact > 900-scrollGutter {
		t.Errorf("the table runs out of the window, to %v", impact)
	}
}

// A table that streams in grows past the column once it is wider.
func TestAWideTableStreamsIn(t *testing.T) {
	h := newHarness(t, deployScenario(&fakedaemon.Turn{Kind: "reply", Deltas: []string{
		"Prose above the table.\n\n| Problem | Evidence | Impact |\n| --- | --- | --- |\n",
		"| The first query fails and the second one works | Without the cache the endpoint takes 2.8 to 4.9 seconds, ",
		"and with it 0.5 seconds; the log shows each ship asked for twice | The caller gives up after 3 seconds, so its first query always times out |\n",
		"\nProse under the table.",
	}}))
	h.tt.SetSize(1400, 900)
	h.send("What broke?")
	h.until("the reply", func() bool {
		return h.hasText("Prose under the table.") && h.hasText("so its first query always times out")
	})
	h.until("the table past the column", func() bool {
		prose, _ := h.box("Prose above the table.")
		problem, _ := h.box("Problem")
		return problem < prose-40
	})
}

func TestATableFitsByWrappingItsWidestColumns(t *testing.T) {
	l := tableLayout{text: []float32{100, 600, 400}, pad: 20, em: 10, maxCol: 24}
	if got := l.width(24); got != 2+100+240+240+60 {
		t.Fatalf("width %v", got)
	}
	if got := l.fit(1000); got != 24 {
		t.Errorf("a table with room to spare fits at %v ems", got)
	}
	// 2 + 100 + 2×180 + 60 = 522.
	if got := l.fit(522); got != 18 {
		t.Errorf("fits at %v ems, not 18", got)
	}
	if got := l.fit(300); got != tableMinColumn {
		t.Errorf("a table with no room fits at %v ems, not %v", got, tableMinColumn)
	}
}
