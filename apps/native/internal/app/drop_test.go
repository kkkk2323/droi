package app

import (
	"testing"

	"github.com/egoist/mygo/transfer"
)

func TestReadDrop(t *testing.T) {
	files, err := transfer.FileData("/Users/dev/notes.md")
	if err != nil {
		t.Fatal(err)
	}
	finder := transfer.New(append(files.Items(), transfer.NewItem(transfer.Bytes(transfer.Text, []byte("notes.md"))))...)
	if d := readDrop(finder); len(d.files) != 1 || d.files[0] != "/Users/dev/notes.md" || d.text != "" {
		t.Errorf("Finder's file: %+v", d)
	}
	browser := transfer.New(transfer.NewItem(
		transfer.Bytes(transfer.URIList, []byte("https://example.com/cat.png\r\n")),
		transfer.Bytes("image/tiff", []byte("II*\x00picture")),
	))
	if d := readDrop(browser); string(d.image) != "II*\x00picture" || d.text != "" {
		t.Errorf("a browser's image: %+v", d)
	}
	link, _ := transfer.URLData("https://example.com/docs")
	if d := readDrop(link); d.text != "https://example.com/docs" {
		t.Errorf("a link: %+v", d)
	}
	text := transfer.New(transfer.NewItem(transfer.Bytes(transfer.Text, []byte("  some text\n"))))
	if d := readDrop(text); d.text != "some text" {
		t.Errorf("text: %+v", d)
	}
}

func TestAppendWords(t *testing.T) {
	for _, c := range []struct{ text, words, want string }{
		{"", "a", "a "},
		{"look at", "a", "look at a "},
		{"look at ", "a", "look at a "},
		{"line\n", "a", "line\na "},
		{"same", "", "same"},
	} {
		if got := appendWords(c.text, c.words); got != c.want {
			t.Errorf("appendWords(%q, %q) = %q, want %q", c.text, c.words, got, c.want)
		}
	}
}
