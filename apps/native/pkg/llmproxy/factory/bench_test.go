package factory

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// bigChat is a chat completions request of size bytes, the shape a long
// Session sends: one system prompt, then text turns.
func bigChat(t testing.TB, size int, opened bool) []byte {
	t.Helper()
	system := "<environment>\nYou are running as the Kernel of Droi.\n</environment>"
	if opened {
		system = DroidLine + "\n" + system
	}
	messages := []map[string]string{{"role": "system", "content": system}}
	for n := 0; len(mustJSON(messages)) < size; n++ {
		role := "user"
		if n%2 == 1 {
			role = "assistant"
		}
		// A turn long enough to reach size in a few hundred messages.
		messages = append(messages, map[string]string{"role": role, "content": strings.Repeat(fmt.Sprintf("turn %d: ", n), 400)})
	}
	return mustJSON(map[string]any{"model": "deepseek-v4.1-flash-1017", "messages": messages, "stream": true})
}

// The proxy runs DroidBody on every model request: this is what it costs on a
// long Session, once for a request that needs opening and once for one that
// does not (the Daemon's own, and every later turn of an opened one).
func BenchmarkDroidBody(b *testing.B) {
	for _, size := range []int{4 << 10, 256 << 10, 1 << 20} {
		for _, opened := range []bool{false, true} {
			body := bigChat(b, size, opened)
			name := fmt.Sprintf("%dB/opened=%v", len(body), opened)
			b.Run(name, func(b *testing.B) {
				b.SetBytes(int64(len(body)))
				for b.Loop() {
					_, _ = DroidBody("/api/llm/o/v1/chat/completions", body)
				}
			})
		}
	}
}

// BenchmarkExtractModel is what the proxy already paid before this package
// existed, for comparison.
func BenchmarkExtractModel(b *testing.B) {
	body := bigChat(b, 1<<20, true)
	b.SetBytes(int64(len(body)))
	for b.Loop() {
		var m struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(body, &m)
	}
}
