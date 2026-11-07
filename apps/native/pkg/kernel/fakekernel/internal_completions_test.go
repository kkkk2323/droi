package fakekernel

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// post asks the scripted model what the Kernel would ask it: one chat completion, streamed or not.
func post(t *testing.T, m *scriptedModel, stream bool, messages ...map[string]string) (int, string) {
	t.Helper()
	body, err := json.Marshal(map[string]any{"model": ModelID, "stream": stream, "messages": messages})
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.Post(m.baseURL+"/chat/completions", "application/json", bytes.NewReader(body))
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

// A completion is the call beside a conversation: it is answered from Completions, and it never takes the
// Step a conversation is waiting for. That is the whole reason the two lists are apart — a title request
// that ate the next reply would make a conversation test assert on words it never scripted.
func TestACompletionIsAnsweredFromCompletionsAndTakesNoStep(t *testing.T) {
	m := startScriptedModel(t, Scenario{
		Steps:       []Step{{Text: "the reply"}},
		Completions: []Completion{{Text: `{"title":"deploy the fix"}`}},
	})
	status, answer := post(t, m, false, message("system", "You name a chat."), message("user", "fix the deploy"))
	if status != http.StatusOK {
		t.Fatalf("status %d: %s", status, answer)
	}
	if !strings.Contains(answer, "deploy the fix") {
		t.Fatalf("the completion's answer is not the one scripted: %s", answer)
	}
	if !strings.Contains(answer, `"object":"chat.completion"`) {
		t.Fatalf("a completion must be answered whole, not as chunks: %s", answer)
	}

	// The conversation's own request still gets its step: the completion above did not consume it.
	status, answer = post(t, m, true, message("user", "fix the deploy"))
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
	if !strings.Contains(completions[0].FullPrompt(), "fix the deploy") {
		t.Fatalf("the recorded completion lost the prompt: %+v", completions[0])
	}
}

// A completion nobody scripted is answered, not counted as a miss: once the Kernel implements model.complete,
// every conversation makes one, and a miss would fail every test that never asked about titles.
func TestAnUnscriptedCompletionAnswersATitleAndIsNoMiss(t *testing.T) {
	m := startScriptedModel(t, Scenario{Steps: []Step{{Text: "the reply"}}})
	status, answer := post(t, m, false, message("system", "You name a chat."), message("user", "hello"))
	if status != http.StatusOK || !strings.Contains(answer, "Fake title") {
		t.Fatalf("%d %s", status, answer)
	}
	if misses := m.unmatched(); misses != 0 {
		t.Fatalf("%d unmatched requests: %v", misses, m.unmatchedList())
	}
	var reply struct {
		Title string `json:"title"`
	}
	if err := json.Unmarshal([]byte(extractContent(t, answer)), &reply); err != nil || reply.Title != "Fake title" {
		t.Fatalf("the default answer is not a title the schema takes: %s (%v)", extractContent(t, answer), err)
	}

	// Steps are untouched: the reply is still there for the conversation that asks for it.
	if status, answer = post(t, m, true, message("user", "hello")); status != http.StatusOK || !strings.Contains(answer, "the reply") {
		t.Fatalf("the step was taken: %d %s", status, answer)
	}
}

// extractContent is the assistant text of a whole answer, for tests that read it as JSON.
func extractContent(t *testing.T, answer string) string {
	t.Helper()
	var whole struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal([]byte(answer), &whole); err != nil || len(whole.Choices) == 0 {
		t.Fatalf("not a completion: %s (%v)", answer, err)
	}
	return whole.Choices[0].Message.Content
}

// The instruction that asks for a title lives in the system message, so a Match has to look at the whole
// prompt: matching only the user's words would make every title call look the same.
func TestACompletionMatchesTheSystemMessageToo(t *testing.T) {
	m := startScriptedModel(t, Scenario{
		Completions: []Completion{{Match: "You name a chat", Text: `{"title":"matched"}`}},
	})
	if _, answer := post(t, m, false, message("system", "You name a chat."), message("user", "hello there")); !strings.Contains(answer, "matched") {
		t.Fatalf("the system message was not matched: %s", answer)
	}
}

// Completions are consumed in the order the scenario lists them, first match wins, like Steps.
func TestCompletionsAreTakenInOrderAndOnlyOnce(t *testing.T) {
	m := startScriptedModel(t, Scenario{
		Completions: []Completion{
			{Match: "first", Text: `{"title":"one"}`},
			{Match: "second", Text: `{"title":"two"}`},
		},
	})
	if _, answer := post(t, m, false, message("user", "the first conversation")); !strings.Contains(answer, "one") {
		t.Fatalf("first completion: %s", answer)
	}
	if _, answer := post(t, m, false, message("user", "the second conversation")); !strings.Contains(answer, "two") {
		t.Fatalf("second completion: %s", answer)
	}
	if _, answer := post(t, m, false, message("user", "the first conversation")); !strings.Contains(answer, "Fake title") {
		t.Fatalf("a taken completion was handed out twice: %s", answer)
	}
}

// A completion can fail the way the provider does, which is how a test drives the fallback to the first
// message: the Host must still name the conversation.
func TestACompletionFailureIsTheProvidersFailure(t *testing.T) {
	m := startScriptedModel(t, Scenario{
		Completions: []Completion{{Failure: &Failure{Status: http.StatusInternalServerError, Body: "upstream exploded"}}},
	})
	status, answer := post(t, m, false, message("user", "hello"))
	if status != http.StatusInternalServerError || !strings.Contains(answer, "upstream exploded") {
		t.Fatalf("%d %s", status, answer)
	}
	if misses := m.unmatched(); misses != 0 {
		t.Fatalf("a failed completion is not a miss: %v", m.unmatchedList())
	}
}

// CompletionFallback replaces the default answer, for a test that wants a title of its own.
func TestCompletionFallbackReplacesTheDefaultTitle(t *testing.T) {
	m := startScriptedModel(t, Scenario{CompletionFallback: `{"title":"Custom title"}`})
	if _, answer := post(t, m, false, message("user", "hello")); !strings.Contains(answer, "Custom title") {
		t.Fatalf("%s", answer)
	}
}
