package sessionview

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel/fakekernel"
)

// TestAKernelPageShowsAndChangesItsModel: the picker on the Kernel path reads the conversation's agent from its
// view and the models harness.info offers, and a choice is conversation.configure with the model's provider; the
// view Kernel pushes next carries it.
func TestAKernelPageShowsAndChangesItsModel(t *testing.T) {
	k := fakekernel.Start(t, fakekernel.Scenario{})
	c := k.Dial(t)
	o := k.Observe(c)
	st := NewStore(3)
	id := k.CreateConversation(t, c)
	pumpNotifications(st, c)
	info, err := c.Harness(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	v := New(strconvID(id), Env{Kernel: o, Sessions: st, Revisions: &Revisions{}, Models: func() []kernel.ModelInfo { return info.Models }})
	v.Open()
	await(t, "the page to open", func() bool { s := v.Frame(); return s != nil && s.Loaded })

	s := v.Frame()
	if got := s.Settings().ModelID; got != fakekernel.ModelID {
		t.Errorf("the page shows the model %q, want %q", got, fakekernel.ModelID)
	}
	if s.Cwd != k.Workspace {
		t.Errorf("the page's working directory is %q, want %q", s.Cwd, k.Workspace)
	}
	models := s.AvailableModels()
	if len(models) != 1 || models[0].ID != fakekernel.ModelID || models[0].DisplayName != "Fake one" {
		t.Fatalf("the picker offers %+v", models)
	}

	if !v.Configure(fakekernel.ModelID, "off") {
		t.Fatal("a Kernel page did not take the change")
	}
	await(t, "conversation.configure", func() bool { return o.Count(kernel.MethodConversationConfig) == 1 })
	var sent kernel.ConfigureParams
	if err := json.Unmarshal(o.CallsOf(kernel.MethodConversationConfig)[0].Params, &sent); err != nil {
		t.Fatal(err)
	}
	if m := sent.Change.Model; m == nil || m.Value == nil || *m.Value != (kernel.ModelRef{Provider: fakekernel.ProviderID, ModelID: fakekernel.ModelID}) {
		t.Errorf("the page configured the model %+v", m)
	}
	if l := sent.Change.ThinkingLevel; l == nil || l.Value == nil || *l.Value != "off" {
		t.Errorf("the page configured the thinking level %+v", l)
	}
	await(t, "the new agent in the view", func() bool { return v.Frame().Settings().ReasoningEffort == "off" })

	// A model no provider offers is refused here, with a sentence, rather than sent for Kernel to refuse.
	v.Configure("no-such-model", "")
	await(t, "the error", func() bool { return strings.Contains(v.Err(), "no-such-model") })
	if n := o.Count(kernel.MethodConversationConfig); n != 1 {
		t.Errorf("an unknown model was sent: %d configure calls", n)
	}
}
