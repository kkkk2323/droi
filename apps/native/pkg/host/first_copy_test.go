package host

import (
	"os"
	"path/filepath"
	"testing"
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
	if len(entries) != 0 {
		t.Fatalf("a missing ~/.factory left %d entries behind", len(entries))
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
