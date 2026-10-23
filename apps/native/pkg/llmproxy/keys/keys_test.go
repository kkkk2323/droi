package keys

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keys.txt")
	content := "key-aaa\nkey-bbb\n\n# comment\nkey-ccc\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	keys, err := LoadFromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 3 {
		t.Fatalf("expected 3 keys, got %d", len(keys))
	}
	if keys[0] != "key-aaa" || keys[1] != "key-bbb" || keys[2] != "key-ccc" {
		t.Fatalf("unexpected keys: %v", keys)
	}
}

func TestLoadFromFile_BlankLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keys.txt")
	content := "\n\n\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	keys, err := LoadFromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 0 {
		t.Fatalf("expected 0 keys, got %d", len(keys))
	}
}

func TestLoadFromFile_NotFound(t *testing.T) {
	_, err := LoadFromFile("/nonexistent/keys.txt")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}
