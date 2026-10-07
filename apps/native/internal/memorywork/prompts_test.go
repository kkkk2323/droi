package memorywork

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func shipped(t *testing.T, name PromptName) string {
	t.Helper()
	b, err := shippedPrompts.ReadFile("prompts/" + PromptFiles[name])
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestPromptsCopyTheShippedPromptOnFirstUseThenReadTheCopy(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "memory")
	if got, err := ReadPrompt(dir, PromptExtraction); err != nil || got != shipped(t, PromptExtraction) {
		t.Fatalf("first read %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "extraction-prompt.md"), []byte("my own prompt"), 0o666); err != nil {
		t.Fatal(err)
	}
	if got, _ := ReadPrompt(dir, PromptExtraction); got != "my own prompt" {
		t.Errorf("edited prompt %q", got)
	}
	if got, _ := ReadPrompt(dir, PromptConsolidation); !strings.Contains(got, "Every id you were sent") {
		t.Errorf("consolidation prompt %q", got)
	}
}

func TestPromptsResetBothPromptsToTheShippedOnes(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "consolidation-prompt.md"), []byte("edited"), 0o666); err != nil {
		t.Fatal(err)
	}
	if err := ResetPrompts(dir); err != nil {
		t.Fatal(err)
	}
	if got, _ := ReadPrompt(dir, PromptConsolidation); got != shipped(t, PromptConsolidation) {
		t.Errorf("reset prompt %q", got)
	}
}

// The embedded prompts are copies of the Desktop Shell's resources, which
// go:embed cannot reach.
func TestEmbeddedPromptsMatchTheDesktopShellResources(t *testing.T) {
	desktop := filepath.Join("..", "..", "..", "desktop", "resources", "memory")
	if _, err := os.Stat(desktop); err != nil {
		t.Skip("no Desktop Shell resources here")
	}
	for name, file := range PromptFiles {
		want, err := os.ReadFile(filepath.Join(desktop, file))
		if err != nil {
			t.Fatal(err)
		}
		if shipped(t, name) != string(want) {
			t.Errorf("prompts/%s differs from %s; copy it again", file, filepath.Join(desktop, file))
		}
	}
}
