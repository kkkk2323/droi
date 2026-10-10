package app

import (
	"strings"

	"github.com/egoist/mygo/transfer"
)

// imageFormats are the pictures another app drags: a browser's carries
// its image as PNG or JPEG, Safari's as TIFF.
var imageFormats = []transfer.Format{transfer.PNG, "image/jpeg", "image/gif", "image/webp", "image/tiff"}

// dropOptions are what the composers take from other apps: files, an
// image, URLs or text.
var dropOptions = transfer.DropOptions{
	Formats:    append(append([]transfer.Format{transfer.FileList}, imageFormats...), transfer.URIList, transfer.Text),
	Operations: transfer.Copy,
}

// dropped is what a drop on a composer adds.
type dropped struct {
	files []string
	image []byte
	text  string
}

// readDrop takes a drop's files, else its image, else its URLs or text:
// Finder's files carry their names as text too, and a browser's image its
// URL.
func readDrop(d transfer.Data) dropped {
	if files, err := d.Files(); err == nil && len(files) > 0 {
		return dropped{files: files}
	}
	for _, f := range imageFormats {
		if b, err := d.Read(f); err == nil && len(b) > 0 {
			return dropped{image: b}
		}
	}
	if urls, err := d.URLs(); err == nil && len(urls) > 0 {
		return dropped{text: strings.Join(urls, " ")}
	}
	if b, err := d.Read(transfer.Text); err == nil {
		return dropped{text: strings.TrimSpace(string(b))}
	}
	return dropped{}
}

// readDrop is readDrop with the file references among the files turned
// into paths.
func (a *App) readDrop(d transfer.Data) dropped {
	r := readDrop(d)
	if a.cfg.FilePath == nil {
		return r
	}
	for i, f := range r.files {
		if strings.HasPrefix(f, "/.file/id=") {
			if p := a.cfg.FilePath(f); p != "" {
				r.files[i] = p
			}
		}
	}
	return r
}

// appendWords adds words to a message, a space apart from what it has.
func appendWords(text, words string) string {
	if words == "" {
		return text
	}
	if text != "" && !strings.HasSuffix(text, " ") && !strings.HasSuffix(text, "\n") {
		words = " " + words
	}
	return text + words + " "
}
