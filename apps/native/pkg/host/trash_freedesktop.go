package host

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// FreedesktopTrash is the Trash of the freedesktop.org Trash specification,
// where a file manager on Linux finds what was trashed and can restore it.
type FreedesktopTrash struct {
	// Dir is the Trash folder: $XDG_DATA_HOME/Trash.
	Dir string
	Now func() time.Time
}

// Move puts path into the Trash with its .trashinfo. It renames, so path
// must be on the same file system as Dir (a Scratch folder under the home
// folder is).
func (t *FreedesktopTrash) Move(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	files, infos := filepath.Join(t.Dir, "files"), filepath.Join(t.Dir, "info")
	for _, d := range []string{files, infos} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return err
		}
	}
	now := time.Now
	if t.Now != nil {
		now = t.Now
	}
	body := "[Trash Info]\nPath=" + encodeTrashPath(abs) + "\nDeletionDate=" + now().Format("2006-01-02T15:04:05") + "\n"
	base := filepath.Base(abs)
	for n := 1; ; n++ {
		name := base
		if n > 1 {
			name += "." + strconv.Itoa(n)
		}
		// The info file reserves the name, so two trashings cannot share one.
		info := filepath.Join(infos, name+".trashinfo")
		f, err := os.OpenFile(info, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return err
		}
		_, werr := f.WriteString(body)
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		if werr == nil {
			dest := filepath.Join(files, name)
			if _, err := os.Lstat(dest); err == nil {
				werr = os.ErrExist
			} else if err := os.Rename(abs, dest); err != nil {
				werr = err
			}
		}
		if werr != nil {
			_ = os.Remove(info)
			if errors.Is(werr, os.ErrExist) {
				continue
			}
			return werr
		}
		return nil
	}
}

// encodeTrashPath is the percent-encoding the specification asks of Path=:
// everything but unreserved characters and "/".
func encodeTrashPath(p string) string {
	var b strings.Builder
	for _, c := range []byte(filepath.ToSlash(p)) {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', strings.IndexByte("-_.~/", c) >= 0:
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}
