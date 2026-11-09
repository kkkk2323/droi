package app

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	"weak"

	"github.com/kkkk2323/droid-sdk-go/fakedaemon"
	"github.com/kkkk2323/droid-sdk-go/protocol"
	"github.com/kkkk2323/droid-sdk-go/session"
)

// transcriptFrameBudget is the most a frame of a Session with its whole long
// history loaded may allocate. The window allocates about 460 KB a frame
// whatever the Session's length; cutting every entry into rows on each frame
// made it 17 MB at 27,000 messages.
const transcriptFrameBudget = 1 << 20

// liveCallBudget is the most the window may allocate for one tool call a
// running agent adds to that Session, frames included. Reading every
// message's tool calls again for each one made it 156 MB at 27,000 messages.
const liveCallBudget = 4 << 20

// TestALongTranscriptStaysCheap opens a Session of 8000 messages, shaped as a
// working agent's, with all of them loaded as when the user reads it back to
// its start, and measures a frame and a tool call the agent adds. Run it with
// -v for the numbers.
func TestALongTranscriptStaysCheap(t *testing.T) {
	const messages = 8000
	h, id := longTranscriptHarness(t, workingSession(messages))
	s := h.openLoaded(id, messages)

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	start := time.Now()
	const frames = 50
	for range frames {
		h.tt.Frame()
	}
	runtime.ReadMemStats(&after)
	perFrame := (after.TotalAlloc - before.TotalAlloc) / frames
	t.Logf("a frame with %d messages loaded: %v, %d KB allocated", messages, time.Since(start)/frames, perFrame>>10)
	if perFrame > transcriptFrameBudget {
		t.Errorf("a frame allocates %d KB, more than %d KB", perFrame>>10, transcriptFrameBudget>>10)
	}

	const calls = 40
	runtime.GC()
	runtime.ReadMemStats(&before)
	start = time.Now()
	now := time.Now().UnixMilli()
	parent := lastMessageID(s.Messages())
	for i := range calls {
		use := fmt.Sprintf("toolu_live_%d", i)
		for _, m := range []map[string]any{
			{"id": "live-a-" + use, "parentId": parent, "role": "assistant", "createdAt": now, "updatedAt": now, "visibility": "both",
				"content": []any{map[string]any{"type": "tool_use", "id": use, "name": "Read", "input": map[string]any{"file_path": "/repo/main.go"}}}},
			{"id": "live-u-" + use, "parentId": "live-a-" + use, "role": "user", "createdAt": now, "updatedAt": now, "visibility": "both",
				"content": []any{map[string]any{"type": "tool_result", "toolUseId": use, "content": "package main\n"}}},
		} {
			if err := h.d.Notify(id, map[string]any{"type": "create_message", "message": m}); err != nil {
				t.Fatal(err)
			}
			parent = m["id"].(string)
		}
		h.until("the call", func() bool { return s.MessageCount() == messages+2*(i+1) })
		h.frame()
	}
	runtime.ReadMemStats(&after)
	perCall := (after.TotalAlloc - before.TotalAlloc) / calls
	t.Logf("a tool call the agent adds: %v, %d KB allocated", time.Since(start)/calls, perCall>>10)
	if perCall > liveCallBudget {
		t.Errorf("a tool call allocates %d KB, more than %d KB", perCall>>10, liveCallBudget>>10)
	}
}

// A long Session read back to its start lets go of what was read once its
// transcript follows its end again, and when another Session is opened.
func TestALongSessionLetsGoOfWhatItRead(t *testing.T) {
	const messages = 3000
	h, id := longTranscriptHarness(t, workingSession(messages))
	s := h.openLoaded(id, messages)
	h.a.views[id].ScrollToLatest()
	h.until("the older messages let go of", func() bool { return s.MessageCount() == LoadedMessageLimit })
	if !s.HasOlderMessages() {
		t.Fatal("the transcript does not offer the older messages again")
	}

	s = h.openLoaded(id, messages)
	first := weak.Make(h.a.views[id].Entries()[0])
	h.a.Go(Route{Name: "session", SessionID: h.d.Sessions[1].SessionID})
	h.until("the other Session", func() bool { return h.hasText("hi") })
	for range 5 {
		h.frame()
	}
	if n := s.MessageCount(); n != LoadedMessageLimit {
		t.Fatalf("the Session left holds %d messages", n)
	}
	if v := h.a.views[id]; v == nil || len(v.Entries()) != 0 {
		t.Fatal("the view left keeps its transcript")
	}
	// Nothing holds on to what was made of the messages read back to.
	runtime.GC()
	if first.Value() != nil {
		t.Fatal("the Session's first entry is still held after leaving it")
	}
	h.a.Go(Route{Name: "session", SessionID: id})
	h.until("the transcript again", func() bool { return len(h.a.views[id].Entries()) > 0 && h.hasText("Long session") })
}

// openLoaded opens Session id and loads all its messages, as when the user
// reads it back to its start, so the transcript no longer follows its end.
func (h *harness) openLoaded(id string, messages int) *session.Session {
	h.t.Helper()
	h.until("the list", func() bool { return h.hasText("Long session") })
	h.a.Go(Route{Name: "session", SessionID: id})
	h.until("the transcript", func() bool { s := h.a.ctl.Store().Session(id); return s != nil && s.MessageCount() > 0 })
	// Up from the end, as the user reads back: a transcript at its end
	// follows it and lets go of what is loaded above.
	h.until("the transcript left its end", func() bool {
		h.tt.Scroll(float32(refW)*0.65, float32(refH)*0.5, 0, -600)
		return h.hasText("Scroll to latest")
	})
	limit := int64(messages)
	if _, err := h.a.ctl.LoadSession(h.a.ctx, protocol.LoadSessionParams{SessionID: id, MessageLimit: &limit}); err != nil {
		h.t.Fatal(err)
	}
	s := h.a.ctl.Store().Session(id)
	h.until("every message", func() bool { return s.MessageCount() == messages })
	for range 20 {
		h.frame()
	}
	return s
}

func lastMessageID(ms []protocol.FactoryDroidMessage) string {
	if len(ms) == 0 {
		return ""
	}
	return ms[len(ms)-1].ID
}

// longTranscriptHarness starts the window over sc, whose first Session is the
// long one, and returns that Session's id. The Fake Daemon reads its
// scenario from an environment variable, which exec limits to about 1 MB, so
// Node gets this one from a file.
func longTranscriptHarness(t *testing.T, sc fakedaemon.Scenario) (*harness, string) {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip(err)
	}
	dir := t.TempDir()
	spec, _ := json.Marshal(sc)
	file := filepath.Join(dir, "scenario.json")
	preload := filepath.Join(dir, "preload.mjs")
	write := func(name, body string, mode os.FileMode) {
		if err := os.WriteFile(name, []byte(body), mode); err != nil {
			t.Fatal(err)
		}
	}
	write(file, string(spec), 0o644)
	write(preload, "import fs from 'node:fs'\nprocess.env.DROID_FAKE_SCENARIO = fs.readFileSync(process.env.LONG_SCENARIO_FILE, 'utf8')\n", 0o644)
	write(filepath.Join(dir, "node"), "#!/bin/sh\nexec '"+node+"' --import '"+preload+"' \"$@\"\n", 0o755)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("LONG_SCENARIO_FILE", file)
	h := newHarness(t, fakedaemon.Scenario{}, "")
	return h, sc.Sessions[0].Extra["sessionId"].(string)
}

// workingSession is a Session of n messages shaped as an agent's long run:
// turns of a prompt, a few dozen tool calls each with thinking and a result
// of a few KB, and a reply in Markdown.
func workingSession(n int) fakedaemon.Scenario {
	const id = "long0000-0000-4000-8000-000000000000"
	result := strings.Repeat("func example() error {\n\treturn nil\n}\n", 60)
	var msgs []fakedaemon.Message
	turn, call := 0, 0
	for len(msgs) < n {
		turn++
		msgs = append(msgs, fakedaemon.Message{Role: "user", Text: fmt.Sprintf("Step %d: go on with the plan.", turn)})
		for k := 0; k < 1+turn%37 && len(msgs) < n-2; k++ {
			call++
			use := fmt.Sprintf("toolu_%06d", call)
			msgs = append(msgs,
				fakedaemon.Message{Role: "assistant", Content: []map[string]any{
					{"type": "thinking", "thinking": "I should read the next file to see how it is wired.", "signature": "sig"},
					{"type": "tool_use", "id": use, "name": "Read", "input": map[string]any{"file_path": fmt.Sprintf("/repo/pkg/file%d.go", call)}},
				}},
				fakedaemon.Message{Role: "user", Content: []map[string]any{{"type": "tool_result", "toolUseId": use, "content": result}}},
			)
		}
		if len(msgs) < n {
			msgs = append(msgs, fakedaemon.Message{Role: "assistant", Text: fmt.Sprintf("## Step %d\n\nDone. The files are wired as planned:\n\n- `main.go` starts it\n- `run.go` does the work\n", turn)})
		}
	}
	return fakedaemon.Scenario{Sessions: []fakedaemon.SessionSpec{
		{Title: "Long session", Cwd: "/Users/dev/long", Messages: msgs[:n], Extra: map[string]any{"sessionId": id, "updatedAt": time.Now().Unix()}},
		{Title: "Short session", Cwd: "/Users/dev/short", Messages: []fakedaemon.Message{{Role: "user", Text: "hi"}}, Extra: map[string]any{"updatedAt": time.Now().Unix() - 60}},
	}}
}
