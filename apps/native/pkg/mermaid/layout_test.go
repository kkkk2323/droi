package mermaid

import (
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// corpus reads testdata/corpus: examples from Mermaid's documentation and
// layout cases from Pele (see testdata/README.md).
func corpus(t *testing.T) map[string]string {
	t.Helper()
	paths, err := filepath.Glob("testdata/corpus/*.mmd")
	if err != nil || len(paths) == 0 {
		t.Fatalf("no corpus: %v", err)
	}
	out := map[string]string{}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		out[strings.TrimSuffix(filepath.Base(p), ".mmd")] = string(b)
	}
	return out
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Diagrams from the layout notes that are hard to get right, kept inline.
var curated = map[string]string{
	"td-subgraphs":   "flowchart TB\n    c1-->a2\n    subgraph one\n    a1-->a2\n    end\n    subgraph two\n    b1-->b2\n    end\n    subgraph three\n    c1-->c2\n    end\n    one --> two\n    three --> two\n    two --> c2\n",
	"lr-labels":      "flowchart LR\n    A[Hard edge] -->|Link text| B(Round edge)\n    B --> C{Decision}\n    C -->|One| D[Result one]\n    C -->|Two| E[Result two]\n",
	"diamonds":       "flowchart TD\n    A[Start] --> B{Is it?}\n    B -->|Yes| C{Really?}\n    C -->|Yes| D[OK]\n    C -->|No| B\n    B ---->|No| E((End))\n    D --> F{{Prepare}} --> G[(Store)]\n    G -.-> B\n",
	"cjk":            "flowchart TD\n  Start([\"打开小程序\"]) --> Splash[\"splash: wx.login + /wxcode 获取 openid\"]\n  Splash --> ML{\"manualLogout? 曾手动退出/解绑\"}\n  ML -- 是 --> TryS[\"/user/get 探活现有会话\"]\n  ML -- 否 --> Login[\"login 登录页\"]\n  TryS --> Home([\"进入 home\"])\n  Login --> Home\n",
	"self-loops":     "flowchart LR\n  A --> A\n  A -->|again| A\n  A --> B{B}\n  B --> B\n",
	"bt-rl":          "flowchart BT\n  subgraph S [Group]\n    a --> b\n  end\n  b --> c\n  c --> a\n",
	"rl":             "flowchart RL\n  subgraph S [Group]\n    a --> b\n  end\n  b --> c\n  c -- back --> a\n",
	"nested-ports":   "flowchart TB\n  subgraph outer\n    subgraph inner\n      x --> y\n    end\n    z\n  end\n  top --> x\n  y --> bottom\n  z --> top\n  bottom --> inner\n",
	"group-to-group": "flowchart LR\n  subgraph A\n    a1\n  end\n  subgraph B\n    b1\n  end\n  A --> B\n  B --> A\n  a1 --> b1\n",
	"shapes":         "flowchart LR\n  a[rect] --> b(round) --> c([stadium]) --> d[[sub]] --> e[(cyl)]\n  e --> f((circle)) --> g(((double))) --> h>odd] --> i{diamond}\n  i --> j{{hex}} --> k[/lean r/] --> l[\\lean l\\] --> m[/trap b\\] --> n[\\trap t/]\n",
	"long-edges":     "flowchart TD\n  A --> B --> C --> D --> E\n  A --> E\n  A ---->|long| D\n  B -.-> E\n",
	"stray":          "flowchart TD\n  subgraph G\n    a --> b\n  end\n  a --> G\n  G --> b\n",
	"seq-alt-notes":  "sequenceDiagram\n    autonumber\n    actor U as User\n    Alice->>+Bob: Hello Bob, how are you?\n    alt is sick\n        Bob->>Alice: Not so good :(\n    else is well\n        Bob->>Alice: Feeling fresh like a daisy\n    end\n    loop Every minute\n        Bob-->>Bob: Self check\n        Note right of Bob: Rational thoughts\n    end\n    Note over Alice,Bob: A typical interaction<br/>in two lines\n    opt Extra response\n        Bob--)-Alice: Thanks for asking\n    end\n    U-xAlice: bye\n",
	"seq-cjk":        "sequenceDiagram\n    participant Syn as Syncer\n    participant OSS as Aliyun OSS\n    Note over OSS: active.json → \"slot_a\"\n    par Syncer 写入非活跃 slot\n        Syn->>OSS: PUT slot_b/meta.json\n    end\n    Note over Syn: 校验所有文件写入成功\n    Syn->>OSS: PUT active.json → \"slot_b\"\n",
}

// checkScene checks that everything lies within the Scene's size and that
// rendering again gives the same Scene.
func checkScene(t *testing.T, name, src string, sc *Scene) {
	t.Helper()
	const tol = 0.01
	in := func(x, y float32) bool {
		return x >= -tol && y >= -tol && x <= sc.Width+tol && y <= sc.Height+tol
	}
	for _, sh := range sc.Shapes {
		for _, op := range sh.Path {
			if op.Kind != Close && !in(op.X, op.Y) || (op.Kind == QuadTo || op.Kind == CubicTo) && !in(op.X1, op.Y1) || op.Kind == CubicTo && !in(op.X2, op.Y2) {
				t.Errorf("%s: point outside %gx%g: %+v", name, sc.Width, sc.Height, op)
				return
			}
		}
	}
	for _, tx := range sc.Texts {
		if !in(tx.X, tx.Y) || !in(tx.X+tx.W, tx.Y+tx.H) || tx.Text == "" {
			t.Errorf("%s: text outside or empty: %+v", name, tx)
			return
		}
	}
	again, err := Render(src, Options{FontSize: 14})
	if err != nil || !reflect.DeepEqual(sc, again) {
		t.Errorf("%s: a second render differs (%v)", name, err)
	}
}

func overlaps(a, b rect, tol float64) bool {
	return a.x0 < b.x1-tol && b.x0 < a.x1-tol && a.y0 < b.y1-tol && b.y0 < a.y1-tol
}

func within(inner, outer rect, tol float64) bool {
	return inner.x0 >= outer.x0-tol && inner.y0 >= outer.y0-tol && inner.x1 <= outer.x1+tol && inner.y1 <= outer.y1+tol
}

// outline traces the drawn outline of a node, curves as short lines.
func outline(v nodeView, cx, cy, s float64) []pt {
	w, h := v.w, v.h
	x0, y0, x1, y1 := cx-w/2, cy-h/2, cx+w/2, cy+h/2
	ellipse := func(rx, ry, from, to float64, ox, oy float64) []pt {
		var out []pt
		for i := 0; i <= 32; i++ {
			a := from + (to-from)*float64(i)/32
			out = append(out, pt{ox + rx*math.Cos(a), oy + ry*math.Sin(a)})
		}
		return out
	}
	poly := func(xy ...float64) []pt {
		var out []pt
		for i := 0; i+1 < len(xy); i += 2 {
			out = append(out, pt{xy[i], xy[i+1]})
		}
		return append(out, out[0])
	}
	switch v.shape {
	case shCircle, shDoubleCircle:
		return ellipse(w/2, h/2, 0, 2*math.Pi, cx, cy)
	case shStadium:
		r := h / 2
		out := ellipse(r, r, math.Pi/2, 3*math.Pi/2, x0+r, cy)
		out = append(out, ellipse(r, r, -math.Pi/2, math.Pi/2, x1-r, cy)...)
		return append(out, out[0])
	case shCylinder:
		ry := cylinderRy(w, s)
		out := ellipse(w/2, ry, math.Pi, 2*math.Pi, cx, y0+ry)
		out = append(out, ellipse(w/2, ry, 0, math.Pi, cx, y1-ry)...)
		return append(out, out[0])
	case shDiamond:
		return poly(cx, y0, x1, cy, cx, y1, x0, cy)
	case shHexagon:
		m := h / 4
		return poly(x0+m, y0, x1-m, y0, x1, cy, x1-m, y1, x0+m, y1, x0, cy)
	case shOdd:
		return poly(x0, y0, x1, y0, x1, y1, x0, y1, x0+h/4, cy)
	case shLeanR:
		return poly(x0+h/4, y0, x1, y0, x1-h/4, y1, x0, y1)
	case shLeanL:
		return poly(x0, y0, x1-h/4, y0, x1, y1, x0+h/4, y1)
	case shTrapB:
		return poly(x0+h/4, y0, x1-h/4, y0, x1, y1, x0, y1)
	case shTrapT:
		return poly(x0, y0, x1, y0, x1-h/4, y1, x0+h/4, y1)
	}
	return poly(x0, y0, x1, y0, x1, y1, x0, y1)
}

func distToOutline(p pt, line []pt) float64 {
	best := math.Inf(1)
	for i := 1; i < len(line); i++ {
		a, b := line[i-1], line[i]
		dx, dy := b.x-a.x, b.y-a.y
		t := 0.0
		if l := dx*dx + dy*dy; l > 0 {
			t = math.Max(0, math.Min(1, ((p.x-a.x)*dx+(p.y-a.y)*dy)/l))
		}
		best = math.Min(best, math.Hypot(p.x-a.x-t*dx, p.y-a.y-t*dy))
	}
	return best
}

func ancestor(items []*fitem, a, b int) bool {
	for p := items[b].parent; p >= 0; p = items[p].parent {
		if p == a {
			return true
		}
	}
	return false
}

// checkFlow checks the layout of a flowchart: nodes apart, edges ending on
// outlines, labels off nodes, and groups around what they hold.
func checkFlow(t *testing.T, name string, fp *flowPlace) {
	t.Helper()
	items := fp.fc.items
	var nodes []int
	for i, it := range items {
		if !it.group {
			nodes = append(nodes, i)
		}
	}
	for x, i := range nodes {
		for _, j := range nodes[x+1:] {
			if overlaps(fp.nodeRect(i), fp.nodeRect(j), 0.5) {
				t.Errorf("%s: nodes %s and %s overlap", name, items[i].id, items[j].id)
			}
		}
	}
	for _, e := range fp.edges {
		if len(e.pts) < 2 || ancestor(items, e.src, e.dst) || ancestor(items, e.dst, e.src) {
			continue
		}
		for k, end := range []struct {
			i int
			p pt
		}{{e.src, e.pts[0]}, {e.dst, e.pts[len(e.pts)-1]}} {
			var d float64
			if items[end.i].group {
				b := fp.boxes[end.i]
				d = distToOutline(end.p, []pt{{b.x0, b.y0}, {b.x1, b.y0}, {b.x1, b.y1}, {b.x0, b.y1}, {b.x0, b.y0}})
			} else {
				r := fp.nodeRect(end.i)
				d = distToOutline(end.p, outline(fp.views[end.i], (r.x0+r.x1)/2, (r.y0+r.y1)/2, fp.s))
			}
			if d > 1 {
				t.Errorf("%s: edge %s-%s end %d is %.1fpx off %s's outline", name, items[e.src].id, items[e.dst].id, k, d, items[end.i].id)
			}
		}
	}
	for _, l := range fp.labels {
		r := rect{l.cx - l.lab.w/2, l.cy - l.lab.h/2, l.cx + l.lab.w/2, l.cy + l.lab.h/2}
		for _, i := range nodes {
			if overlaps(r, fp.nodeRect(i), 0.5) {
				t.Errorf("%s: label %q overlaps node %s", name, l.lab.lines, items[i].id)
			}
		}
	}
	for i, it := range items {
		if it.parent < 0 {
			continue
		}
		r := fp.boxes[i]
		if !it.group {
			r = fp.nodeRect(i)
		}
		if !within(r, fp.boxes[it.parent], 0.5) {
			t.Errorf("%s: %s sticks out of group %s", name, it.id, items[it.parent].id)
		}
	}
}

func renderAndCheck(t *testing.T, name, src string) {
	t.Helper()
	sc, err := Render(src, Options{FontSize: 14})
	if err != nil {
		t.Errorf("%s: %v", name, err)
		return
	}
	writeSVG(t, name, sc)
	checkScene(t, name, src, sc)
	if kind, v, err := parseOf(t, src); err == nil && kind == "flowchart" {
		checkFlow(t, name, placeFlowchart(v.(*flowchart), 14))
	}
}

func TestCorpus(t *testing.T) {
	c := corpus(t)
	for _, name := range sortedKeys(c) {
		renderAndCheck(t, name, c[name])
	}
}

func TestCurated(t *testing.T) {
	for _, name := range sortedKeys(curated) {
		renderAndCheck(t, name, curated[name])
	}
}

// TestHistory renders the diagrams in $DROI_MERMAID_HISTORY, a local folder
// of real diagrams that is not part of the repository.
func TestHistory(t *testing.T) {
	dir := os.Getenv("DROI_MERMAID_HISTORY")
	if dir == "" {
		t.Skip("DROI_MERMAID_HISTORY is not set")
	}
	paths, _ := filepath.Glob(filepath.Join(dir, "*.mmd"))
	count := 0
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		src := string(b)
		lines := strings.Split(src, "\n")
		_, start := frontMatter(lines)
		if kind, _ := detect(lines, start); kind == "" {
			continue
		}
		count++
		renderAndCheck(t, "history-"+strings.TrimSuffix(filepath.Base(p), ".mmd"), src)
	}
	t.Logf("rendered %d flowchart and sequence diagrams", count)
}

func TestFontSizeScales(t *testing.T) {
	src := curated["lr-labels"]
	small, _ := Render(src, Options{FontSize: 10})
	big, _ := Render(src, Options{FontSize: 20})
	if r := big.Width / small.Width; r < 1.8 || r > 2.2 {
		t.Errorf("width ratio %v for a font twice as large", r)
	}
	def, _ := Render(src, Options{})
	if def.Texts[0].Size != 14*0.875 && def.Texts[0].Size != 14 {
		t.Errorf("default size %v", def.Texts[0].Size)
	}
}
