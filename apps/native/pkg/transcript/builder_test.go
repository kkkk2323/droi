package transcript

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"reflect"
	"runtime"
	"slices"
	"testing"

	"github.com/kkkk2323/droid-sdk-go/protocol"
)

// referenceBuild is Build as it was before Builder kept anything between
// builds: every block decoded and every entry made anew.
func referenceBuild(messages []protocol.FactoryDroidMessage) []*Entry {
	decoded := func(m protocol.FactoryDroidMessage) []*decodedBlock {
		out := make([]*decodedBlock, len(m.Content))
		for i, b := range m.Content {
			out[i] = decodeBlock(b, fmt.Sprintf("%s:%d", m.ID, i))
		}
		return out
	}
	results := map[string]*protocol.ToolResult{}
	for _, m := range messages {
		if m.Role != protocol.MessageRoleTool {
			continue
		}
		for _, d := range decoded(m) {
			if d.typ == protocol.ContentBlockTypeToolResult && d.ok {
				results[d.result.ToolUseID] = d.result
			}
		}
	}
	records, resolved := queuedRecords(messages)
	watching := map[string]*ToolCall{}
	var entries []*Entry
	for _, m := range messages {
		if isReceipt(m) {
			continue
		}
		if recs := records[m.ID]; recs != nil {
			if n := len(entries); n > 0 && isRecordEntry(entries[n-1]) {
				prev := entries[n-1]
				prev.Blocks[0].Records = append(prev.Blocks[0].Records, recs...)
				prev.CreatedAt = m.CreatedAt
				continue
			}
			entries = append(entries, &Entry{ID: m.ID, User: true, CreatedAt: m.CreatedAt,
				Blocks: []Block{{Kind: QueuedAnswers, ID: m.ID + ":0", Records: recs}}})
			continue
		}
		role := m.Role
		if m.Visibility == protocol.MessageVisibilityUserOnly && (role == protocol.MessageRoleSystem || role == protocol.MessageRoleUser) {
			role = protocol.MessageRoleAssistant
		}
		if role != protocol.MessageRoleUser && role != protocol.MessageRoleAssistant {
			continue
		}
		if m.IsUserVisible != nil && !*m.IsUserVisible {
			continue
		}
		var blocks []Block
		for _, d := range decoded(m) {
			if !d.ok {
				continue
			}
			switch d.typ {
			case protocol.ContentBlockTypeText:
				blocks = append(blocks, Block{Kind: Text, ID: d.id, Text: d.text, Streaming: d.streaming})
			case protocol.ContentBlockTypeImage:
				blocks = append(blocks, Block{Kind: Picture, ID: d.id, Image: d.image})
			case protocol.ContentBlockTypeThinking:
				blocks = append(blocks, Block{Kind: Thinking, ID: d.id, Text: d.text, DurationMs: d.durationMs, Streaming: d.streaming})
			case protocol.ContentBlockTypeToolUse:
				use := d.use
				call := &ToolCall{Use: use, Input: d.input, RawInput: d.rawInput, Keys: d.keys, Result: results[use.ID]}
				call.Queued = queuedStateOf(call, resolved)
				if use.ScriptExecution != nil {
					if owner := watching[use.ScriptExecution.OuterToolUseID]; owner != nil {
						owner.Nested = append(owner.Nested, call)
						continue
					}
				}
				if run := ScriptRunOf(use, call.Input); run != "" {
					call.Lifecycle = true
					watching[run] = call
				}
				if use.Name == TaskTool {
					blocks = append(blocks, Block{Kind: Subagent, ID: d.id, Call: call})
					continue
				}
				if use.Name == SpecTool {
					blocks = append(blocks, Block{Kind: Spec, ID: d.id, Call: call})
					continue
				}
				if n := len(blocks); n > 0 && blocks[n-1].Kind == Tools {
					blocks[n-1].Calls = append(blocks[n-1].Calls, call)
				} else {
					blocks = append(blocks, Block{Kind: Tools, ID: d.id, Calls: []*ToolCall{call}})
				}
			}
		}
		if len(blocks) == 0 {
			continue
		}
		isError := m.IsError != nil && *m.IsError
		user := role == protocol.MessageRoleUser
		if n := len(entries); !user && n > 0 && !entries[n-1].User {
			prev := entries[n-1]
			for _, b := range blocks {
				if k := len(prev.Blocks); b.Kind == Tools && k > 0 && prev.Blocks[k-1].Kind == Tools {
					prev.Blocks[k-1].Calls = append(prev.Blocks[k-1].Calls, b.Calls...)
				} else {
					prev.Blocks = append(prev.Blocks, b)
				}
			}
			prev.CreatedAt = m.CreatedAt
			prev.IsError = prev.IsError || isError
			continue
		}
		entries = append(entries, &Entry{ID: m.ID, User: user, Blocks: blocks, CreatedAt: m.CreatedAt, IsError: isError})
	}
	return entries
}

// randomSession makes and changes a Session's messages as a working agent,
// the Daemon and the user do.
type randomSession struct {
	r       *rand.Rand
	all     []protocol.FactoryDroidMessage
	lo      int
	n       int
	uses    []string
	scripts []string
	asks    []string
}

func (s *randomSession) message(role string, content []map[string]any, edit ...func(*protocol.FactoryDroidMessage)) protocol.FactoryDroidMessage {
	s.n++
	m := protocol.FactoryDroidMessage{ID: fmt.Sprintf("m%d", s.n), Role: protocol.MessageRole(role), CreatedAt: float64(s.n), UpdatedAt: float64(s.n)}
	raw, _ := json.Marshal(content)
	if err := json.Unmarshal(raw, &m.Content); err != nil {
		panic(err)
	}
	for _, f := range edit {
		f(&m)
	}
	return m
}

func (s *randomSession) pick(ids []string) string {
	if len(ids) == 0 {
		return "none"
	}
	return ids[s.r.IntN(len(ids))]
}

func (s *randomSession) toolUse() map[string]any {
	s.n++
	id := fmt.Sprintf("u%d", s.n)
	use := map[string]any{"type": "tool_use", "id": id}
	switch s.r.IntN(9) {
	case 0:
		use["name"], use["input"] = AskUserTool, map[string]any{"blocking": false, "context": "pick one"}
		s.asks = append(s.asks, id)
	case 1:
		use["name"], use["input"] = TaskTool, map[string]any{"subagent_type": "worker"}
	case 2:
		use["name"], use["input"] = SpecTool, map[string]any{"plan": "the plan"}
	case 3:
		use["name"], use["input"] = ScriptTool, map[string]any{"script": "x"}
		s.scripts = append(s.scripts, id)
	case 4:
		use["name"], use["input"] = WaitForScriptTool, map[string]any{"toolCallId": s.pick(s.scripts)}
	case 5:
		run := s.pick(s.scripts)
		use["name"], use["input"] = "Read", map[string]any{"file_path": "/n"}
		use["scriptExecution"] = map[string]any{"runId": run, "outerToolUseId": run}
	default:
		use["name"], use["input"] = "Read", map[string]any{"file_path": fmt.Sprintf("/f%d", s.r.IntN(3))}
	}
	s.uses = append(s.uses, id)
	return use
}

func (s *randomSession) random() protocol.FactoryDroidMessage {
	text := func(t string) map[string]any { return map[string]any{"type": "text", "text": t} }
	switch s.r.IntN(14) {
	case 0, 1:
		return s.message("user", []map[string]any{text(fmt.Sprintf("prompt %d", s.r.IntN(4)))})
	case 2:
		return s.message("user", []map[string]any{text("look"), {"type": "image", "source": map[string]any{"type": "base64", "mediaType": "image/png", "data": "AAAA"}}})
	case 3:
		t := text(fmt.Sprintf("reply %d", s.r.IntN(4)))
		if s.r.IntN(2) == 0 {
			t["isStreaming"] = true
		}
		return s.message("assistant", []map[string]any{t})
	case 4, 5, 6:
		var content []map[string]any
		if s.r.IntN(2) == 0 {
			content = append(content, map[string]any{"type": "thinking", "thinking": "hm", "signature": "", "durationMs": s.r.IntN(3)})
		}
		for range 1 + s.r.IntN(3) {
			content = append(content, s.toolUse())
		}
		return s.message("assistant", content)
	case 7, 8:
		return s.message("tool", []map[string]any{{"type": "tool_result", "toolUseId": s.pick(s.uses), "content": fmt.Sprintf("out %d", s.r.IntN(3))}})
	case 9:
		call := s.pick(s.asks)
		head := "Answered"
		if s.r.IntN(2) == 0 {
			head = "Dismissed"
		}
		rec := head + " queued questions (AskUser call " + call + "):\nQ: \"Which?\"\nA: \"this\""
		id := "ask-user-answer:" + call
		if s.r.IntN(2) == 0 {
			id = "ask-user-receipt:" + call
		}
		return s.message("user", []map[string]any{text(rec)}, func(m *protocol.FactoryDroidMessage) { m.ID = id })
	case 10:
		return s.message("system", []map[string]any{text("usage limit")}, func(m *protocol.FactoryDroidMessage) { m.Visibility = protocol.MessageVisibilityUserOnly })
	case 11:
		no := false
		return s.message("user", []map[string]any{text("hidden")}, func(m *protocol.FactoryDroidMessage) { m.IsUserVisible = &no })
	case 12:
		yes := true
		return s.message("assistant", []map[string]any{text("failed")}, func(m *protocol.FactoryDroidMessage) { m.IsError = &yes })
	default:
		return s.message("user", nil)
	}
}

// step changes the messages once and returns those loaded.
func (s *randomSession) step() []protocol.FactoryDroidMessage {
	switch k := s.r.IntN(20); {
	case k < 8 || len(s.all) == 0:
		s.all = append(s.all, s.random())
	case k < 11:
		// The last message streams on, or is the same again.
		last := &s.all[len(s.all)-1]
		if s.r.IntN(3) > 0 {
			fresh := s.random()
			fresh.ID = last.ID
			*last = fresh
		}
	case k < 13:
		// An older page comes, or the older messages are let go of.
		if s.lo > 0 && s.r.IntN(2) == 0 {
			s.lo = max(0, s.lo-1-s.r.IntN(6))
		} else {
			s.lo = min(len(s.all), s.lo+1+s.r.IntN(4))
		}
	case k < 15:
		i := s.lo + s.r.IntN(max(1, len(s.all)-s.lo))
		if i < len(s.all) {
			fresh := s.random()
			fresh.ID = s.all[i].ID
			s.all[i] = fresh
		}
	case k < 16:
		if i := s.lo + s.r.IntN(max(1, len(s.all)-s.lo)); i < len(s.all) {
			s.all = slices.Delete(s.all, i, i+1)
		}
	case k < 17:
		// A message whose id another has.
		m := s.random()
		m.ID = s.all[s.r.IntN(len(s.all))].ID
		s.all = append(s.all, m)
	default:
		// Nothing changed.
	}
	s.lo = min(s.lo, len(s.all))
	return slices.Clone(s.all[s.lo:])
}

// A Builder, build after build as a Session changes, makes what Build made
// before it kept anything; keeps each entry that came out the same; and
// leaves what it returned before as it was.
func TestABuilderMatchesAFreshBuildAsASessionChanges(t *testing.T) {
	for seed := range uint64(40) {
		s := &randomSession{r: rand.New(rand.NewPCG(seed, 7))}
		var b Builder
		var prevGot, prevWant []*Entry
		for step := range 300 {
			msgs := s.step()
			got, want := b.Build(msgs), referenceBuild(msgs)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("seed %d step %d: got %#v\nwant %#v", seed, step, got, want)
			}
			if !reflect.DeepEqual(prevGot, prevWant) {
				t.Fatalf("seed %d step %d: the build before changed", seed, step)
			}
			ids := map[string]int{}
			for _, e := range prevGot {
				ids[e.ID]++
			}
			for i, e := range got {
				for _, old := range prevGot {
					if old.ID == e.ID && ids[e.ID] == 1 && old != e && reflect.DeepEqual(old, e) {
						t.Fatalf("seed %d step %d: entry %d (%s) came out the same but is new", seed, step, i, e.ID)
					}
				}
			}
			prevGot, prevWant = got, referenceBuild(msgs)
		}
	}
}

// Building again when the agent adds a tool call to a long Session makes
// only the entry that changed.
func TestABuilderMakesOnlyTheEntryThatChanged(t *testing.T) {
	s := &randomSession{r: rand.New(rand.NewPCG(1, 2))}
	for len(s.all) < 4000 {
		s.all = append(s.all, s.random())
	}
	var b Builder
	b.Build(s.all)
	allocated := func(fn func()) uint64 {
		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		fn()
		runtime.ReadMemStats(&after)
		return after.TotalAlloc - before.TotalAlloc
	}
	first := allocated(func() { referenceBuild(s.all) })
	use := s.toolUse()
	s.all = append(s.all, s.message("assistant", []map[string]any{use}))
	again := allocated(func() { b.Build(s.all) })
	t.Logf("a fresh build allocates %d KB, building again %d KB", first>>10, again>>10)
	if again*8 > first {
		t.Fatalf("building again allocated %d KB, a fresh build %d KB", again>>10, first>>10)
	}
}
