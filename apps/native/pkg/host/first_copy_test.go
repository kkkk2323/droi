package host

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/kernel"
)

// write puts a file, creating its folder.
func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The three user-level things come over once, and only where nothing is there
// yet: a second call changes nothing, and a target the user already has wins.
func TestFirstCopyBringsTheUsersThreeThingsOverOnce(t *testing.T) {
	factory := t.TempDir()
	user := t.TempDir()
	write(t, filepath.Join(factory, "skills", "gofmt", "SKILL.md"), "---\nname: gofmt\n---\n")
	write(t, filepath.Join(factory, "skills", "review", "SKILL.md"), "---\nname: review\n---\n")
	write(t, filepath.Join(factory, "mcp.json"), `{"mcpServers":{"github":{"command":"gh"}}}`)
	write(t, filepath.Join(factory, "AGENTS.md"), "be careful\n")

	copied, err := CopyFactoryHome(factory, user)
	if err != nil {
		t.Fatal(err)
	}
	if copied.Skills != 2 || !copied.MCP || !copied.Agents {
		t.Fatalf("first copy: %+v", copied)
	}
	for _, want := range []struct{ path, body string }{
		{filepath.Join(user, "skills", "gofmt", "SKILL.md"), "---\nname: gofmt\n---\n"},
		{filepath.Join(user, "skills", "review", "SKILL.md"), "---\nname: review\n---\n"},
		{filepath.Join(user, "mcp.json"), `{"mcpServers":{"github":{"command":"gh"}}}`},
		{filepath.Join(user, "AGENTS.md"), "be careful\n"},
	} {
		got, err := os.ReadFile(want.path)
		if err != nil {
			t.Fatalf("%s: %v", want.path, err)
		}
		if string(got) != want.body {
			t.Fatalf("%s = %q, want %q", want.path, got, want.body)
		}
	}

	// What Droi wrote after the copy is Droi's, and droid's newer file does not
	// overwrite it.
	write(t, filepath.Join(user, "mcp.json"), `{"mcpServers":{"mine":{"command":"x"}}}`)
	write(t, filepath.Join(factory, "mcp.json"), `{"mcpServers":{"newer":{"command":"y"}}}`)
	write(t, filepath.Join(user, "skills", "gofmt", "SKILL.md"), "edited\n")
	copied, err = CopyFactoryHome(factory, user)
	if err != nil {
		t.Fatal(err)
	}
	if !copied.Empty() {
		t.Fatalf("the second copy brought something over: %+v", copied)
	}
	if got, _ := os.ReadFile(filepath.Join(user, "mcp.json")); string(got) != `{"mcpServers":{"mine":{"command":"x"}}}` {
		t.Fatalf("the second copy replaced mcp.json with %q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(user, "skills", "gofmt", "SKILL.md")); string(got) != "edited\n" {
		t.Fatalf("the second copy replaced the edited skill with %q", got)
	}
}

// A computer that never ran droid has nothing to bring over, and that is not a
// failure: the user's own folder is simply empty until they add to it.
func TestFirstCopyWithNoDroidFolderIsNothing(t *testing.T) {
	user := t.TempDir()
	copied, err := CopyFactoryHome(filepath.Join(t.TempDir(), "no-factory"), user)
	if err != nil {
		t.Fatal(err)
	}
	if !copied.Empty() {
		t.Fatalf("a missing ~/.factory copied %+v", copied)
	}
	entries, err := os.ReadDir(user)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != factoryImportedMarker {
		t.Fatalf("a missing ~/.factory left %d entries behind, want only the marker", len(entries))
	}
}

// The Host's own call goes from the droid CLI's ~/.factory into the brand's
// folder, and never the other way round.
func TestTheHostCopiesIntoItsOwnFolder(t *testing.T) {
	home := t.TempDir()
	write(t, filepath.Join(home, ".factory", "AGENTS.md"), "from droid\n")
	h := New(Config{UserData: t.TempDir(), Home: home, Env: func(string) string { return "" }})
	copied, err := h.CopyFactoryHome()
	if err != nil {
		t.Fatal(err)
	}
	if !copied.Agents {
		t.Fatalf("AGENTS.md did not come over: %+v", copied)
	}
	if got, err := os.ReadFile(filepath.Join(home, ".droi", "AGENTS.md")); err != nil || string(got) != "from droid\n" {
		t.Fatalf(".droi/AGENTS.md = %q, %v", got, err)
	}
}

// Deleting what came over is the user's choice: the next start does not bring
// it back, and only "import again" does.
func TestADeletedSkillsFolderStaysDeletedUntilImportedAgain(t *testing.T) {
	factory := t.TempDir()
	user := t.TempDir()
	write(t, filepath.Join(factory, "skills", "gofmt", "SKILL.md"), "gofmt\n")
	write(t, filepath.Join(factory, "mcp.json"), "{}")
	if _, err := CopyFactoryHome(factory, user); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(user, "skills")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(user, "mcp.json")); err != nil {
		t.Fatal(err)
	}
	copied, err := CopyFactoryHome(factory, user)
	if err != nil || !copied.Empty() {
		t.Fatalf("the next start copied %+v, %v", copied, err)
	}
	if _, err := os.Stat(filepath.Join(user, "skills", "gofmt")); err == nil {
		t.Fatal("a deleted skill came back at the next start")
	}
	copied, err = ImportFactoryHome(factory, user)
	if err != nil || copied.Skills != 1 || !copied.MCP {
		t.Fatalf("import again copied %+v, %v", copied, err)
	}
}

// A skill that is a link — to a checkout the user works in — is copied as what
// it names; a link that names nothing is left out and said so.
func TestLinkedSkillsAreFollowedAndBrokenOnesNamed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating a symbolic link needs a privilege on Windows")
	}
	factory := t.TempDir()
	user := t.TempDir()
	checkout := t.TempDir()
	write(t, filepath.Join(checkout, "browser", "SKILL.md"), "browser\n")
	write(t, filepath.Join(checkout, "browser", "ref", "a.md"), "a\n")
	if err := os.MkdirAll(filepath.Join(factory, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(checkout, "browser"), filepath.Join(factory, "skills", "browser")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(checkout, "gone"), filepath.Join(factory, "skills", "gone")); err != nil {
		t.Fatal(err)
	}
	copied, err := CopyFactoryHome(factory, user)
	if err != nil {
		t.Fatal(err)
	}
	if copied.Skills != 1 || len(copied.Skipped) != 1 || copied.Skipped[0] != filepath.Join("skills", "gone") {
		t.Fatalf("copied %+v", copied)
	}
	got, err := os.ReadFile(filepath.Join(user, "skills", "browser", "ref", "a.md"))
	if err != nil || string(got) != "a\n" {
		t.Fatalf("the linked skill's file = %q, %v", got, err)
	}
	if info, err := os.Lstat(filepath.Join(user, "skills", "browser")); err != nil || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("the copy is a link, not Droi's own folder: %v", err)
	}
}

// A copy that fails part way leaves no half skill and no marker, so the next
// start copies what is still missing.
func TestAFailedCopyIsFinishedAtTheNextStart(t *testing.T) {
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Skip("needs a file this user cannot read")
	}
	factory := t.TempDir()
	user := t.TempDir()
	write(t, filepath.Join(factory, "skills", "a", "SKILL.md"), "a\n")
	write(t, filepath.Join(factory, "skills", "b", "SKILL.md"), "b\n")
	locked := filepath.Join(factory, "skills", "b", "SKILL.md")
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o644) })
	if _, err := CopyFactoryHome(factory, user); err == nil {
		t.Fatal("an unreadable skill was not an error")
	}
	entries, _ := os.ReadDir(filepath.Join(user, "skills"))
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if len(names) != 1 || names[0] != "a" {
		t.Fatalf("after the failed copy the skills folder holds %v, want only a", names)
	}
	if err := os.Chmod(locked, 0o644); err != nil {
		t.Fatal(err)
	}
	copied, err := CopyFactoryHome(factory, user)
	if err != nil || copied.Skills != 1 {
		t.Fatalf("the next start copied %+v, %v", copied, err)
	}
	if got, _ := os.ReadFile(filepath.Join(user, "skills", "b", "SKILL.md")); string(got) != "b\n" {
		t.Fatalf("b = %q", got)
	}
}

// An mcp.json that holds a token is private, and stays private.
func TestTheCopiedMCPFileKeepsItsPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits")
	}
	factory := t.TempDir()
	user := t.TempDir()
	write(t, filepath.Join(factory, "mcp.json"), `{"token":"x"}`)
	if err := os.Chmod(filepath.Join(factory, "mcp.json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := CopyFactoryHome(factory, user); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(user, "mcp.json"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mcp.json mode %v, %v", info.Mode().Perm(), err)
	}
}

// A launcher that already chose the Kernel's user directory — a test's own
// temporary one — keeps it: the Supervisor's default must not replace it.
func TestALaunchersUserDirectoryWins(t *testing.T) {
	env := withUserDir([]string{"A=1", "DROI_USER_DIR=/tmp/test-own"}, "/home/real/.droi")
	n := 0
	for _, kv := range env {
		if strings.HasPrefix(kv, "DROI_USER_DIR=") {
			n++
			if kv != "DROI_USER_DIR=/tmp/test-own" {
				t.Fatalf("got %s", kv)
			}
		}
	}
	if n != 1 {
		t.Fatalf("%d DROI_USER_DIR entries", n)
	}
	if env := withUserDir(nil, "/x"); len(env) < 2 {
		t.Fatalf("a launcher without an environment left the Kernel with only %v", env)
	}
}

// Import again names what both folders have and differ in, keeping Droi's; replacing takes droid's and moves Droi's
// aside, where the user can take it back. What is the same is no difference.
func TestImportAgainNamesWhatDiffersAndReplacingKeepsDroisAside(t *testing.T) {
	factory := t.TempDir()
	user := t.TempDir()
	write(t, filepath.Join(factory, "skills", "gofmt", "SKILL.md"), "gofmt\n")
	write(t, filepath.Join(factory, "skills", "same", "SKILL.md"), "same\n")
	write(t, filepath.Join(factory, "AGENTS.md"), "droid's\n")
	write(t, filepath.Join(factory, "mcp.json"), "{}")
	if _, err := CopyFactoryHome(factory, user); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(user, "skills", "gofmt", "SKILL.md"), "Droi's edit\n")
	write(t, filepath.Join(user, "AGENTS.md"), "Droi's\n")
	write(t, filepath.Join(factory, "skills", "gofmt", "extra.md"), "more\n")

	copied, err := ImportFactoryHome(factory, user)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join("skills", "gofmt"), "AGENTS.md"}
	if !slices.Equal(copied.Differs, want) || !copied.Empty() {
		t.Fatalf("import again: %+v, want differs %v and nothing copied", copied, want)
	}
	if got, _ := os.ReadFile(filepath.Join(user, "AGENTS.md")); string(got) != "Droi's\n" {
		t.Fatalf("import again replaced AGENTS.md with %q", got)
	}

	at := time.Date(2026, 11, 8, 9, 30, 0, 0, time.UTC)
	backup, err := ReplaceFromFactoryHome(factory, user, copied.Differs, at)
	if err != nil {
		t.Fatal(err)
	}
	if backup != filepath.Join(user, ".replaced", "2026-11-08T09-30-00") {
		t.Fatalf("backup in %s", backup)
	}
	for _, f := range []struct{ path, body string }{
		{filepath.Join(user, "AGENTS.md"), "droid's\n"},
		{filepath.Join(user, "skills", "gofmt", "extra.md"), "more\n"},
		{filepath.Join(backup, "AGENTS.md"), "Droi's\n"},
		{filepath.Join(backup, "skills", "gofmt", "SKILL.md"), "Droi's edit\n"},
	} {
		if got, err := os.ReadFile(f.path); err != nil || string(got) != f.body {
			t.Fatalf("%s = %q, %v; want %q", f.path, got, err, f.body)
		}
	}
	if copied, _ := ImportFactoryHome(factory, user); len(copied.Differs) != 0 {
		t.Fatalf("after replacing, import again still finds %v", copied.Differs)
	}
}

// Replacing takes only what the import brings over: never a path out of the two folders.
func TestReplacingRefusesWhatTheImportDoesNotBring(t *testing.T) {
	factory := t.TempDir()
	user := t.TempDir()
	for _, name := range []string{"../x", "skills", filepath.Join("skills", ".."), filepath.Join("skills", "a", "b"), "settings.json", filepath.Join("skills", ".hidden")} {
		if _, err := ReplaceFromFactoryHome(factory, user, []string{name}, time.Now()); err == nil {
			t.Errorf("replacing %q was not refused", name)
		}
	}
}

// The Host copies ~/.factory into the brand's folder before its Kernel starts, and tells the Kernel that folder:
// the Kernel's first skills list already has what droid had.
func TestTheHostsKernelStartsWithWhatDroidHad(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the Kernel")
	}
	home := t.TempDir()
	write(t, filepath.Join(home, ".factory", "skills", "gofmt", "SKILL.md"), "---\nname: gofmt\ndescription: Format Go\n---\nRun gofmt.\n")
	env := map[string]string{KernelEnv: "1", KernelBinEnv: kernelBinaryForTest(t)}
	h := New(Config{UserData: t.TempDir(), Home: home, Env: func(k string) string { return env[k] }})
	t.Cleanup(h.KeyPool().Stop)
	clients := make(chan *kernel.Client, 4)
	link := NewKernelLink(KernelLinkOptions{
		Client:   "window",
		Token:    h.KernelToken,
		OnClient: func(c *kernel.Client, _ int) { clients <- c },
		Log:      func(s string) { t.Log("link:", s) },
	})
	unhear := h.OnKernelState(link.Use)
	h.startKernel()
	t.Cleanup(func() {
		unhear()
		link.Close()
		h.stopKernel()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	var c *kernel.Client
	select {
	case c = <-clients:
	case <-ctx.Done():
		t.Fatal("no Kernel connection")
	}
	skills, err := c.Skills(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, s := range skills.Skills {
		names = append(names, s.Name+"@"+s.Path)
		if s.Name == "gofmt" && strings.HasPrefix(s.Path, filepath.Join(home, ".droi")) {
			return
		}
	}
	t.Fatalf("the Kernel's skills are %v, want gofmt from %s", names, filepath.Join(home, ".droi"))
}
