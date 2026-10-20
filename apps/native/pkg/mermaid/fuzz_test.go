package mermaid

import (
	"os"
	"path/filepath"
	"testing"
)

func FuzzRender(f *testing.F) {
	for _, name := range []string{"docs-flowchart-102", "docs-flowchart-091", "layout-subgraphs-03", "docs-sequence-026", "docs-sequence-032"} {
		b, err := os.ReadFile(filepath.Join("testdata/corpus", name+".mmd"))
		if err != nil {
			f.Fatal(err)
		}
		f.Add(string(b))
	}
	f.Add("flowchart LR\nA o--o B <--> C x--x D\nsubgraph S [t]\nC ~~~ D\nend")
	f.Fuzz(func(t *testing.T, src string) {
		// render, not Render: a panic must fail the fuzzer rather than be recovered.
		sc, err := render(src, Options{FontSize: 13})
		if err == nil && (sc.Width < 0 || sc.Height < 0) {
			t.Fatalf("negative size %gx%g", sc.Width, sc.Height)
		}
	})
}
