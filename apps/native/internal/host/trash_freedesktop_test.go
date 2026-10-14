package host

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFreedesktopTrashKeepsTheFolderAndItsTrashInfo(t *testing.T) {
	data := t.TempDir()
	src := filepath.Join(t.TempDir(), "chats", "2026-10-14-ab12cd")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "notes.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	trash := &FreedesktopTrash{Dir: filepath.Join(data, "Trash"), Now: func() time.Time { return time.Date(2026, 10, 14, 9, 30, 5, 0, time.UTC) }}
	if err := trash.Move(src); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("the folder is still at %s: %v", src, err)
	}
	if b, err := os.ReadFile(filepath.Join(data, "Trash", "files", "2026-10-14-ab12cd", "notes.txt")); err != nil || string(b) != "hi" {
		t.Fatalf("trashed file: %q %v", b, err)
	}
	info, err := os.ReadFile(filepath.Join(data, "Trash", "info", "2026-10-14-ab12cd.trashinfo"))
	if err != nil {
		t.Fatal(err)
	}
	want := "[Trash Info]\nPath=" + src + "\nDeletionDate=2026-10-14T09:30:05\n"
	if string(info) != want {
		t.Fatalf("trash info:\n%s\nwant:\n%s", info, want)
	}
}

func TestFreedesktopTrashNamesDoNotCollide(t *testing.T) {
	trash := &FreedesktopTrash{Dir: filepath.Join(t.TempDir(), "Trash")}
	parent := t.TempDir()
	for range 2 {
		src := filepath.Join(parent, "2026-10-14-ab12cd")
		if err := os.MkdirAll(src, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := trash.Move(src); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"2026-10-14-ab12cd", "2026-10-14-ab12cd.2"} {
		if _, err := os.Stat(filepath.Join(trash.Dir, "files", name)); err != nil {
			t.Errorf("files/%s: %v", name, err)
		}
		if _, err := os.Stat(filepath.Join(trash.Dir, "info", name+".trashinfo")); err != nil {
			t.Errorf("info/%s.trashinfo: %v", name, err)
		}
	}
}

func TestFreedesktopTrashEncodesThePathInTheInfo(t *testing.T) {
	trash := &FreedesktopTrash{Dir: filepath.Join(t.TempDir(), "Trash")}
	src := filepath.Join(t.TempDir(), "my chats %1", "2026-10-14-ab12cd")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := trash.Move(src); err != nil {
		t.Fatal(err)
	}
	info, _ := os.ReadFile(filepath.Join(trash.Dir, "info", "2026-10-14-ab12cd.trashinfo"))
	if !strings.Contains(string(info), "/my%20chats%20%251/2026-10-14-ab12cd\n") {
		t.Fatalf("path not percent-encoded:\n%s", info)
	}
}
