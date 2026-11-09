package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/kernel/fakekernel"
)

// rpc is a phone's side of the Kernel's protocol, through the Gateway: one request at a time, the notifications that
// arrive meanwhile kept.
type rpc struct {
	t     *testing.T
	c     *websocket.Conn
	next  int
	heard []map[string]any
}

func (r *rpc) read(ctx context.Context) map[string]any {
	r.t.Helper()
	_, data, err := r.c.Read(ctx)
	if err != nil {
		r.t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		r.t.Fatalf("%s: %v", data, err)
	}
	return m
}

func (r *rpc) call(method string, params any) map[string]any {
	r.t.Helper()
	r.next++
	id := fmt.Sprint(r.next)
	frame, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := r.c.Write(ctx, websocket.MessageText, frame); err != nil {
		r.t.Fatal(err)
	}
	for {
		m := r.read(ctx)
		if m["id"] == id {
			if e, ok := m["error"]; ok {
				r.t.Fatalf("%s: %v", method, e)
			}
			result, _ := m["result"].(map[string]any)
			return result
		}
		r.heard = append(r.heard, m)
	}
}

// waitFor reads notifications until one of method matches.
func (r *rpc) waitFor(method string, match func(params map[string]any) bool) map[string]any {
	r.t.Helper()
	for i, m := range r.heard {
		if p, _ := m["params"].(map[string]any); m["method"] == method && match(p) {
			r.heard = append(r.heard[:i], r.heard[i+1:]...)
			return p
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for {
		m := r.read(ctx)
		if p, _ := m["params"].(map[string]any); m["method"] == method && match(p) {
			return p
		}
		r.heard = append(r.heard, m)
	}
}

// A phone runs a turn on the real Kernel through the Gateway, and the Kernel knows it as the phone: the approval it
// answered says so.
func TestAPhoneRunsATurnOnTheKernelThroughTheGateway(t *testing.T) {
	k := fakekernel.Start(t, fakekernel.Scenario{
		Autonomy: "ask",
		Steps: []fakekernel.Step{
			{Match: "write it", Call: &fakekernel.Call{Name: "write", Arguments: map[string]any{"path": "note.txt", "content": "hi"}}},
			{Match: "write it", Text: "Wrote it."},
		},
	})
	opts := baseOptions(nil)
	opts.KernelURL = func() string { return kernel.URL(k.Port) }
	opts.KernelToken = func() string { return k.Token }
	g := startGateway(t, opts)

	r := &rpc{t: t, c: connect(t, kernelSocketURL(g.URL(), testToken))}
	r.c.SetReadLimit(maxMessage)
	info := r.call("harness.info", nil)
	if _, ok := info["capabilities"]; !ok {
		t.Fatalf("harness.info: %v", info)
	}
	made := r.call("conversation.create", map[string]any{"cwd": k.Workspace, "agent": map[string]any{"model": map[string]any{"provider": fakekernel.ProviderID, "modelId": fakekernel.ModelID}}})
	id := made["conversationId"]
	r.call("conversation.open", map[string]any{"id": id, "subscribe": "delta"})
	r.call("conversation.submit", map[string]any{"id": id, "input": "write it"})

	asked := r.waitFor("approval.requested", func(map[string]any) bool { return true })
	r.call("approval.respond", map[string]any{"requestId": asked["requestId"], "outcome": "proceed_once"})
	resolved := r.waitFor("approval.resolved", func(p map[string]any) bool { return p["requestId"] == asked["requestId"] })
	if resolved["by"] != kernel.ClientPhone {
		t.Fatalf("answered by %v", resolved["by"])
	}
	// A delta subscriber gets ops, not whole views.
	view := r.waitFor("conversation.view", func(p map[string]any) bool { return p["id"] == id })
	if view["kind"] == "value" && view["value"] != nil {
		t.Fatalf("a delta subscriber got a whole view: %v", view)
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		page := r.call("conversation.entries", map[string]any{"id": id, "limit": 50, "query": map[string]any{"kinds": []string{"pi.assistant"}}})
		got, _ := json.Marshal(page["items"])
		if strings.Contains(string(got), "Wrote it.") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no answer: %s", got)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if b, err := os.ReadFile(filepath.Join(k.Workspace, "note.txt")); err != nil || string(b) != "hi" {
		t.Fatalf("the file: %q %v", b, err)
	}
}
