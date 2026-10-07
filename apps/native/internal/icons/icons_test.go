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
