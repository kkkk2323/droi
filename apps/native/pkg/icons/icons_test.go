package icons

import (
	"io/fs"
	"strings"
	"testing"
)

func TestEveryIconParses(t *testing.T) {
	entries, err := fs.ReadDir(files, "svg")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) < 50 {
		t.Fatalf("only %d icons", len(entries))
	}
	for _, e := range entries {
		Get(strings.TrimSuffix(e.Name(), ".svg"))
	}
}

func TestRegisterAddsAnIconAndKeepsDrois(t *testing.T) {
	defer func() {
		mu.Lock()
		delete(added, "corp-test-mark")
		delete(added, "check")
		mu.Unlock()
	}()
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M4 4h16v16H4z"/></svg>`)
	Register("corp-test-mark", svg)
	if Get("corp-test-mark") == nil || Stroked("corp-test-mark", 1.5) == nil {
		t.Fatal("the registered icon does not draw")
	}
	Register("check", svg)
	if b, _ := source("check"); string(b) == string(svg) {
		t.Error("a registered icon replaced Droi's")
	}
}
