package host

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/kkkk2323/droi/apps/native/v2/pkg/brand"
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
	// Differs names what both folders have, with different contents: Droi's
	// was kept, and ReplaceFromFactoryHome takes droid's in its place when the
	// user chooses that. Paths are relative to ~/.factory.
	Differs []string
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
		ok, differs, err := copyFileIfAbsent(filepath.Join(factoryDir, name), filepath.Join(userDir, name))
		if err != nil {
			errs = append(errs, err)
		}
		if differs {
			copied.Differs = append(copied.Differs, name)
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

// ReplaceFromFactoryHome is Settings' answer to what the import found different: droid's copies of names in
// place of Droi's, which move into the folder it answers.
func (h *Host) ReplaceFromFactoryHome(names []string) (string, error) {
	return ReplaceFromFactoryHome(filepath.Join(h.cfg.Home, ".factory"), brand.UserFolder(h.cfg.Home), names, time.Now())
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
		from := filepath.Join(src, name)
		if _, err := os.Lstat(target); err == nil {
			if !sameContent(from, target, 0) {
				copied.Differs = append(copied.Differs, filepath.Join("skills", name))
			}
			continue
		}
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
// and answers whether it copied anything, and whether a dst that was there
// differs from src.
func copyFileIfAbsent(src, dst string) (copied, differs bool, err error) {
	if _, err := os.Lstat(dst); err == nil {
		return false, !sameContent(src, dst, 0), nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return false, false, err
	}
	info, err := os.Stat(src)
	if errors.Is(err, fs.ErrNotExist) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	if !info.Mode().IsRegular() {
		return false, false, nil
	}
	if _, err := placeWhole(src, dst, false); err != nil {
		return false, false, fmt.Errorf("host: copy %s: %w", filepath.Base(src), err)
	}
	return true, false, nil
}

// sameContent reports whether src, as the copy would read it (links
// followed), holds what dst holds: the same files with the same bytes. A src
// that cannot be read is no difference worth asking about.
func sameContent(src, dst string, depth int) bool {
	si, err := os.Stat(src)
	if err != nil {
		return true
	}
	di, err := os.Stat(dst)
	if err != nil || si.IsDir() != di.IsDir() || depth > 32 {
		return false
	}
	if !si.IsDir() {
		if !si.Mode().IsRegular() {
			return true
		}
		if si.Size() != di.Size() {
			return false
		}
		a, errA := os.ReadFile(src)
		b, errB := os.ReadFile(dst)
		return errA == nil && errB == nil && bytes.Equal(a, b)
	}
	se, errS := os.ReadDir(src)
	de, errD := os.ReadDir(dst)
	if errS != nil || errD != nil {
		return false
	}
	names := map[string]bool{}
	for _, e := range de {
		names[e.Name()] = true
	}
	for _, e := range se {
		path := filepath.Join(src, e.Name())
		if info, err := os.Stat(path); err != nil || !(info.IsDir() || info.Mode().IsRegular()) {
			continue
		}
		if !names[e.Name()] || !sameContent(path, filepath.Join(dst, e.Name()), depth+1) {
			return false
		}
		delete(names, e.Name())
	}
	return len(names) == 0
}

// replacedFolder holds, by when, what ReplaceFromFactoryHome moved out of the
// way, inside the user's directory.
const replacedFolder = ".replaced"

// ReplaceFromFactoryHome puts droid's copy of each of names (as Differs names
// them) in place of Droi's. Droi's is not deleted: it moves into
// <userDir>/.replaced/<time>/, which it answers, so the user can take it back.
func ReplaceFromFactoryHome(factoryDir, userDir string, names []string, now time.Time) (string, error) {
	if factoryDir == "" || userDir == "" {
		return "", errors.New("host: replace from ~/.factory: no source or no target")
	}
	for _, name := range names {
		if !importable(name) {
			return "", fmt.Errorf("host: replace from ~/.factory: %q is not something the import brings over", name)
		}
	}
	if err := os.MkdirAll(filepath.Join(userDir, replacedFolder), 0o755); err != nil {
		return "", err
	}
	// A folder of its own each time: two replaces within a second must not put the second's over the first's.
	backup, err := os.MkdirTemp(filepath.Join(userDir, replacedFolder), now.Format("2006-01-02T15-04-05")+"-")
	if err != nil {
		return "", err
	}
	var errs []error
	for _, name := range names {
		from, target, kept := filepath.Join(factoryDir, name), filepath.Join(userDir, name), filepath.Join(backup, name)
		info, err := os.Stat(from)
		if err != nil || !(info.IsDir() || info.Mode().IsRegular()) {
			errs = append(errs, fmt.Errorf("host: replace %s: droid's copy cannot be read: %v", name, err))
			continue
		}
		if err := os.MkdirAll(filepath.Dir(kept), 0o755); err != nil {
			errs = append(errs, err)
			continue
		}
		if err := os.Rename(target, kept); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, fmt.Errorf("host: replace %s: %w", name, err))
			continue
		}
		if _, err := placeWhole(from, target, info.IsDir()); err != nil {
			// Droi's own comes back rather than leaving nothing in its place.
			_ = os.Rename(kept, target)
			errs = append(errs, fmt.Errorf("host: replace %s: %w", name, err))
		}
	}
	return backup, errors.Join(errs...)
}

// importable reports whether name is one of the things the import brings
// over: mcp.json, AGENTS.md or one skill.
func importable(name string) bool {
	if name == "mcp.json" || name == "AGENTS.md" {
		return true
	}
	dir, skill := filepath.Split(name)
	return filepath.Clean(dir) == "skills" && skill != "" && skill[0] != '.' && skill != ".." &&
		filepath.Base(skill) == skill
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
