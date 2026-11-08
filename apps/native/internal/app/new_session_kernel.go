package app

// A new Session on Kernel: the New session page's conversation.create, while the window lists from Kernel.

import (
	"errors"
	"log"
	"os"
	"path/filepath"
	"slices"

	"github.com/google/uuid"
	"github.com/kkkk2323/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/pkg/gateway"
	"github.com/kkkk2323/droi/apps/native/pkg/kernel"
	"github.com/kkkk2323/droi/apps/native/pkg/models"
	"github.com/kkkk2323/droi/apps/native/pkg/sessionview"
)

// onKernel reports whether the window lists and starts its Sessions on Kernel: once the Host handed it a
// connection.
func (a *App) onKernel() bool {
	_, err := a.kernel.client()
	return err == nil
}

// kernelChoices are the models Kernel offers, for the New session page's picker; nil while the window is not on
// Kernel or harness.info has not answered.
func (a *App) kernelChoices() []models.Choice {
	if !a.onKernel() {
		return nil
	}
	offered := a.kernel.offered()
	md := make([]protocol.ModelMetadata, len(offered))
	for i, m := range offered {
		md[i] = sessionview.ModelMetadata(m)
	}
	return models.ToChoices(md)
}

// createConversation makes the conversation the New session page asks for in workspace (or a new Scratch folder),
// and answers its Session id, or why it could not. The page's worktree choice is not made: on Kernel the Host makes
// worktrees, which comes with M4.
func (a *App) createConversation(c *kernel.Client, workspace string, p protocol.InitializeSessionParams) (string, error) {
	kind := "chat"
	if workspace == scratchPick {
		dir, err := "", gateway.ErrNotFound
		if a.cfg.Gateway != nil {
			dir, err = a.cfg.Gateway.CreateScratch(a.ctx)
		}
		if errors.Is(err, gateway.ErrNotFound) {
			return "", errors.New(a.noScratchFolder())
		}
		if err != nil {
			return "", err
		}
		workspace, kind = dir, "scratch"
	}
	cwd, err := usableDir(workspace)
	if err != nil {
		return "", err
	}
	res, err := c.Create(a.ctx, kernel.CreateParams{Agent: a.newAgent(p.ModelID, string(p.ReasoningEffort)), Cwd: cwd, Kind: kind, RequestID: uuid.NewString()})
	if err != nil {
		return "", err
	}
	// A level picked on the page is the conversation's own, set before its first message can run a tool; the
	// default's is left to follow the default.
	if level := string(p.AutonomyLevel); level != "" {
		if dv := a.sessionDefaults(); dv == nil || dv.AutonomyLevel != level {
			if err := a.setConversationLevel(res.ConversationID, kernelLevel(level)); err != nil {
				log.Printf("droi: kernel: the autonomy of conversation %d: %v", res.ConversationID, err)
			}
		}
	}
	return a.sessionIDOf(res.ConversationID), nil
}

// newAgent is the agent the page picked: a model Kernel offers, with a thinking level it takes. A model it does
// not offer is left to Kernel's own default.
func (a *App) newAgent(modelID, effort string) *kernel.AgentChange {
	offered := a.kernel.offered()
	i := slices.IndexFunc(offered, func(m kernel.ModelInfo) bool { return m.ID == modelID })
	if i < 0 {
		return nil
	}
	m := offered[i]
	change := &kernel.AgentChange{Model: kernel.Some(kernel.ModelRef{Provider: m.Provider, ModelID: m.ID})}
	if effort != "" && slices.Contains(m.Thinking, effort) {
		change.ThinkingLevel = kernel.Some(effort)
	}
	return change
}

// usableDir is the directory a conversation works in, or why path cannot be one. Kernel runs on this computer, so
// the check is the file system's.
func usableDir(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err == nil {
		var info os.FileInfo
		if info, err = os.Stat(abs); err == nil && !info.IsDir() {
			err = errors.New("not a directory")
		}
	}
	if err != nil {
		return "", errors.New(L("%s is not a usable directory.", path))
	}
	return abs, nil
}
