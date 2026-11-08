package app

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/kernel"
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
