package host

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/kkkk2323/droi/apps/native/pkg/brand"
)

// FirstCopy says what CopyFactoryHome brought over, for a log line and for the
// tests. A field is false or zero when there was nothing to copy or the target
// already existed.
type FirstCopy struct {
	// Skills is how many skill files were copied from the folder.
	Skills int
	// MCP says the global mcp.json came over.
	MCP bool
	// Agents says the global AGENTS.md came over.
	Agents bool
}

// Empty reports that nothing was copied: everything was already Droi's, which
// is the usual answer after the first start.
func (c FirstCopy) Empty() bool { return c.Skills == 0 && !c.MCP && !c.Agents }

// CopyFactoryHome copies the droid CLI's three user-level things into the
// user's own directory, once: the personal skills, the global mcp.json and the
// global AGENTS.md (Q11).
//
// It copies a thing only when the target does not exist, so running it twice is
// running it once, and a user who edited or deleted what came over keeps their
// version. A source that is not there is not an error: a computer that never ran
// droid simply has nothing to bring over.
//
// The three are what Kernel reads from DROI_USER_DIR. It is a copy rather than a
// link because from here on the folder is Droi's: Kernel writes its own mcp.json
// there when the user adds a server, and a link would put that into the droid
// CLI's file. ADR 0017's per-account folders are a different thing — the Daemon's
// ~/.factory by account — and are not used here.
func CopyFactoryHome(factoryDir, userDir string) (FirstCopy, error) {
	var copied FirstCopy
	if factoryDir == "" || userDir == "" {
		return copied, errors.New("host: copy the user's folder: no source or no target")
	}
	if err := os.MkdirAll(userDir, 0o755); err != nil {
		return copied, err
	}

	skills, err := copyTree(filepath.Join(factoryDir, "skills"), filepath.Join(userDir, "skills"))
	copied.Skills = skills
	if err != nil {
		return copied, err
	}
	if copied.MCP, err = copyFileIfAbsent(filepath.Join(factoryDir, "mcp.json"), filepath.Join(userDir, "mcp.json")); err != nil {
		return copied, err
	}
	if copied.Agents, err = copyFileIfAbsent(filepath.Join(factoryDir, "AGENTS.md"), filepath.Join(userDir, "AGENTS.md")); err != nil {
		return copied, err
	}
	return copied, nil
}

// CopyFactoryHome is the Host's own call: from the droid CLI's ~/.factory into
// the user's directory, ~/.droi.
func (h *Host) CopyFactoryHome() (FirstCopy, error) {
	return CopyFactoryHome(filepath.Join(h.cfg.Home, ".factory"), brand.UserFolder(h.cfg.Home))
}

// copyTree copies src into dst unless dst is already there, and answers how many
// files it copied.
func copyTree(src, dst string) (int, error) {
	if _, err := os.Lstat(dst); err == nil {
		return 0, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return 0, err
	}
	if _, err := os.Lstat(src); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	copied := 0
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		switch {
		case d.IsDir():
			return os.MkdirAll(target, 0o755)
		case d.Type().IsRegular():
			if err := copyFile(path, target); err != nil {
				return err
			}
			copied++
			return nil
		default:
			// A link or a device in a skills folder is not something to copy
			// into Droi's: the files it names are, and the user can add those.
			return nil
		}
	})
	if err != nil {
		return copied, fmt.Errorf("host: copy %s: %w", src, err)
	}
	return copied, nil
}

// copyFileIfAbsent copies src to dst unless dst exists, and answers whether it
// copied anything.
func copyFileIfAbsent(src, dst string) (bool, error) {
	if _, err := os.Lstat(dst); err == nil {
		return false, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	if _, err := os.Lstat(src); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	if err := copyFile(src, dst); err != nil {
		return false, err
	}
	return true, nil
}

// copyFile writes src's bytes to dst, creating dst's folder when it is not there.
func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	mode := info.Mode().Perm()
	if mode == 0 {
		mode = 0o644
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(dst)
		return err
	}
	return out.Close()
}
