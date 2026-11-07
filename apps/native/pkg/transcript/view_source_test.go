package transcript

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The Kernel path may not learn what happened by reading text.
//
// The Daemon's path recognizes a background command by `Background process started (PID: N)`, a queued question by
// a sentence, a script's run by its closing line; every one of those is a guess about somebody else's wording. The
// Kernel's path reads fields — an entry's kind, a message's stop reason, a result's isError — so the check below
// allows it nothing to search text with.
//
// It is an allowlist of imports rather than a list of forbidden calls, because a list of calls invites the next
// spelling: `bytes.Contains([]byte(text), …)` slipped past the first version of this guard, and so would a helper in
// another file. The files it covers are every `view_*.go` in this package — the naming is the contract, so a mapping
// split into view_blocks.go is covered without anyone remembering to add it.
//
// What it cannot see: code that reads text through a helper in a file not named view_*. Nothing can, short of a
// type-level separation. Naming the files in one place is the honest version of that.
func TestTheKernelPathImportsNoWayToReadText(t *testing.T) {
	// The imports a mapping may have: JSON, comparison, formatting, and the packages that name the data.
	allowed := map[string]bool{
		"bytes":         true,
		"encoding/json": true,
		"fmt":           true,
		"github.com/kkkk2323/droid-sdk-go/protocol":       true,
		"github.com/kkkk2323/droi/apps/native/pkg/kernel": true,
		"github.com/kkkk2323/droi/apps/native/pkg/l10n":   true,
	}
	// bytes is allowed for the cache's comparison of unchanged JSON, not for searching text in it.
	forbiddenInBytes := []string{"bytes.Contains", "bytes.HasPrefix", "bytes.HasSuffix", "bytes.Index", "bytes.Split", "bytes.Fields"}

	dir := packageDir(t)
	files, err := filepath.Glob(filepath.Join(dir, "view_*.go"))
	if err != nil {
		t.Fatalf("listing the package: %v", err)
	}
	checked := 0
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		checked++
		name := filepath.Base(file)
		imports := importsOf(t, file)
		for _, path := range imports {
			if !allowed[path] {
				t.Errorf("%s imports %q: the Kernel path reads fields, never text to decide what happened", name, path)
			}
		}
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, call := range forbiddenInBytes {
			if strings.Contains(string(src), call) {
				t.Errorf("%s calls %q: searching a field's bytes is reading text", name, call)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no view_*.go to check: the guard has lost the files it is meant to cover")
	}
}

// packageDir is this package's directory, from the compiled test's own path.
func packageDir(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("working directory: %v", err)
	}
	return wd
}

// importsOf is one file's imports, as the source spells them.
func importsOf(t *testing.T, file string) []string {
	t.Helper()
	parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("%s: %v", file, err)
	}
	out := make([]string, 0, len(parsed.Imports))
	for _, spec := range parsed.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		out = append(out, path)
	}
	return out
}
