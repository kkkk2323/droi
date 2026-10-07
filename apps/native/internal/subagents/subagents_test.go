package subagents

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/kkkk2323/droi/apps/native/internal/sessions"
	"github.com/kkkk2323/droi/apps/native/internal/transcript"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/protocol"
)

func summary(id string, edit func(*sessions.Summary)) sessions.Summary {
	s := sessions.Summary{SessionID: id, Title: "t"}
	if edit != nil {
		edit(&s)
	}
	return s
}

func ids(list []sessions.Summary) []string {
	out := []string{}
	for _, s := range list {
		out = append(out, s.SessionID)
	}
	return out
}

func task(content string, isError bool, input map[string]any) *transcript.ToolCall {
	call := &transcript.ToolCall{Use: protocol.ToolUse{Type: "tool_use", ID: "call", Name: "Task"}, Input: input}
	if content != "" {
		raw, _ := json.Marshal(content)
		call.Result = &protocol.ToolResult{Type: "tool_result", ToolUseID: "call", Content: raw}
		if isError {
			call.Result.IsError = &isError
		}
	}
	return call
}

func run(status Status) Run { return Run{Status: status} }

func TestSubagentsInTheList(t *testing.T) {
	main := summary("main", func(s *sessions.Summary) { s.Title = "Main" })
	older := summary("a", func(s *sessions.Summary) { s.CallingSessionID, s.UpdatedAt = "main", 1 })
	newer := summary("b", func(s *sessions.Summary) { s.CallingSessionID, s.UpdatedAt = "main", 2 })
	nested := summary("c", func(s *sessions.Summary) { s.CallingSessionID, s.Title = "b", "Nested" })
	list := []sessions.Summary{main, older, newer, nested}

	t.Run("Of lists the direct subagents of the given callers, newest first", func(t *testing.T) {
		if got := ids(Of(list, []string{"main"})); !reflect.DeepEqual(got, []string{"b", "a"}) {
			t.Errorf("got %v", got)
		}
		if got := ids(Of(list, []string{"b"})); !reflect.DeepEqual(got, []string{"c"}) {
			t.Errorf("got %v", got)
		}
	})

	t.Run("CallerTrail walks up to the main Session, and names a caller the list lacks", func(t *testing.T) {
		want := []SessionRef{{"main", "Main"}, {"b", "t"}}
		if got := CallerTrail(list, "c", "b"); !reflect.DeepEqual(got, want) {
			t.Errorf("got %v", got)
		}
		if got := CallerTrail(list, "main", ""); len(got) != 0 {
			t.Errorf("main Session trail = %v", got)
		}
		want = []SessionRef{{"gone", "Main session"}}
		if got := CallerTrail(nil, "x", "gone"); !reflect.DeepEqual(got, want) {
			t.Errorf("got %v", got)
		}
	})

	t.Run("a caller continued after a compaction leads back to its latest link", func(t *testing.T) {
		main2 := summary("main2", func(s *sessions.Summary) { s.Title, s.ParentID = "Main", "main" })
		main3 := summary("main3", func(s *sessions.Summary) { s.Title, s.ParentID = "Main again", "main2" })
		late := summary("d", func(s *sessions.Summary) { s.CallingSessionID, s.UpdatedAt = "main3", 3 })
		all := append(append([]sessions.Summary{}, list...), main2, main3, late)
		want := []SessionRef{{"main3", "Main again"}, {"b", "t"}}
		if got := CallerTrail(all, "c", "b"); !reflect.DeepEqual(got, want) {
			t.Errorf("got %v", got)
		}
		if got := ids(Siblings(all, "main")); !reflect.DeepEqual(got, []string{"d", "b", "a"}) {
			t.Errorf("siblings of a = %v", got)
		}
		if got := ids(Siblings(all, "b")); !reflect.DeepEqual(got, []string{"c"}) {
			t.Errorf("siblings of c = %v", got)
		}
		if got := Siblings(all, ""); len(got) != 0 {
			t.Errorf("siblings of main = %v", got)
		}
	})

	t.Run("a subagent belongs to its main Session's row, at the latest compaction", func(t *testing.T) {
		continued := summary("main2", func(s *sessions.Summary) { s.ParentID = "main" })
		if got := ListedSessionOf(append(list[:4:4], continued), "c"); got != "main2" {
			t.Errorf("got %q", got)
		}
		if got := ListedSessionOf(list, "main"); got != "main" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("RunningCounts counts running subagents per listed row", func(t *testing.T) {
		runs := map[string]Run{"a": run(Completed), "b": run(Running), "c": run(Pending)}
		if got := RunningCounts(list, runs); !reflect.DeepEqual(got, map[string]int{"main": 2}) {
			t.Errorf("got %v", got)
		}
	})
}

func TestTaskCalls(t *testing.T) {
	t.Run("reads the request from the input", func(t *testing.T) {
		got := TaskRequest(task("", false, map[string]any{"subagent_type": "explorer", "description": "Map it", "prompt": "Go"}))
		if got != (Request{"explorer", "Map it", "Go"}) {
			t.Errorf("got %+v", got)
		}
		if SubagentName("explorer") != "Explorer" || SubagentName("") != "Subagent" {
			t.Error("SubagentName")
		}
	})

	t.Run("a background launch names the subagent's Session and is no report", func(t *testing.T) {
		launch := task("Task launched in background.\ntask_id: s-1\nsession_id: s-1\nsubagent_type: explorer", false, nil)
		if got := LaunchedSessionID(launch); got != "s-1" {
			t.Errorf("session = %q", got)
		}
		if got := StateOf(launch, nil); got != Launched {
			t.Errorf("state = %q", got)
		}
		if got := Report(launch); got != "" {
			t.Errorf("report = %q", got)
		}
	})

	t.Run("the Daemon's account wins over the tool result, except for a failed call", func(t *testing.T) {
		completed, running := run(Completed), run(Running)
		for _, c := range []struct {
			call *transcript.ToolCall
			run  *Run
			want TaskState
		}{
			{task("", false, nil), nil, "running"},
			{task("report", false, nil), nil, "completed"},
			{task("Task launched in background.", false, nil), &completed, "completed"},
			{task("boom", true, nil), &running, "failed"},
		} {
			if got := StateOf(c.call, c.run); got != c.want {
				t.Errorf("StateOf = %q, want %q", got, c.want)
			}
		}
		if got := Report(task("report", false, nil)); got != "report" {
			t.Errorf("report = %q", got)
		}
	})

	t.Run("formats run durations", func(t *testing.T) {
		for ms, want := range map[float64]string{4_200: "4s", 134_000: "2m 14s", 3_900_000: "1h 5m"} {
			if got := FormatRunDuration(ms); got != want {
				t.Errorf("FormatRunDuration(%v) = %q, want %q", ms, got, want)
			}
		}
	})
}

func TestLinkFor(t *testing.T) {
	call := task("Task launched in background.\nsession_id: s-9", false, nil)
	running := run(Running)
	if l := LinkFor(call, nil); l.SessionID != "" || l.State != Launched {
		t.Errorf("without links: %+v", l)
	}
	links := &Links{ByToolUse: map[string]sessions.Summary{}, Runs: map[string]Run{"s-9": running}}
	if l := LinkFor(call, links); l.SessionID != "s-9" || l.State != "running" || l.Run == nil {
		t.Errorf("launched: %+v", l)
	}
	links.ByToolUse["call"] = summary("s-1", nil)
	if l := LinkFor(call, links); l.SessionID != "s-1" || l.Run != nil {
		t.Errorf("listed: %+v", l)
	}
}

func TestRunFrom(t *testing.T) {
	done := Run{Status: Completed}
	if r, ok := RunFrom(&done, "idle"); !ok || r.Status != Completed {
		t.Errorf("summary: %v %v", r, ok)
	}
	if r, ok := RunFrom(nil, "streaming"); !ok || r.Status != Running {
		t.Errorf("working: %v %v", r, ok)
	}
	if _, ok := RunFrom(nil, "idle"); ok {
		t.Error("idle Session without a summary has no run")
	}
}

func TestUnlistedTaskCalls(t *testing.T) {
	entry := func(call *transcript.ToolCall) []*transcript.Entry {
		return []*transcript.Entry{{ID: "e", Blocks: []transcript.Block{{Kind: transcript.Subagent, ID: "e:0", Call: call}}}}
	}
	listed := map[string]sessions.Summary{"call": summary("s", nil)}
	for _, c := range []struct {
		entries []*transcript.Entry
		by      map[string]sessions.Summary
		want    string
	}{
		{entry(task("", false, nil)), listed, ""},
		{entry(task("", false, nil)), nil, "call"},
		{entry(task("session_id: s", false, nil)), nil, "call:answered"},
	} {
		if got := UnlistedTaskCalls(c.entries, c.by); got != c.want {
			t.Errorf("got %q, want %q", got, c.want)
		}
	}
}
