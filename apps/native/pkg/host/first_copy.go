package host

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/kkkk2323/droi/apps/native/pkg/brand"
)

// factoryImportedMarker is written into the user's directory once the first
// copy from ~/.factory has succeeded. Its presence, not the presence of what was
// copied, is what makes the copy happen once: a user who deletes the skills
// folder afterwards does not get it back at the next start.
const factoryImportedMarker = ".factory-imported"

// FirstCopy says what a copy from ~/.factory brought over, for a log line and
// for the Settings page that offers it again.
type FirstCopy struct {
	// Skills is how many skill folders (or single-file skills) were copied.
	Skills int
	// MCP says the global mcp.json came over.
	MCP bool
	// Agents says the global AGENTS.md came over.
	Agents bool
	// Skipped names what could not be copied and was left out on purpose: a
	// link that points nowhere, a device. Paths are relative to ~/.factory.
	Skipped []string
}

// Empty reports that nothing was copied: everything was already Droi's, which
// is the usual answer after the first start.
func (c FirstCopy) Empty() bool { return c.Skills == 0 && !c.MCP && !c.Agents }

// CopyFactoryHome copies the droid CLI's three user-level things into the
// user's own directory, once: the personal skills, the global mcp.json and the
// global AGENTS.md (Q11).
//
// Once is a marker in the user's directory, written after a copy that
// succeeded, so a user who edits or deletes what came over keeps that choice. A
// copy that failed part way writes no marker; the next start copies what is
// still missing. A source that is not there is not an error: a computer that
// never ran droid simply has nothing to bring over.
//
// It is a copy rather than a link because from here on the folder is Droi's:
// Kernel writes its own mcp.json there when the user adds a server, and a link
// would put that into the droid CLI's file.
func CopyFactoryHome(factoryDir, userDir string) (FirstCopy, error) {
	if factoryDir == "" || userDir == "" {
		return FirstCopy{}, errors.New("host: copy the user's folder: no source or no target")
	}
	if _, err := os.Stat(filepath.Join(userDir, factoryImportedMarker)); err == nil {
		return FirstCopy{}, nil
	}
	return ImportFactoryHome(factoryDir, userDir)
}

// ImportFactoryHome is Settings' "import again": it copies every skill, and
// mcp.json and AGENTS.md, that the user's directory does not have, whatever was
// copied before. What is already there is never replaced.
func ImportFactoryHome(factoryDir, userDir string) (FirstCopy, error) {
	var copied FirstCopy
	if factoryDir == "" || userDir == "" {
		return copied, errors.New("host: copy the user's folder: no source or no target")
	}
	if err := os.MkdirAll(userDir, 0o755); err != nil {
		return copied, err
	}
	var errs []error
	if err := copySkills(factoryDir, filepath.Join(userDir, "skills"), &copied); err != nil {
		errs = append(errs, err)
	}
	for _, name := range []string{"mcp.json", "AGENTS.md"} {
		ok, err := copyFileIfAbsent(filepath.Join(factoryDir, name), filepath.Join(userDir, name))
		if err != nil {
			errs = append(errs, err)
		}
		if name == "mcp.json" {
			copied.MCP = ok
		} else {
			copied.Agents = ok
		}
	}
	if err := errors.Join(errs...); err != nil {
		return copied, err
	}
	if err := os.WriteFile(filepath.Join(userDir, factoryImportedMarker), nil, 0o644); err != nil {
		return copied, err
	}
	return copied, nil
}

// CopyFactoryHome is the Host's first-start call: from the droid CLI's
// ~/.factory into the user's directory, ~/.droi. It runs before Kernel starts,
// so Kernel never sees a half-filled folder as the user's choice.
func (h *Host) CopyFactoryHome() (FirstCopy, error) {
	return CopyFactoryHome(filepath.Join(h.cfg.Home, ".factory"), brand.UserFolder(h.cfg.Home))
}

// ImportFactoryHome is the Host's "import again", for Settings.
func (h *Host) ImportFactoryHome() (FirstCopy, error) {
	return ImportFactoryHome(filepath.Join(h.cfg.Home, ".factory"), brand.UserFolder(h.cfg.Home))
}

// copySkills copies each skill under factoryDir/skills that dst does not have.
// A skill is one entry of that folder — a folder with its SKILL.md, or a single
// file — and is copied whole or not at all: it is written beside its place and
// renamed into it, so a copy that stops part way leaves nothing a reader could
// mistake for a skill.
func copySkills(factoryDir, dst string, copied *FirstCopy) error {
	src := filepath.Join(factoryDir, "skills")
	entries, err := os.ReadDir(src)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	var errs []error
	for _, e := range entries {
		name := e.Name()
		if name == "" || name[0] == '.' {
			continue
		}
		target := filepath.Join(dst, name)
		if _, err := os.Lstat(target); err == nil {
			continue
		}
		from := filepath.Join(src, name)
		// Stat, not Lstat: a skill that is a link to the user's own checkout is a
		// skill the user uses, and the copy takes what the link names.
		info, err := os.Stat(from)
		if err != nil || !(info.IsDir() || info.Mode().IsRegular()) {
			copied.Skipped = append(copied.Skipped, filepath.Join("skills", name))
			continue
		}
		skipped, err := placeWhole(from, target, info.IsDir())
		for _, s := range skipped {
			copied.Skipped = append(copied.Skipped, filepath.Join("skills", name, s))
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("host: copy skill %s: %w", name, err))
			continue
		}
		copied.Skills++
	}
	return errors.Join(errs...)
}

// placeWhole copies from into a temporary sibling of target and renames it
// into place, answering what inside it was skipped.
func placeWhole(from, target string, dir bool) ([]string, error) {
	tmp, err := os.MkdirTemp(filepath.Dir(target), "."+filepath.Base(target)+".importing-")
	if err != nil {
		return nil, err
	}
	staged := filepath.Join(tmp, "x")
	var skipped []string
	if dir {
		skipped, err = copyDir(from, staged)
	} else {
		err = copyFile(from, staged)
	}
	if err == nil {
		err = os.Rename(staged, target)
	}
	_ = os.RemoveAll(tmp)
	return skipped, err
}

// copyDir copies a folder's files and folders, following links to what they
// name, and answers the entries it left out (a link that points nowhere, a
// device), relative to from.
func copyDir(from, to string) ([]string, error) {
	var skipped []string
	var walk func(src, dst, rel string, depth int) error
	walk = func(src, dst, rel string, depth int) error {
		if depth > 32 {
			skipped = append(skipped, rel)
			return nil
		}
		if err := os.MkdirAll(dst, 0o755); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, e := range entries {
			name := e.Name()
			path := filepath.Join(src, name)
			info, err := os.Stat(path)
			switch {
			case err != nil:
				skipped = append(skipped, filepath.Join(rel, name))
			case info.IsDir():
				if err := walk(path, filepath.Join(dst, name), filepath.Join(rel, name), depth+1); err != nil {
					return err
				}
			case info.Mode().IsRegular():
				if err := copyFile(path, filepath.Join(dst, name)); err != nil {
					return err
				}
			default:
				skipped = append(skipped, filepath.Join(rel, name))
			}
		}
		return nil
	}
	return skipped, walk(from, to, "", 0)
}

// copyFileIfAbsent copies src to dst unless dst exists, whole or not at all,
// and answers whether it copied anything.
func copyFileIfAbsent(src, dst string) (bool, error) {
	if _, err := os.Lstat(dst); err == nil {
		return false, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	info, err := os.Stat(src)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, nil
	}
	if _, err := placeWhole(src, dst, false); err != nil {
		return false, fmt.Errorf("host: copy %s: %w", filepath.Base(src), err)
	}
	return true, nil
}

// copyFile writes src's bytes to a new file dst with src's permission bits, so
// an mcp.json that holds a token and is 0600 stays 0600.
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
