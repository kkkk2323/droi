package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ebitengine/purego/objc"
)

func TestFilePath(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "report 1.pdf")
	if err := os.WriteFile(file, []byte("%PDF"), 0o600); err != nil {
		t.Fatal(err)
	}
	url := objc.ID(objc.GetClass("NSURL")).Send(objc.RegisterName("fileURLWithPath:"), nsString(file))
	ref := url.Send(objc.RegisterName("fileReferenceURL")).Send(objc.RegisterName("absoluteString"))
	refPath := strings.TrimPrefix(objc.Send[string](ref, objc.RegisterName("UTF8String")), "file://")
	if !strings.HasPrefix(refPath, "/.file/id=") {
		t.Fatalf("reference = %q", refPath)
	}
	if got := filePath(refPath); got != file {
		t.Errorf("filePath(%q) = %q, want %q", refPath, got, file)
	}
	os.Remove(file)
	if got := filePath(refPath); got != "" {
		t.Errorf("filePath of a removed file = %q", got)
	}
}
