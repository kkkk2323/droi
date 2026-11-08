package host

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/kernel"
)

// One real round through everything the Host runs: the Kernel asks for a Factory model, the Key Pool's proxy sends
// it with the pool's key, and Factory answers. Only with DROI_LIVE_FACTORY_KEY (a Factory key, never logged), on the
// cheapest chat model, one word.
func TestLiveHostKernelAnswersWithFactory(t *testing.T) {
	key := os.Getenv("DROI_LIVE_FACTORY_KEY")
	if key == "" {
		t.Skip("DROI_LIVE_FACTORY_KEY is not set")
	}
	bin := kernelBinaryForTest(t)
	env := map[string]string{KernelEnv: "1", KernelBinEnv: bin}
	h := New(Config{UserData: t.TempDir(), Home: t.TempDir(), Env: func(k string) string { return env[k] }})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := h.KeyPool().Add(ctx, key); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.KeyPool().Stop)
	clients := make(chan *kernel.Client, 4)
	link := NewKernelLink(KernelLinkOptions{Client: "window", Token: h.KernelToken, OnClient: func(c *kernel.Client, _ int) { clients <- c }})
	unhear := h.OnKernelState(link.Use)
	h.startKernel()
	t.Cleanup(func() { unhear(); link.Close(); h.stopKernel() })
	var c *kernel.Client
	select {
	case c = <-clients:
	case <-ctx.Done():
		t.Fatal("no Kernel connection")
	}
	model := os.Getenv("DROI_LIVE_MODEL")
	if model == "" {
		model = "deepseek-v4.1-flash-1017"
	}
	provider := "factory-chat"
	if strings.HasPrefix(model, "claude-") {
		provider = "factory-anthropic"
	}
	created, err := c.Create(ctx, kernel.CreateParams{
		Agent: &kernel.AgentChange{Model: kernel.Some(kernel.ModelRef{Provider: provider, ModelID: model})},
		Cwd:   t.TempDir(), Kind: "chat",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Submit(ctx, kernel.SubmitParams{ID: created.ConversationID, Input: "Reply with the single word: pong"}); err != nil {
		t.Fatal(err)
	}
	for ctx.Err() == nil {
		page, err := c.Entries(ctx, kernel.EntriesParams{ID: created.ConversationID, Limit: 50})
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range page.Items {
			if e.Kind != "pi.assistant" {
				continue
			}
			text := string(e.Model)
			if strings.Contains(text, `"stopReason":"error"`) {
				t.Fatalf("Factory refused: %s", text)
			}
			t.Logf("%s answered: %s", model, text)
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("no answer")
}
