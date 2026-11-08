package sessionview

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/kkkk2323/droid-sdk-go/protocol"
	"github.com/kkkk2323/droid-sdk-go/session"

	"github.com/kkkk2323/droi/apps/native/pkg/kernel"
)

// The conversation's settings on the Kernel path: its agent, as pi.agent stores it, and the models harness.info
// offers. The composer's picker reads them in the Daemon's shapes, so it draws one list for both backends.

// kernelSettings are the agent's model and thinking level as the picker reads a Session's settings.
func kernelSettings(agent kernel.Agent) session.Settings {
	var s session.Settings
	if agent.Model != nil {
		s.ModelID = agent.Model.ModelID
	}
	s.ReasoningEffort = protocol.ReasoningEffort(agent.ThinkingLevel)
	return s
}

// modelMetadata is a model Kernel offers as the picker reads one of the Daemon's.
func modelMetadata(m kernel.ModelInfo) protocol.ModelMetadata {
	efforts := make([]protocol.ReasoningEffort, len(m.Thinking))
	for i, t := range m.Thinking {
		efforts[i] = protocol.ReasoningEffort(t)
	}
	md := protocol.ModelMetadata{
		ID:                        m.ID,
		DisplayName:               cmp.Or(m.Name, m.ID),
		ModelProvider:             protocol.ModelProvider(m.Provider),
		SupportedReasoningEfforts: efforts,
	}
	if len(efforts) > 0 {
		md.DefaultReasoningEffort = efforts[0]
	}
	if len(m.Input) > 0 && !slices.Contains(m.Input, "image") {
		no := true
		md.NoImageSupport = &no
	}
	return md
}

// kernelModels are the models Kernel offers, for the picker; none until the app has read harness.info.
func (v *View) kernelModels() []protocol.ModelMetadata {
	if v.env.Models == nil {
		return nil
	}
	list := v.env.Models()
	out := make([]protocol.ModelMetadata, len(list))
	for i, m := range list {
		out[i] = modelMetadata(m)
	}
	return out
}

// Configure changes the model or the thinking level of the conversation Kernel drives; "" leaves one as it is. The
// view Kernel pushes after it carries the new agent, which is what the picker then shows. It reports false for a
// page the Daemon draws, whose settings the app changes itself.
func (v *View) Configure(modelID, thinking string) bool {
	id, ok := v.kernelID()
	if !ok {
		return false
	}
	change := &kernel.AgentChange{}
	if modelID != "" {
		ref, err := v.modelRef(modelID)
		if err != nil {
			v.Fail(err)
			return true
		}
		change.Model = kernel.Some(ref)
	}
	if thinking != "" {
		change.ThinkingLevel = kernel.Some(thinking)
	}
	go func() {
		if _, err := v.env.Kernel.Configure(v.env.Ctx, kernel.ConfigureParams{ID: id, Change: change}); err != nil {
			v.Fail(err)
		}
	}()
	return true
}

// modelRef is the provider and id of a model the picker names by id alone: the first provider that offers it, as
// harness.info lists them.
func (v *View) modelRef(modelID string) (kernel.ModelRef, error) {
	if v.env.Models != nil {
		for _, m := range v.env.Models() {
			if m.ID == modelID {
				return kernel.ModelRef{Provider: m.Provider, ModelID: m.ID}, nil
			}
		}
	}
	return kernel.ModelRef{}, fmt.Errorf("no provider offers the model %q", modelID)
}
