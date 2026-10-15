package memorywork

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
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

func TestPromptsReplaceAnUneditedCopyOfAnEarlierReleaseOnly(t *testing.T) {
	dir := t.TempDir()
	earlier := []byte("an earlier release's prompt")
	sum := sha256.Sum256(earlier)
	saved := earlierPrompts[PromptExtraction]
	earlierPrompts[PromptExtraction] = append(slices.Clone(saved), hex.EncodeToString(sum[:]))
	t.Cleanup(func() { earlierPrompts[PromptExtraction] = saved })
	file := filepath.Join(dir, "extraction-prompt.md")
	if err := os.WriteFile(file, earlier, 0o666); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadPrompt(dir, PromptExtraction); err != nil || got != shipped(t, PromptExtraction) {
		t.Fatalf("earlier prompt read as %q: %v", got, err)
	}
	if err := os.WriteFile(file, append(earlier, " Edited."...), 0o666); err != nil {
		t.Fatal(err)
	}
	if got, _ := ReadPrompt(dir, PromptExtraction); got != string(earlier)+" Edited." {
		t.Errorf("edited prompt %q", got)
	}
}

// shippedHashes pin the prompts this release ships, so a change to one
// cannot leave its old hash out of earlierPrompts.
var shippedHashes = map[PromptName]string{
	PromptConsolidation: "faf60cf205c7bbdec46d9938e6734ccccff2693cc7a6cfbaee10db7a8931a17c",
	PromptExtraction:    "c3ae72bfe21ece1e4d2d94b25fd25e3efdf23f258049d8882aa7de616d4a978f",
}

func TestPromptsListEveryEarlierReleasesPrompt(t *testing.T) {
	for name, want := range shippedHashes {
		sum := sha256.Sum256([]byte(shipped(t, name)))
		got := hex.EncodeToString(sum[:])
		if got != want {
			t.Errorf("%s changed: add %s to earlierPrompts and pin %s in shippedHashes", PromptFiles[name], want, got)
		}
		if isEarlierPrompt(name, []byte(shipped(t, name))) {
			t.Errorf("%s is listed as an earlier prompt", PromptFiles[name])
		}
	}
}
