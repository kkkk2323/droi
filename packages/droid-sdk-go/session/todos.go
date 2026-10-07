package session

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"github.com/kkkk2323/droi/packages/droid-sdk-go/protocol"
)

const todoWriteTool = "TodoWrite"

// TodoStatus is the state of a todo item.
type TodoStatus string

const (
	TodoPending    TodoStatus = "pending"
	TodoInProgress TodoStatus = "in_progress"
	TodoCompleted  TodoStatus = "completed"
)

// TodoItem is one entry of the Session's task list, read from the latest
// successful TodoWrite tool call.
type TodoItem struct {
	ID       string
	Content  string
	Status   TodoStatus
	Priority string // high, medium or low; high when the agent gave none
}

// todoState follows TodoWrite tool uses. Each tool use gets a sequence
// number when first seen, so a replayed success result for an older
// TodoWrite cannot roll the list back.
type todoState struct {
	uses       map[string]protocol.ToolUse
	seq        map[string]int
	next       int
	current    []TodoItem
	currentSeq int
}

func newTodoState() todoState {
	return todoState{uses: map[string]protocol.ToolUse{}, seq: map[string]int{}, currentSeq: -1}
}

func (t *todoState) track(tu protocol.ToolUse) {
	if tu.Name != todoWriteTool {
		return
	}
	t.uses[tu.ID] = tu
	if _, ok := t.seq[tu.ID]; !ok {
		t.seq[tu.ID] = t.next
		t.next++
	}
}

func (t *todoState) trackMessage(m message) {
	for _, b := range m.Content {
		if b.Type == blockToolUse {
			t.track(decode[protocol.ToolUse](b))
		}
	}
}

// onResult applies a tool result; it reports whether the list changed.
func (t *todoState) onResult(toolUseID string, isError bool) bool {
	if isError {
		return false
	}
	tu, ok := t.uses[toolUseID]
	if !ok {
		return false
	}
	seq, ok := t.seq[toolUseID]
	if !ok {
		seq = -1
	}
	if seq < t.currentSeq {
		return false
	}
	todos := todosOf(tu)
	if todos == nil {
		return false
	}
	t.current, t.currentSeq = todos, seq
	return true
}

func (t *todoState) applyMessage(m message) bool {
	t.trackMessage(m)
	changed := false
	for _, b := range m.Content {
		if r, ok := toolResultOf(b); ok {
			changed = t.onResult(r.ToolUseID, r.IsError) || changed
		}
	}
	return changed
}

func (t *todoState) rebuild(msgs []message) {
	*t = newTodoState()
	for _, m := range msgs {
		t.trackMessage(m)
	}
	if tu, ok := latestTodoWrite(msgs); ok {
		t.current = todosOf(tu)
		t.currentSeq = t.seq[tu.ID]
	}
}

// latestTodoWrite finds the newest TodoWrite that succeeded, or failing
// that the newest one still waiting for its result.
func latestTodoWrite(msgs []message) (protocol.ToolUse, bool) {
	succeeded, failed := map[string]bool{}, map[string]bool{}
	var unresolved *protocol.ToolUse
	for i := len(msgs) - 1; i >= 0; i-- {
		content := msgs[i].Content
		for j := len(content) - 1; j >= 0; j-- {
			b := content[j]
			if r, ok := toolResultOf(b); ok {
				if r.IsError {
					failed[r.ToolUseID] = true
				} else {
					succeeded[r.ToolUseID] = true
				}
				continue
			}
			if b.Type != blockToolUse {
				continue
			}
			tu := decode[protocol.ToolUse](b)
			if todosOf(tu) == nil {
				continue
			}
			if !failed[tu.ID] && unresolved == nil {
				unresolved = &tu
			}
			if succeeded[tu.ID] {
				return tu, true
			}
		}
	}
	if unresolved != nil {
		return *unresolved, true
	}
	return protocol.ToolUse{}, false
}

// todosOf reads a TodoWrite's list; nil when it is not one or is empty.
func todosOf(tu protocol.ToolUse) []TodoItem {
	if tu.Name != todoWriteTool {
		return nil
	}
	raw, ok := tu.Input["todos"]
	if !ok {
		return nil
	}
	todos := ParseTodos(raw)
	if len(todos) == 0 {
		return nil
	}
	return todos
}

// ParseTodos reads a TodoWrite "todos" argument in any form agents write
// it: an array of items, an array of lines, a JSON string of either, or
// Markdown-style lines ("1. [completed] Fix it", "- [x] Fix it", ...).
func ParseTodos(raw json.RawMessage) []TodoItem {
	if items, ok := parseTodoArray(raw); ok {
		return items
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return nil
	}
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "[") {
		if items, ok := parseTodoArray(json.RawMessage(s)); ok {
			return items
		}
	}
	return parseTodoLines(s)
}

func parseTodoArray(raw json.RawMessage) ([]TodoItem, bool) {
	var elems []json.RawMessage
	if json.Unmarshal(raw, &elems) != nil {
		return nil, false
	}
	if len(elems) > 0 {
		if _, ok := jsonString(elems[0]); ok {
			lines := make([]string, 0, len(elems))
			for _, e := range elems {
				s, _ := jsonString(e)
				lines = append(lines, s)
			}
			return parseTodoLines(strings.Join(lines, "\n")), true
		}
	}
	var items []TodoItem
	for _, e := range elems {
		var obj map[string]json.RawMessage
		if json.Unmarshal(e, &obj) != nil {
			continue
		}
		content, ok := jsonString(obj["content"])
		status, _ := jsonString(obj["status"])
		if !ok || !validStatus(TodoStatus(status)) {
			continue
		}
		id, ok := jsonString(obj["id"])
		if !ok {
			id = strconv.Itoa(len(items) + 1)
		}
		priority, _ := jsonString(obj["priority"])
		if priority != "high" && priority != "medium" && priority != "low" {
			priority = "high"
		}
		items = append(items, TodoItem{ID: id, Content: content, Status: TodoStatus(status), Priority: priority})
	}
	return items, true
}

func jsonString(raw json.RawMessage) (string, bool) {
	var s string
	if len(raw) == 0 || raw[0] != '"' || json.Unmarshal(raw, &s) != nil {
		return "", false
	}
	return s, true
}

func validStatus(s TodoStatus) bool {
	return s == TodoPending || s == TodoInProgress || s == TodoCompleted
}

const todoPrefix = `^(?:(?:\d+[.)]\s*)|(?:[-*]\s+))?`

var (
	todoStatusRe    = regexp.MustCompile(todoPrefix + `\[(completed|in_progress|pending)\]\s*(.*)$`)
	todoCheckedRe   = regexp.MustCompile(todoPrefix + `\[[xX]\]\s*(.*)$`)
	todoUncheckedRe = regexp.MustCompile(todoPrefix + `\[\s*\]\s*(.*)$`)
	todoNumberedRe  = regexp.MustCompile(`^\d+[.)]\s+(.+)$`)
	todoBulletRe    = regexp.MustCompile(`^[-*]\s+(.+)$`)
)

func parseTodoLines(s string) []TodoItem {
	var items []TodoItem
	for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		item := TodoItem{ID: strconv.Itoa(len(items) + 1), Status: TodoPending, Priority: "high"}
		described := func(text string) string {
			if text = strings.TrimSpace(text); text == "" {
				return "(no description)"
			}
			return text
		}
		if m := todoStatusRe.FindStringSubmatch(line); m != nil {
			item.Status, item.Content = TodoStatus(m[1]), described(m[2])
		} else if m := todoCheckedRe.FindStringSubmatch(line); m != nil {
			item.Status, item.Content = TodoCompleted, described(m[1])
		} else if m := todoUncheckedRe.FindStringSubmatch(line); m != nil {
			item.Content = described(m[1])
		} else if m := todoNumberedRe.FindStringSubmatch(line); m != nil {
			item.Content = strings.TrimSpace(m[1])
		} else if m := todoBulletRe.FindStringSubmatch(line); m != nil {
			item.Content = strings.TrimSpace(m[1])
		} else {
			item.Content = line
		}
		items = append(items, item)
	}
	return items
}
