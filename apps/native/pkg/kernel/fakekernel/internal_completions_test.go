package fakekernel

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// post asks the scripted model what the Kernel asks it: one chat completion. call marks it as a
// model.complete request the way the Kernel does, with CallHeader, and stream sends the streamed shape, which
// is the shape every text call has (model.complete included).
func post(t *testing.T, m *scriptedModel, stream, call bool, messages ...map[string]string) (int, string) {
	t.Helper()
	body, err := json.Marshal(map[string]any{"model": ModelID, "stream": stream, "messages": messages})
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, m.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	if call {
		request.Header.Set(CallHeader, CallModelComplete)
	}
	res, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer res.Body.Close()
	answer, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, string(answer)
}

func message(role, text string) map[string]string {
	return map[string]string{"role": role, "content": text}
}

// wholeAnswer is the assistant text of an unstreamed answer, for tests that read it as JSON.
func wholeAnswer(t *testing.T, answer string) (object, text string) {
	t.Helper()
	var whole struct {
		Object  string `json:"object"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal([]byte(answer), &whole); err != nil {
		t.Fatalf("not a completion: %s (%v)", answer, err)
	}
	if len(whole.Choices) == 0 {
		t.Fatalf("no choice in %s", answer)
	}
	return whole.Object, whole.Choices[0].Message.Content
}

// A completion is the call beside a conversation: the Kernel marks it with CallHeader, it is answered from
// Completions in the streamed shape every text call has, and it never takes the Step a conversation waits
// for. That last part is the reason the two lists are apart — a title request that ate the next reply would
// make a conversation test assert on words it never scripted.
func TestACompletionIsAnsweredFromCompletionsAndTakesNoStep(t *testing.T) {
	m := startScriptedModel(t, Scenario{
		Steps:       []Step{{Text: "the reply"}},
		Completions: []Completion{{Text: `{"title":"deploy the fix"}`}},
	})
	status, answer := post(t, m, true, true, message("system", "You name a chat."), message("user", "fix the deploy"))
	if status != http.StatusOK {
		t.Fatalf("status %d: %s", status, answer)
	}
	if !strings.HasPrefix(answer, "data: ") {
		t.Fatalf("a streamed request must be answered as a stream: %s", answer)
	}
	if !strings.Contains(answer, "deploy the fix") {
		t.Fatalf("the completion's answer is not the one scripted: %s", answer)
	}

	// The conversation's own request still gets its step: the completion above did not consume it.
	status, answer = post(t, m, true, false, message("user", "fix the deploy"))
	if status != http.StatusOK || !strings.Contains(answer, "the reply") {
		t.Fatalf("the conversation's reply was taken by the completion: %d %s", status, answer)
	}
	if misses := m.unmatched(); misses != 0 {
		t.Fatalf("%d unmatched requests: %v", misses, m.unmatchedList())
	}
	completions := m.completions()
	if len(completions) != 1 {
		t.Fatalf("%d completions recorded, want 1", len(completions))
	}
	if got := completions[0].Header(CallHeader); got != CallModelComplete {
		t.Fatalf("the recorded completion lost its header: %q", got)
	}
	if !strings.Contains(completions[0].FullPrompt(), "fix the deploy") {
		t.Fatalf("the recorded completion lost the prompt: %+v", completions[0])
	}
}

// A streamed request without the header is a turn, not a completion: the header is what tells them apart,
// since the body of both is streamed.
func TestAnUnmarkedStreamedRequestIsATurn(t *testing.T) {
	m := startScriptedModel(t, Scenario{
		Steps:       []Step{{Text: "the reply"}},
		Completions: []Completion{{Text: `{"title":"never"}`}},
	})
	if _, answer := post(t, m, true, false, message("user", "hello")); !strings.Contains(answer, "the reply") {
		t.Fatalf("an unmarked request is a turn: %s", answer)
	}
	if completions := m.completions(); len(completions) != 0 {
		t.Fatalf("%d completions recorded for a turn", len(completions))
	}
}

// A completion nobody scripted is answered, not counted as a miss: once the Kernel implements model.complete,
// every conversation makes one, and a miss would fail every test that never asked about titles.
func TestAnUnscriptedCompletionAnswersATitleAndIsNoMiss(t *testing.T) {
	m := startScriptedModel(t, Scenario{Steps: []Step{{Text: "the reply"}}})
	status, answer := post(t, m, false, true, message("system", "You name a chat."), message("user", "hello"))
	if status != http.StatusOK {
		t.Fatalf("%d %s", status, answer)
	}
	object, text := wholeAnswer(t, answer)
	if object != "chat.completion" {
		t.Fatalf("an unstreamed request is answered whole, got object %q in %s", object, answer)
	}
	var title struct {
		Title string `json:"title"`
	}
	if err := json.Unmarshal([]byte(text), &title); err != nil || title.Title != "Fake title" {
		t.Fatalf("the default answer is not a title the schema takes: %s (%v)", text, err)
	}
	if misses := m.unmatched(); misses != 0 {
		t.Fatalf("%d unmatched requests: %v", misses, m.unmatchedList())
	}

	// Steps are untouched: the reply is still there for the conversation that asks for it.
	if status, answer = post(t, m, true, false, message("user", "hello")); status != http.StatusOK || !strings.Contains(answer, "the reply") {
		t.Fatalf("the step was taken: %d %s", status, answer)
	}
}

// The instruction that asks for a title lives in the system message, so a Match has to look at the whole
// prompt: matching only the user's words would make every title call look the same.
func TestACompletionMatchesTheSystemMessageToo(t *testing.T) {
	m := startScriptedModel(t, Scenario{
		Completions: []Completion{{Match: "You name a chat", Text: `{"title":"matched"}`}},
	})
	if _, answer := post(t, m, true, true, message("system", "You name a chat."), message("user", "hello there")); !strings.Contains(answer, "matched") {
		t.Fatalf("the system message was not matched: %s", answer)
	}
}

// Completions are consumed in the order the scenario lists them, first match wins, and only once.
func TestCompletionsAreTakenInOrderAndOnlyOnce(t *testing.T) {
	m := startScriptedModel(t, Scenario{
		Completions: []Completion{
			{Match: "first", Text: `{"title":"one"}`},
			{Match: "second", Text: `{"title":"two"}`},
		},
	})
	if _, answer := post(t, m, true, true, message("user", "the first conversation")); !strings.Contains(answer, "one") {
		t.Fatalf("first completion: %s", answer)
	}
	if _, answer := post(t, m, true, true, message("user", "the second conversation")); !strings.Contains(answer, "two") {
		t.Fatalf("second completion: %s", answer)
	}
	if _, answer := post(t, m, true, true, message("user", "the first conversation")); !strings.Contains(answer, "Fake title") {
		t.Fatalf("a taken completion was handed out twice: %s", answer)
	}
}

// A completion can fail the way the provider does, which is how a test drives the fallback to the first
// message: the Host must still name the conversation.
func TestACompletionFailureIsTheProvidersFailure(t *testing.T) {
	m := startScriptedModel(t, Scenario{
		Completions: []Completion{{Failure: &Failure{Status: http.StatusInternalServerError, Body: "upstream exploded"}}},
	})
	status, answer := post(t, m, true, true, message("user", "hello"))
	if status != http.StatusInternalServerError || !strings.Contains(answer, "upstream exploded") {
		t.Fatalf("%d %s", status, answer)
	}
	if misses := m.unmatched(); misses != 0 {
		t.Fatalf("a failed completion is not a miss: %v", m.unmatchedList())
	}
}

// Scenario.Fallback is the one knob a test sets for an answer it did not script, completions included.
func TestScenarioFallbackAnswersACompletion(t *testing.T) {
	m := startScriptedModel(t, Scenario{Fallback: `{"title":"Custom title"}`})
	if _, answer := post(t, m, true, true, message("user", "hello")); !strings.Contains(answer, "Custom title") {
		t.Fatalf("%s", answer)
	}
}

// Every text call the Kernel makes is streamed, which is why a completion cannot be told apart by its body
// and carries CallHeader instead. This pins the premise: model.complete must send that header, because
// nothing about the request's shape says it is one.
func TestEveryModelRequestARealKernelMakesIsStreamed(t *testing.T) {
	k := Start(t, Scenario{Steps: []Step{{Text: "hi"}}})
	c := k.Dial(t)
	id := k.CreateConversation(t, c)
	k.Ask(t, c, id, "hello", "hi", 30*time.Second)
	requests := k.Requests()
	if len(requests) == 0 {
		t.Fatal("the Kernel made no model request")
	}
	for i, request := range requests {
		if !request.Stream {
			t.Errorf("request %d is not streamed: a model.complete call arrives streamed too, so it must carry %s: %s",
				i, CallHeader, CallModelComplete)
		}
	}
}
