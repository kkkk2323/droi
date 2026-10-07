package memorywork

import (
	"embed"
	"os"
	"path/filepath"
)

// The Memory Sessions' prompts. The app ships them embedded and copies them
// into the Memory folder on first use; from then on the copies are read, so a
// user can edit them, and "Reset prompts to default" copies them again.

// The copies in prompts/ must match apps/desktop/resources/memory; a test
// checks they do.
//
//go:embed prompts/*.md
var shippedPrompts embed.FS

// PromptName names one Memory Session prompt.
type PromptName string

const (
	PromptConsolidation PromptName = "consolidation"
	PromptExtraction    PromptName = "extraction"
)

// PromptFiles are the prompts' file names in the Memory folder.
var PromptFiles = map[PromptName]string{
	PromptConsolidation: "consolidation-prompt.md",
	PromptExtraction:    "extraction-prompt.md",
}

func copyPrompt(memoryDir string, name PromptName) error {
	b, err := shippedPrompts.ReadFile("prompts/" + PromptFiles[name])
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(memoryDir, PromptFiles[name]), b, 0o666)
}

// ReadPrompt reads a prompt from the Memory folder, copying the shipped one
// there first when it is missing.
func ReadPrompt(memoryDir string, name PromptName) (string, error) {
	file := filepath.Join(memoryDir, PromptFiles[name])
	if _, err := os.Stat(file); os.IsNotExist(err) {
		if err := os.MkdirAll(memoryDir, 0o777); err != nil {
			return "", err
		}
		if err := copyPrompt(memoryDir, name); err != nil {
			return "", err
		}
	}
	b, err := os.ReadFile(file)
	return string(b), err
}

// ResetPrompts copies both shipped prompts into the Memory folder again.
func ResetPrompts(memoryDir string) error {
	if err := os.MkdirAll(memoryDir, 0o777); err != nil {
		return err
	}
	for _, name := range []PromptName{PromptConsolidation, PromptExtraction} {
		if err := copyPrompt(memoryDir, name); err != nil {
			return err
		}
	}
	return nil
}
