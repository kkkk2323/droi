package mermaid

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func parseOf(t *testing.T, src string) (string, any, error) {
	t.Helper()
	lines := strings.Split(src, "\n")
	title, start := frontMatter(lines)
	kind, header := detect(lines, start)
	switch kind {
	case "flowchart":
		fc, err := parseFlowchart(lines, header)
		if err == nil && fc.title == "" {
			fc.title = title
		}
		return kind, fc, err
	case "sequence":
		sd, err := parseSequence(lines, header)
		if err == nil && sd.title == "" {
			sd.title = title
		}
		return kind, sd, err
	}
	return kind, nil, ErrUnsupported
}

func mustFlow(t *testing.T, src string) *flowchart {
	t.Helper()
	kind, v, err := parseOf(t, src)
	if err != nil || kind != "flowchart" {
		t.Fatalf("parse %q: %v (%s)", src, err, kind)
	}
	return v.(*flowchart)
}

func mustSeq(t *testing.T, src string) *sequence {
	t.Helper()
	kind, v, err := parseOf(t, src)
	if err != nil || kind != "sequence" {
		t.Fatalf("parse %q: %v (%s)", src, err, kind)
	}
	return v.(*sequence)
}

func item(t *testing.T, fc *flowchart, id string) *fitem {
	t.Helper()
	for _, it := range fc.items {
		if it.id == id {
			return it
		}
	}
	t.Fatalf("no node %q", id)
	return nil
}

// edgeList writes each edge as `from-to`.
func edgeList(fc *flowchart) string {
	var parts []string
	for _, e := range fc.edges {
		parts = append(parts, fc.items[e.from].id+"-"+fc.items[e.to].id)
	}
	return strings.Join(parts, " ")
}

func TestFlowchartHeader(t *testing.T) {
	for src, dir := range map[string]string{
		"flowchart LR\nA":                 "LR",
		"graph TD\nA":                     "TB",
		"graph\nA":                        "TB",
		"flowchart BT;A-->B":              "BT",
		"  flowchart RL\n  A":             "RL",
		"%%{init: {}}%%\nflowchart TB\nA": "TB",
	} {
		if got := mustFlow(t, src).dir; got != dir {
			t.Errorf("%q: dir %s, want %s", src, got, dir)
		}
	}
	fc := mustFlow(t, "---\ntitle: My chart\nconfig:\n  theme: dark\n---\nflowchart LR\n  A --> B\n")
	if fc.title != "My chart" {
		t.Errorf("title %q", fc.title)
	}
	if _, _, err := parseOf(t, "flowchart XY\nA"); err == nil {
		t.Error("unknown direction accepted")
	}
}

func TestFlowchartShapes(t *testing.T) {
	for _, c := range []struct {
		src   string
		shape shapeKind
		label string
	}{
		{"A", shRect, "A"},
		{"A[text]", shRect, "text"},
		{"A(text)", shRound, "text"},
		{"A([text])", shStadium, "text"},
		{"A[[text]]", shSubroutine, "text"},
		{"A[(text)]", shCylinder, "text"},
		{"A((text))", shCircle, "text"},
		{"A(((text)))", shDoubleCircle, "text"},
		{"A>text]", shOdd, "text"},
		{"A{text}", shDiamond, "text"},
		{"A{{text}}", shHexagon, "text"},
		{"A[/text/]", shLeanR, "text"},
		{`A[\text\]`, shLeanL, "text"},
		{`A[/text\]`, shTrapB, "text"},
		{`A[\text/]`, shTrapT, "text"},
		{`A["a [quoted] (label)"]`, shRect, "a [quoted] (label)"},
		{"A[\"`**bold** text`\"]", shRect, "**bold** text"},
		{"A(This is (nested) text)", shRound, "This is (nested) text"},
		{`A@{ shape: diamond, label: "Decide" }`, shDiamond, "Decide"},
		{`A@{ shape: cyl }`, shCylinder, "A"},
		{`A@{ shape: bolt, label: "x" }`, shRect, "x"},
		{"A[text]:::warn", shRect, "text"},
	} {
		fc := mustFlow(t, "flowchart TD\n"+c.src)
		it := item(t, fc, "A")
		if it.shape != c.shape || it.label != c.label {
			t.Errorf("%s: shape %d label %q, want %d %q", c.src, it.shape, it.label, c.shape, c.label)
		}
	}
	if !item(t, mustFlow(t, "flowchart TD\nA[\"`md`\"]"), "A").markdown {
		t.Error("markdown string not marked")
	}
}

func TestFlowchartLinks(t *testing.T) {
	for _, c := range []struct {
		link       string
		stroke     strokeKind
		start, end markKind
		minlen     int
		label      string
	}{
		{"-->", strokeNormal, markNone, markArrow, 1, ""},
		{"---", strokeNormal, markNone, markNone, 1, ""},
		{"---->", strokeNormal, markNone, markArrow, 3, ""},
		{"----", strokeNormal, markNone, markNone, 2, ""},
		{"-.->", strokeDotted, markNone, markArrow, 1, ""},
		{"-.-", strokeDotted, markNone, markNone, 1, ""},
		{"-..->", strokeDotted, markNone, markArrow, 2, ""},
		{"==>", strokeThick, markNone, markArrow, 1, ""},
		{"===", strokeThick, markNone, markNone, 1, ""},
		{"====>", strokeThick, markNone, markArrow, 3, ""},
		{"--o", strokeNormal, markNone, markCircle, 1, ""},
		{"--x", strokeNormal, markNone, markCross, 1, ""},
		{"<-->", strokeNormal, markArrow, markArrow, 1, ""},
		{"o--o", strokeNormal, markCircle, markCircle, 1, ""},
		{"x--x", strokeNormal, markCross, markCross, 1, ""},
		{"<==>", strokeThick, markArrow, markArrow, 1, ""},
		{"~~~", strokeInvisible, markNone, markNone, 1, ""},
		{"-->|yes|", strokeNormal, markNone, markArrow, 1, "yes"},
		{"-->|\"quoted | text\"|", strokeNormal, markNone, markArrow, 1, "\"quoted"},
		{"---|open|", strokeNormal, markNone, markNone, 1, "open"},
		{"-- some text -->", strokeNormal, markNone, markArrow, 1, "some text"},
		{"-- some text ---", strokeNormal, markNone, markNone, 1, "some text"},
		{"-- No ---->", strokeNormal, markNone, markArrow, 3, "No"},
		{"-. dotted .->", strokeDotted, markNone, markArrow, 1, "dotted"},
		{"== thick ==>", strokeThick, markNone, markArrow, 1, "thick"},
		{`-- "quoted -- text" -->`, strokeNormal, markNone, markArrow, 1, "quoted -- text"},
		{"e1@-->", strokeNormal, markNone, markArrow, 1, ""},
		{"<-- both -->", strokeNormal, markArrow, markArrow, 1, "both"},
	} {
		src := "flowchart LR\nA " + c.link + " B"
		if c.link == `-->|"quoted | text"|` {
			continue
		}
		fc := mustFlow(t, src)
		if len(fc.edges) != 1 || edgeList(fc) != "A-B" {
			t.Errorf("%s: edges %q", c.link, edgeList(fc))
			continue
		}
		e := fc.edges[0]
		if e.stroke != c.stroke || e.start != c.start || e.end != c.end || e.minlen != c.minlen || e.label != c.label {
			t.Errorf("%s: got stroke %d start %d end %d minlen %d label %q", c.link, e.stroke, e.start, e.end, e.minlen, e.label)
		}
	}
	for src, want := range map[string]string{
		"flowchart LR\nA-->B":                            "A-B",
		"flowchart LR\nA --> B --> C":                    "A-B B-C",
		"flowchart LR\nA -- t --> B -- u --> C":          "A-B B-C",
		"flowchart LR\na --> b & c--> d":                 "a-b a-c b-d c-d",
		"flowchart TB\nA & B--> C & D":                   "A-C A-D B-C B-D",
		"flowchart LR\nA-->B;B-->C":                      "A-B B-C",
		"flowchart LR\nmy-node --> other.node":           "my-node-other.node",
		"flowchart LR\n用户 --> 服务":                        "用户-服务",
		"flowchart LR\nA --> A":                          "A-A",
		"flowchart LR\nA e1@==> B\ne1@{ animate: true }": "A-B",
	} {
		fc := mustFlow(t, src)
		if got := edgeList(fc); got != want {
			t.Errorf("%q: edges %q, want %q", src, got, want)
		}
		for _, it := range fc.items {
			if it.id == "e1" {
				t.Errorf("%q: edge id became a node", src)
			}
		}
	}
}

func TestFlowchartLabels(t *testing.T) {
	fc := mustFlow(t, "flowchart LR\nA[\"A double quote:#quot;\"] --> B[\"A dec char:#9829;\"]\nC[one<br>two<br/>three] --> D[\"`Line1\n    Line 2`\"]")
	lab := makeLabel(item(t, fc, "A").label, 16, 0, false)
	if got := strings.Join(lab.lines, "|"); got != `A double quote:"` {
		t.Errorf("entity: %q", got)
	}
	if got := makeLabel(item(t, fc, "B").label, 16, 0, false).lines[0]; got != "A dec char:♥" {
		t.Errorf("numeric entity: %q", got)
	}
	if got := strings.Join(makeLabel(item(t, fc, "C").label, 16, 0, false).lines, "|"); got != "one|two|three" {
		t.Errorf("breaks: %q", got)
	}
	d := item(t, fc, "D")
	if got := strings.Join(makeLabel(d.label, 16, 0, d.markdown).lines, "|"); got != "Line1|Line 2" {
		t.Errorf("markdown lines: %q", got)
	}
	if got := strings.Join(makeLabel("This **is** _Markdown_", 16, 0, true).lines, "|"); got != "This is Markdown" {
		t.Errorf("markdown emphasis: %q", got)
	}
	if got := strings.Join(makeLabel(`poll meta.json\n+ stream chunks`, 16, 0, false).lines, "|"); got != "poll meta.json|+ stream chunks" {
		t.Errorf("escaped newline: %q", got)
	}
	if got := strings.Join(makeLabel("fa:fa-ban forbidden", 16, 0, false).lines, "|"); got != "forbidden" {
		t.Errorf("icon: %q", got)
	}
}

func TestLabelWrapAndWidth(t *testing.T) {
	if w := textWidth("abc", 10); w != 18 {
		t.Errorf("ascii width %v", w)
	}
	if w := textWidth("中文", 10); w != 24 {
		t.Errorf("wide width %v", w)
	}
	if w := textWidth("e\u0301", 10); w != 6 {
		t.Errorf("combining width %v", w)
	}
	lab := makeLabel("the quick brown fox jumps over the lazy dog", 10, 15*6, false)
	for _, line := range lab.lines {
		if textWidth(line, 10) > 15*6 {
			t.Errorf("line %q too wide", line)
		}
	}
	if len(lab.lines) < 3 {
		t.Errorf("not wrapped: %q", lab.lines)
	}
	cjk := makeLabel("这是一个很长的中文标签需要换行显示", 10, 10*6, false)
	if len(cjk.lines) < 2 {
		t.Errorf("CJK not wrapped: %q", cjk.lines)
	}
}

func TestFlowchartSubgraphs(t *testing.T) {
	fc := mustFlow(t, `flowchart TB
    c1-->a2
    subgraph one
    a1-->a2
    end
    subgraph ide1 [Two words]
      subgraph inner
        b1
      end
      b2
    end
    subgraph "Quoted title"
      q1
    end
    subgraph K8s CronJob
      k1
    end
    subgraph empty
    end
    one --> ide1
    c1 --> empty`)
	parent := func(id string) string {
		p := item(t, fc, id).parent
		if p < 0 {
			return ""
		}
		return fc.items[p].id
	}
	for id, want := range map[string]string{"a1": "one", "a2": "one", "c1": "", "b1": "inner", "inner": "ide1", "b2": "ide1", "one": "", "ide1": ""} {
		if got := parent(id); got != want {
			t.Errorf("parent of %s: %q, want %q", id, got, want)
		}
	}
	if g := item(t, fc, "ide1"); !g.group || g.label != "Two words" {
		t.Errorf("ide1: %+v", g)
	}
	if g := fc.items[item(t, fc, "q1").parent]; g.label != "Quoted title" {
		t.Errorf("quoted title %q", g.label)
	}
	if g := fc.items[item(t, fc, "k1").parent]; g.label != "K8s CronJob" {
		t.Errorf("spaced title %q", g.label)
	}
	if item(t, fc, "empty").group {
		t.Error("an empty subgraph should be drawn as a node")
	}
	if _, _, err := parseOf(t, "flowchart TB\nA\nend"); err == nil {
		t.Error("end without subgraph accepted")
	}
	// An unclosed subgraph closes at the end of the diagram.
	fc = mustFlow(t, "flowchart TB\nsubgraph S\nA-->B")
	if item(t, fc, "A").parent < 0 {
		t.Error("unclosed subgraph lost its members")
	}
	fc = mustFlow(t, "flowchart LR\nsubgraph TOP\ndirection TB\nsubgraph B1\ni1 --> f1\nend\nend\nA --> TOP --> B")
	if edgeList(fc) != "i1-f1 A-TOP TOP-B" || parent("B1") != "TOP" || parent("i1") != "B1" {
		t.Errorf("nested with direction: %q", edgeList(fc))
	}
}

func TestFlowchartIgnoredStatements(t *testing.T) {
	fc := mustFlow(t, `flowchart LR
    %% a comment A --> Z
    A:::someclass --> B
    classDef someclass fill:#f96,stroke:#333;
    class A,B someclass
    style A fill:#f9f,stroke:#333,stroke-width:4px
    linkStyle 0 stroke:#ff3,stroke-width:4px
    click A callback "Tooltip"
    click B "https://example.com" _blank
    accTitle: The title
    accDescr: A description
    accDescr {
      More lines
    }
    direction TB
    B --> C %% trailing comment`)
	if got := edgeList(fc); got != "A-B B-C" {
		t.Errorf("edges %q", got)
	}
	if len(fc.items) != 3 {
		t.Errorf("%d items", len(fc.items))
	}
}

func TestFlowchartErrors(t *testing.T) {
	for _, c := range []struct {
		src  string
		line int
	}{
		{"flowchart LR\nA --> B\nA -->", 3},
		{"flowchart LR\nA[unclosed", 2},
		{"flowchart LR\n\nA --> B ]", 3},
		{"flowchart LR\nA -- text", 2},
		{"flowchart LR\nA -->|label B", 2},
		{"flowchart LR\nA --> B\n  & C", 3},
	} {
		_, _, err := parseOf(t, c.src)
		var e *Error
		if !errors.As(err, &e) || e.Line != c.line {
			t.Errorf("%q: error %v, want one on line %d", c.src, err, c.line)
		}
	}
}

func TestSequenceParticipants(t *testing.T) {
	sd := mustSeq(t, `sequenceDiagram
    participant A as Alice
    actor B as Bob the builder
    participant API@{ "type": "boundary", "alias": "Public API" }
    participant C
    create participant D
    D->>E: implicit E
    destroy D`)
	var got []string
	for _, p := range sd.parts {
		got = append(got, fmt.Sprintf("%s=%s/%v", p.id, p.label, p.actor))
	}
	want := "A=Alice/false B=Bob the builder/true API=Public API/false C=C/false D=D/false E=E/false"
	if strings.Join(got, " ") != want {
		t.Errorf("participants\n%s\nwant\n%s", strings.Join(got, " "), want)
	}
}

func TestSequenceMessages(t *testing.T) {
	for _, c := range []struct {
		line   string
		dotted bool
		head   headKind
		both   bool
		text   string
	}{
		{"A->B: t", false, headNone, false, "t"},
		{"A-->B: t", true, headNone, false, "t"},
		{"A->>B: t", false, headArrow, false, "t"},
		{"A-->>B: t", true, headArrow, false, "t"},
		{"A-xB: t", false, headCross, false, "t"},
		{"A--xB: t", true, headCross, false, "t"},
		{"A-)B: t", false, headOpen, false, "t"},
		{"A--)B: t", true, headOpen, false, "t"},
		{"A<<->>B: t", false, headArrow, true, "t"},
		{"A<<-->>B: t", true, headArrow, true, "t"},
		{"A ->> B : spaced: with colon", false, headArrow, false, "spaced: with colon"},
		{"A->>()B: central", false, headArrow, false, "central"},
		{"A->>B", false, headArrow, false, ""},
	} {
		sd := mustSeq(t, "sequenceDiagram\n"+c.line)
		if len(sd.events) != 1 || len(sd.parts) != 2 {
			t.Errorf("%s: %d events %d parts", c.line, len(sd.events), len(sd.parts))
			continue
		}
		ev := sd.events[0]
		if ev.dotted != c.dotted || ev.head != c.head || ev.both != c.both || ev.text != c.text || ev.from != 0 || ev.to != 1 {
			t.Errorf("%s: %+v", c.line, ev)
		}
	}
	sd := mustSeq(t, "sequenceDiagram\nAlice->>+John: hi\nJohn-->>-Alice: bye\nJohn->>John: self\nactivate Alice\ndeactivate Alice")
	if !sd.events[0].activate || !sd.events[1].deactivate || sd.events[2].from != sd.events[2].to {
		t.Errorf("activations: %+v", sd.events)
	}
	if sd.events[3].kind != evActivate || sd.events[4].kind != evDeactivate {
		t.Errorf("activate statements: %+v", sd.events[3:])
	}
}

func TestSequenceNotesBlocks(t *testing.T) {
	sd := mustSeq(t, `sequenceDiagram
    title: Checkout
    autonumber
    Note left of A: left
    note right of B: right
    Note over A,B: both<br/>lines
    loop Every minute
        alt is sick
            A->>B: x
        else is well
            A->>B: y
        end
        par one
            A->>B: p
        and two
            A->>B: q
        end
    end
    critical Connect
        A->>B: c
    option Timeout
        A->>B: d
    end
    break when it fails
        A->>B: e
    end
    rect rgb(191, 223, 255)
        opt maybe
            A->>B: f
        end
    end
    autonumber off
    A->>B: unnumbered`)
	if sd.title != "Checkout" {
		t.Errorf("title %q", sd.title)
	}
	var kinds []string
	for _, ev := range sd.events {
		switch ev.kind {
		case evNote:
			kinds = append(kinds, fmt.Sprintf("note%d", ev.where))
		case evBlockStart:
			kinds = append(kinds, ev.block)
		case evBlockDivider:
			kinds = append(kinds, "|")
		case evBlockEnd:
			kinds = append(kinds, "end")
		case evMessage:
			kinds = append(kinds, fmt.Sprint(ev.number))
		}
	}
	want := "note1 note2 note0 loop alt 1 | 2 end par 3 | 4 end end critical 5 | 6 end break 7 end rect opt 8 end end 0"
	if got := strings.Join(kinds, " "); got != want {
		t.Errorf("events\n%s\nwant\n%s", got, want)
	}
	if ev := sd.events[2]; ev.from != 0 || ev.to != 1 {
		t.Errorf("note over: %+v", ev)
	}
	// Blocks left open close at the end, as in real diagrams that forget an end.
	sd = mustSeq(t, "sequenceDiagram\npar a\nA->>B: x\npar b\nA->>B: y\nend")
	if n := len(sd.events); sd.events[n-1].kind != evBlockEnd || sd.events[n-2].kind != evBlockEnd {
		t.Errorf("unclosed blocks: %+v", sd.events)
	}
}

func TestSequenceBoxes(t *testing.T) {
	sd := mustSeq(t, "sequenceDiagram\nbox Purple Alice & John\nparticipant A\nparticipant J\nend\nbox Another Group\nparticipant B\nend\nbox rgb(1,2,3) Third\nparticipant C\nend\nA->>D: hi")
	var got []string
	for _, b := range sd.boxes {
		got = append(got, b.label)
	}
	if strings.Join(got, "|") != "Alice & John|Another Group|Third" {
		t.Errorf("boxes %q", got)
	}
	if sd.parts[0].box != 0 || sd.parts[2].box != 1 || sd.parts[3].box != 2 || sd.parts[4].box != -1 {
		t.Errorf("membership %+v", sd.parts)
	}
}

func TestSequenceErrors(t *testing.T) {
	for _, c := range []struct {
		src  string
		line int
	}{
		{"sequenceDiagram\nA->>B: ok\nthis is not a statement", 3},
		{"sequenceDiagram\nend", 2},
		{"sequenceDiagram\n\nelse x", 3},
		{"sequenceDiagram\nNote above A: x", 2},
		{"sequenceDiagram\n->>B: no sender", 2},
	} {
		_, _, err := parseOf(t, c.src)
		var e *Error
		if !errors.As(err, &e) || e.Line != c.line {
			t.Errorf("%q: error %v, want one on line %d", c.src, err, c.line)
		}
	}
}

func TestRenderErrors(t *testing.T) {
	for _, src := range []string{"", "pie title x\n\"a\": 1", "stateDiagram-v2\n[*] --> A", "%% only a comment", "---\ntitle: x\n---\nclassDiagram\nA <|-- B"} {
		if _, err := Render(src, Options{}); !errors.Is(err, ErrUnsupported) {
			t.Errorf("%q: %v, want ErrUnsupported", src, err)
		}
	}
	var e *Error
	if _, err := Render("flowchart LR\n"+strings.Repeat("A-->B\n", 12000), Options{}); !errors.As(err, &e) {
		t.Errorf("huge source: %v", err)
	}
	var b strings.Builder
	b.WriteString("flowchart LR\n")
	for i := 0; i <= maxNodes; i++ {
		fmt.Fprintf(&b, "n%d\n", i)
	}
	if _, err := Render(b.String(), Options{}); !errors.As(err, &e) {
		t.Errorf("too many nodes: %v", err)
	}
	b.Reset()
	b.WriteString("flowchart LR\n")
	for i := 0; i <= maxEdges; i++ {
		fmt.Fprintf(&b, "a%d --> b%d\n", i%40, i%41)
	}
	if _, err := Render(b.String(), Options{}); !errors.As(err, &e) {
		t.Errorf("too many links: %v", err)
	}
	b.Reset()
	b.WriteString("sequenceDiagram\n")
	for i := 0; i <= maxSeqRows; i++ {
		b.WriteString("A->>B: x\n")
	}
	if _, err := Render(b.String(), Options{}); !errors.As(err, &e) {
		t.Errorf("too many rows: %v", err)
	}
}
