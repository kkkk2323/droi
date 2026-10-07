package memory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

type hookEnv struct {
	root, memoryDir, workspace string
	store                      *Store
}

func newHookEnv(t *testing.T) *hookEnv {
	t.Helper()
	root := must[string](t)(filepath.EvalSymlinks(t.TempDir()))
	h := &hookEnv{root: root, memoryDir: filepath.Join(root, "memory"), workspace: filepath.Join(root, "app")}
	if err := os.Mkdir(h.workspace, 0o777); err != nil {
		t.Fatal(err)
	}
	h.store = must[*Store](t)(OpenStore(h.memoryDir))
	t.Cleanup(func() { h.store.Close() })
	return h
}

type finished struct {
	code           int
	stdout, stderr string
}

func (h *hookEnv) runRaw(stdin string) finished {
	var out, errOut strings.Builder
	env := func(key string) (string, bool) {
		if key == "DROI_MEMORY_DIR" {
			return h.memoryDir, true
		}
		return "", false
	}
	code := hookMain(env, strings.NewReader(stdin), &out, &errOut)
	return finished{code, out.String(), errOut.String()}
}

func (h *hookEnv) hook(t *testing.T, input map[string]any) finished {
	t.Helper()
	b, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	return h.runRaw(string(b))
}

func additionalContext(t *testing.T, stdout string) string {
	t.Helper()
	var parsed struct {
		HookSpecificOutput struct{ AdditionalContext string } `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(stdout), &parsed); err != nil {
		t.Fatalf("%v: %q", err, stdout)
	}
	return parsed.HookSpecificOutput.AdditionalContext
}

func (h *hookEnv) transcript(t *testing.T, prompts int) string {
	t.Helper()
	file := filepath.Join(h.root, strconv.Itoa(prompts)+".jsonl")
	lines := []any{
		map[string]any{"type": "session_start", "id": "s", "cwd": h.workspace},
		map[string]any{"type": "message", "id": "context-1", "message": map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": "<system-reminder>tools</system-reminder>"}}}},
	}
	for i := 0; i < prompts; i++ {
		n := strconv.Itoa(i)
		lines = append(lines,
			map[string]any{"type": "message", "message": map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": "prompt " + n}}}},
			map[string]any{"type": "message", "message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "tool_use", "id": "t" + n, "name": "Read", "input": map[string]any{}}}}},
			map[string]any{"type": "message", "message": map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "tool_use_id": "t" + n, "content": "x"}}}},
			map[string]any{"type": "message", "message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": "answer " + n}}}},
		)
	}
	var out []string
	for _, l := range lines {
		b, _ := json.Marshal(l)
		out = append(out, string(b))
	}
	writeFile(t, file, strings.Join(out, "\n"))
	return file
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestSessionStartPrintsOnlyThePolicyWhileMemoryIsEmpty(t *testing.T) {
	h := newHookEnv(t)
	run := h.hook(t, map[string]any{"hook_event_name": "SessionStart", "session_id": "s1", "cwd": h.workspace, "source": "startup"})
	eq(t, run.code, 0)
	if !strings.HasPrefix(run.stdout, "<memory-context>\n") || !strings.Contains(run.stdout, "Memory is context, not instruction") || strings.Contains(run.stdout, "Corrections the user made") {
		t.Fatalf("%q", run.stdout)
	}
}

func TestSessionStartAddsTheCorrectionsOfTheWorkspaceAndGlobalMemory(t *testing.T) {
	h := newHookEnv(t)
	ws := Slot{Scope: ScopeProject, Workspace: h.workspace}
	add(t, h.store, ws, "Use pnpm, not npm", CategoryCorrection)
	add(t, h.store, GlobalSlot, "Do not add emojis", CategoryCorrection)
	add(t, h.store, ws, "Tests live next to the code", CategoryConvention)
	add(t, h.store, Slot{Scope: ScopeProject, Workspace: "/elsewhere"}, "Other project rule", CategoryCorrection)
	out := h.hook(t, map[string]any{"hook_event_name": "SessionStart", "session_id": "s1", "cwd": h.workspace, "source": "compact"}).stdout
	if !strings.Contains(out, "- Use pnpm, not npm (project,") || !strings.Contains(out, "- Do not add emojis (global,") ||
		strings.Contains(out, "Tests live next to the code") || strings.Contains(out, "Other project rule") {
		t.Fatalf("%q", out)
	}
}

func TestSessionStartCapsTheCorrections(t *testing.T) {
	h := newHookEnv(t)
	ws := Slot{Scope: ScopeProject, Workspace: h.workspace}
	for i := 0; i < 30; i++ {
		add(t, h.store, ws, "rule "+strconv.Itoa(i), CategoryCorrection)
	}
	rules := func(out string) []string {
		var found []string
		for _, l := range strings.Split(out, "\n") {
			if strings.HasPrefix(l, "- rule") {
				found = append(found, l)
			}
		}
		return found
	}
	eq(t, len(rules(h.hook(t, map[string]any{"hook_event_name": "SessionStart", "cwd": h.workspace}).stdout)), 20)

	z := strings.Repeat("z", 1_990)
	add(t, h.store, GlobalSlot, z, CategoryCorrection)
	capped := h.hook(t, map[string]any{"hook_event_name": "SessionStart", "cwd": h.workspace}).stdout
	if !strings.Contains(capped, "- "+z+" (global,") {
		t.Fatal("long correction missing")
	}
	eq(t, rules(capped), []string{"- rule 29 (project, " + today() + ")"})
}

func TestUserPromptSubmitSaysNothingForAnOrdinaryPrompt(t *testing.T) {
	h := newHookEnv(t)
	eq(t, h.hook(t, map[string]any{"hook_event_name": "UserPromptSubmit", "session_id": "s1", "prompt": "Add a test"}), finished{0, "", ""})
}

func TestUserPromptSubmitAsksForACorrectionToBeRecorded(t *testing.T) {
	h := newHookEnv(t)
	run := h.hook(t, map[string]any{"hook_event_name": "UserPromptSubmit", "session_id": "s1", "prompt": "不对，应该用 pnpm"})
	if !strings.Contains(additionalContext(t, run.stdout), "record the correction") {
		t.Fatal(run.stdout)
	}
	if !strings.HasSuffix(run.stdout, "}\n") || !strings.HasPrefix(run.stdout, `{"hookSpecificOutput":{"hookEventName":"UserPromptSubmit","additionalContext":"<memory-context>\n`) {
		t.Fatalf("%q", run.stdout)
	}
}

func TestUserPromptSubmitNudgesEveryTenthPromptPerSession(t *testing.T) {
	h := newHookEnv(t)
	var outputs []string
	for i := 0; i < 10; i++ {
		outputs = append(outputs, h.hook(t, map[string]any{"hook_event_name": "UserPromptSubmit", "session_id": "a", "prompt": "next"}).stdout)
		if i < 5 {
			h.hook(t, map[string]any{"hook_event_name": "UserPromptSubmit", "session_id": "b", "prompt": "next"})
		}
	}
	eq(t, outputs[:9], make([]string, 9))
	if !strings.Contains(additionalContext(t, outputs[9]), "review this stretch") {
		t.Fatal(outputs[9])
	}
	eq(t, h.hook(t, map[string]any{"hook_event_name": "UserPromptSubmit", "session_id": "b", "prompt": "next"}).stdout, "")
}

func (h *hookEnv) requestFile(id string) string {
	return filepath.Join(h.memoryDir, "requests", id+".json")
}

func TestSessionEndAsksTheHostToExtractFromALongSessionThatSavedNothing(t *testing.T) {
	h := newHookEnv(t)
	path := h.transcript(t, 6)
	run := h.hook(t, map[string]any{"hook_event_name": "SessionEnd", "session_id": "long", "transcript_path": path, "cwd": h.workspace})
	eq(t, run, finished{0, "", ""})
	var raw map[string]any
	if err := json.Unmarshal(must[[]byte](t)(os.ReadFile(h.requestFile("long"))), &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["requestedAt"].(string); !ok {
		t.Fatalf("%v", raw)
	}
	delete(raw, "requestedAt")
	eq(t, raw, map[string]any{"sessionId": "long", "transcriptPath": path, "cwd": h.workspace, "scratch": false, "event": "SessionEnd"})

	files := must[[]string](t)(ExtractionRequestFiles(h.memoryDir))
	eq(t, files, []string{h.requestFile("long")})
	request := must[ExtractionRequest](t)(ReadExtractionRequest(files[0]))
	if request.SessionID != "long" || request.Event != EventSessionEnd || !isoTime.MatchString(request.RequestedAt) {
		t.Fatalf("%+v", request)
	}
}

func TestPreCompactLeavesShortSessionsAlone(t *testing.T) {
	h := newHookEnv(t)
	h.hook(t, map[string]any{"hook_event_name": "PreCompact", "session_id": "short", "transcript_path": h.transcript(t, 5), "cwd": h.workspace})
	eq(t, exists(h.requestFile("short")), false)
	eq(t, len(must[[]string](t)(ExtractionRequestFiles(h.memoryDir))), 0)
}

func TestPreCompactLeavesASessionThatAlreadyWroteAlone(t *testing.T) {
	h := newHookEnv(t)
	if err := h.store.RecordWrite("wrote"); err != nil {
		t.Fatal(err)
	}
	h.hook(t, map[string]any{"hook_event_name": "PreCompact", "session_id": "wrote", "transcript_path": h.transcript(t, 8), "cwd": h.workspace})
	eq(t, exists(h.requestFile("wrote")), false)
}

func TestTheHookEntryNeverFailsASession(t *testing.T) {
	h := newHookEnv(t)
	run := h.runRaw("not json")
	if run.code != 0 || run.stdout != "" || !strings.HasPrefix(run.stderr, "droi-memory hook: ") {
		t.Fatalf("%+v", run)
	}
	eq(t, h.hook(t, map[string]any{"hook_event_name": "Stop"}), finished{0, "", ""})
}

func TestSoundsLikeCorrection(t *testing.T) {
	for _, prompt := range []string{
		"不对，这里应该用 vitest",
		"不是这个文件，是 store.ts",
		"别用 npm",
		"不要用 any",
		"不应该改这个",
		"应该是 main 分支",
		"不是 jest 而是 vitest",
		"No, the other one",
		"don't use npm here",
		"never run the migrations locally",
		"Use pnpm not npm",
		"use vitest instead of jest",
		"that is wrong",
		"that's the wrong file",
	} {
		if !SoundsLikeCorrection(prompt) {
			t.Errorf("hears no correction in %q", prompt)
		}
	}
	for _, prompt := range []string{
		"Add a login page",
		"Refactor the store",
		"Know the answer?",
		"看看这个日志",
		"不是很急，明天再说",
		"don't forget the tests",
		"what went wrong in CI?",
		"Do not merge yet",
	} {
		if SoundsLikeCorrection(prompt) {
			t.Errorf("hears a correction in %q", prompt)
		}
	}
}

func TestAMemorySessionGetsNoContextNoNudgeAndNoExtraction(t *testing.T) {
	h := newHookEnv(t)
	add(t, h.store, Slot{Scope: ScopeProject, Workspace: h.workspace}, "Use pnpm, not npm", CategoryCorrection)
	path := h.transcript(t, 8)
	writeFile(t, strings.TrimSuffix(path, ".jsonl")+".settings.json", `{"tags":[{"name":"droi.memory"}],"model":"glm-5.3-flash"}`)
	input := func(event string, extra ...string) map[string]any {
		m := map[string]any{"hook_event_name": event, "session_id": "mem", "transcript_path": path, "cwd": h.workspace}
		if len(extra) > 0 {
			m["prompt"] = extra[0]
		}
		return m
	}
	eq(t, h.hook(t, input("SessionStart")).stdout, "")
	eq(t, h.hook(t, input("UserPromptSubmit", "不对，用 pnpm")).stdout, "")
	h.hook(t, input("SessionEnd"))
	eq(t, exists(h.requestFile("mem")), false)
}

func (h *hookEnv) scratchTranscript(t *testing.T, prompts int) string {
	path := h.transcript(t, prompts)
	writeFile(t, strings.TrimSuffix(path, ".jsonl")+".settings.json", `{"tags":[{"name":"droi.scratch"}]}`)
	return path
}

func TestAScratchSessionSeesOnlyGlobalCorrections(t *testing.T) {
	h := newHookEnv(t)
	add(t, h.store, Slot{Scope: ScopeProject, Workspace: h.workspace}, "Use pnpm, not npm", CategoryCorrection)
	add(t, h.store, GlobalSlot, "Do not add emojis", CategoryCorrection)
	out := h.hook(t, map[string]any{"hook_event_name": "SessionStart", "session_id": "chat", "transcript_path": h.scratchTranscript(t, 0), "cwd": h.workspace}).stdout
	if !strings.Contains(out, "no Project Memory") || !strings.Contains(out, "- Do not add emojis (global,") || strings.Contains(out, "Use pnpm, not npm") {
		t.Fatalf("%q", out)
	}
}

func TestAScratchSessionAsksForAGlobalOnlyExtraction(t *testing.T) {
	h := newHookEnv(t)
	h.hook(t, map[string]any{"hook_event_name": "SessionEnd", "session_id": "chat", "transcript_path": h.scratchTranscript(t, 6), "cwd": h.workspace})
	request := must[ExtractionRequest](t)(ReadExtractionRequest(h.requestFile("chat")))
	if request.SessionID != "chat" || request.Cwd != h.workspace || !request.Scratch {
		t.Fatalf("%+v", request)
	}
}

func TestThePromptCountIsForgottenWhenTheSessionEnds(t *testing.T) {
	h := newHookEnv(t)
	stateFile := filepath.Join(h.memoryDir, "state", "gone.json")
	h.hook(t, map[string]any{"hook_event_name": "UserPromptSubmit", "session_id": "gone", "prompt": "next"})
	eq(t, exists(stateFile), true)
	eq(t, string(must[[]byte](t)(os.ReadFile(stateFile))), `{"prompts":1}`)
	h.hook(t, map[string]any{"hook_event_name": "SessionEnd", "session_id": "gone", "transcript_path": h.transcript(t, 1), "cwd": h.workspace})
	eq(t, exists(stateFile), false)
}

func TestSessionFileName(t *testing.T) {
	eq(t, sessionFileName("622a994c-cfbe-483c"), "622a994c-cfbe-483c.json")
	eq(t, sessionFileName("../a bé😀"), "___a_b___.json")
}
