package memorywork

import (
	"embed"
	"os"
	"path/filepath"
	"slices"

	"github.com/kkkk2323/droi/apps/native/internal/memory"
)

// The Memory Sessions' prompts. The app ships them embedded and copies them
// into the Memory folder on first use; from then on the copies are read, so a
// user can edit them, and "Reset prompts to default" copies them again. They
// adapt Codex's memory v2 prompts (openai/codex, codex-rs/memories/write/
// templates/memories, Apache-2.0): stage_one_system_v2.md and
// consolidation_v2.md, answering JSON instead of editing files.
//
//go:embed prompts/*.md
var shippedPrompts embed.FS

// earlierPrompts are the SHA-256 of every prompt an earlier release shipped.
// A copy that still matches one was never edited, so it gets the shipped
// prompt in its place; add the old hash here whenever a prompt changes.
var earlierPrompts = map[PromptName][]string{}

func isEarlierPrompt(name PromptName, text []byte) bool {
	return slices.Contains(earlierPrompts[name], memory.Hash(text))
}

// PromptName names one Memory Session prompt.
type PromptName string

const (
	PromptSessionSummary PromptName = "session-summary"
	PromptMemorySummary  PromptName = "memory-summary"
)

// PromptFiles are the prompts' file names in the Memory folder.
var PromptFiles = map[PromptName]string{
	PromptSessionSummary: "session-summary-prompt.md",
	PromptMemorySummary:  "memory-summary-prompt.md",
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
	for _, name := range []PromptName{PromptSessionSummary, PromptMemorySummary} {
		if err := copyPrompt(memoryDir, name); err != nil {
			return err
		}
	}
	return nil
}
