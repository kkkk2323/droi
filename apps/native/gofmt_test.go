package main

import (
	"bytes"
	"go/format"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// gofmt lays out some lines differently from one Go release to the next (the
// columns after pkg/l10n/zh.go's wide characters), and the PR workflow checks
// with go.mod's. A gofmt on PATH from an older Go writes files it rejects;
// go test runs go.mod's Go, so this fails before the push does.
func TestTheSourcesAreFormattedByThisGo(t *testing.T) {
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.HasPrefix(d.Name(), ".") || !strings.HasSuffix(path, ".go") {
			return err
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if out, err := format.Source(src); err != nil {
			t.Errorf("%s: %v", path, err)
		} else if !bytes.Equal(out, src) {
			t.Errorf("%s is not formatted: run \"$(go env GOROOT)/bin/gofmt\" -w apps/native", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
