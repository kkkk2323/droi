package memorywork

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
)

// The Memory Sessions' prompts. The app ships them embedded and copies them
// into the Memory folder on first use; from then on the copies are read, so a
// user can edit them, and "Reset prompts to default" copies them again.
//
//go:embed prompts/*.md
var shippedPrompts embed.FS

// earlierPrompts are the SHA-256 of every prompt an earlier release shipped.
// A copy that still matches one was never edited, so it gets the shipped
// prompt in its place; add the old hash here whenever a prompt changes.
var earlierPrompts = map[PromptName][]string{
	PromptConsolidation: {
		"4bb5601ee72d78e6bda5dc963e473bf1c50f1f569b9f953766352641f281336f",
		"faf60cf205c7bbdec46d9938e6734ccccff2693cc7a6cfbaee10db7a8931a17c",
	},
	PromptExtraction: {
		"9ebfdf21dc5443d17547dd9468e50f8652eb12694ea29f6cc7eff16a17e06dee",
		"4598bd35c89e1c67c7d3a41a53003e6089416ae7cd2ce89ba1cde7307303e0ae",
		"c3ae72bfe21ece1e4d2d94b25fd25e3efdf23f258049d8882aa7de616d4a978f",
	},
}

func isEarlierPrompt(name PromptName, text []byte) bool {
	sum := sha256.Sum256(text)
	return slices.Contains(earlierPrompts[name], hex.EncodeToString(sum[:]))
}

// PromptName names one Memory Session prompt.
type PromptName string

const (
	PromptConsolidation PromptName = "consolidation"
	PromptExtraction    PromptName = "extraction"
	PromptSorting       PromptName = "sorting"
)

// PromptFiles are the prompts' file names in the Memory folder.
var PromptFiles = map[PromptName]string{
	PromptConsolidation: "consolidation-prompt.md",
	PromptExtraction:    "extraction-prompt.md",
	PromptSorting:       "sorting-prompt.md",
}

func copyPrompt(memoryDir string, name PromptName) error {
	b, err := shippedPrompts.ReadFile("prompts/" + PromptFiles[name])
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(memoryDir, PromptFiles[name]), b, 0o666)
}

// ReadPrompt reads a prompt from the Memory folder, copying the shipped one
// there first when it is missing or an earlier release's, unedited.
func ReadPrompt(memoryDir string, name PromptName) (string, error) {
	file := filepath.Join(memoryDir, PromptFiles[name])
	b, err := os.ReadFile(file)
	switch {
	case os.IsNotExist(err):
		if err := os.MkdirAll(memoryDir, 0o777); err != nil {
			return "", err
		}
	case err != nil:
		return "", err
	case !isEarlierPrompt(name, b):
		return string(b), nil
	}
	if err := copyPrompt(memoryDir, name); err != nil {
		return "", err
	}
	b, err = os.ReadFile(file)
	return string(b), err
}

// ResetPrompts copies every shipped prompt into the Memory folder again.
func ResetPrompts(memoryDir string) error {
	if err := os.MkdirAll(memoryDir, 0o777); err != nil {
		return err
	}
	for _, name := range []PromptName{PromptConsolidation, PromptExtraction, PromptSorting} {
		if err := copyPrompt(memoryDir, name); err != nil {
			return err
		}
	}
	return nil
}
