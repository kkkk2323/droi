package app

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel/fakekernel"
)

func (a *App) kernelModelIDs() []string {
	a.kernel.mu.Lock()
	defer a.kernel.mu.Unlock()
	ids := make([]string, 0, len(a.kernel.models))
	for _, m := range a.kernel.models {
		ids = append(ids, m.Provider+"/"+m.ID)
	}
	return ids
}

// TestTheWindowOffersTheModelsTheHostGaveTheRunningKernel: harness.configure changes the running Kernel's models, and
// the window, told by harness.changed, offers what the Kernel has now, on the same connection.
func TestTheWindowOffersTheModelsTheHostGaveTheRunningKernel(t *testing.T) {
	h := kernelHarness(t)
	h.until("the Kernel's models", func() bool { return len(h.a.kernelModelIDs()) > 0 })
	if slices.Contains(h.a.kernelModelIDs(), "hot/hot-model") {
		t.Fatal("the model is offered before the Host gave it")
	}
	providers, _ := json.Marshal([]map[string]any{{
		"id": "hot", "api": "openai-completions", "baseUrl": "http://127.0.0.1:9/v1",
		"models": []map[string]any{{"id": "hot-model", "contextWindow": 1000, "maxTokens": 100}},
	}})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := h.testConn().HarnessConfigure(ctx, kernel.HarnessConfigureParams{Providers: providers}); err != nil {
		t.Fatal(err)
	}
	h.until("the new model", func() bool { return slices.Contains(h.a.kernelModelIDs(), "hot/hot-model") })
}

// TestTheSessionDefaultsModelIsTheKernelsDefault: the model the Session defaults name is the one Kernel starts a
// conversation with when its create names none, so a Session a phone starts has the model this window's would.
func TestTheSessionDefaultsModelIsTheKernelsDefault(t *testing.T) {
	sc := sessionsScenario()
	sc.Input = map[string]any{"defaults": map[string]any{"modelId": fakekernel.ModelID}}
	h := newHarness(t, sc, "")
	if h.kernel == nil {
		t.Skip("the Sessions are on the Fake Daemon")
	}
	want := kernel.ModelRef{Provider: fakekernel.ProviderID, ModelID: fakekernel.ModelID}
	h.until("Kernel's default model", func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		info, err := h.testConn().Harness(ctx)
		return err == nil && info.Agent != nil && info.Agent.Model == want
	})
}
