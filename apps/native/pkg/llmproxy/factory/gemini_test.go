package factory

import (
	"encoding/json"
	"strings"
	"testing"
)

// Gemini's golden cases: gemini.test.mjs, one subtest per upstream test. The EU case is the proxy's
// (TestAKernelGeminiRequestGoesToFactorysRoute); of the ZCode case only its Gemini half applies to Droi.

const geminiArgon = "/api/llm/g/v1/models/gemini-4-argon:streamGenerateContent"

var geminiRequest = obj{
	"contents":          []any{obj{"role": "user", "parts": []any{obj{"text": "hi"}, obj{"inlineData": obj{"mimeType": "image/png", "data": "aW1n"}}}}},
	"systemInstruction": obj{"role": "user", "parts": []any{obj{"text": "Be brief."}, obj{"text": "Use tools."}}},
	"generationConfig":  obj{"maxOutputTokens": 512, "thinkingConfig": obj{"includeThoughts": true, "thinkingLevel": "high"}},
	"tools":             []any{obj{"functionDeclarations": []any{obj{"name": "Read", "parameters": obj{"type": "object"}}}}},
}

func sendGemini(t *testing.T, path string, body any) (string, obj) {
	t.Helper()
	raw, _ := json.Marshal(body)
	to, out, _ := Request(path, raw)
	var got obj
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("not JSON: %v: %s", err, out)
	}
	return to, got
}

func TestGemini(t *testing.T) {
	t.Run("a Gemini model goes to /api/llm/g/v1/generate, the model in the body, x-api-provider google", func(t *testing.T) {
		to, sent := sendGemini(t, geminiArgon, geminiRequest)
		same(t, "path", to, "/api/llm/g/v1/generate")
		same(t, "model", sent["model"], "gemini-4-argon")
		same(t, "system instruction", sent["systemInstruction"], obj{"parts": []any{obj{"text": DroidLine + "\nBe brief.\nUse tools."}}})
		for _, k := range []string{"contents", "generationConfig", "tools"} {
			same(t, k, sent[k], geminiRequest[k])
		}
		if got := Upstream(to, "gemini-4-argon"); got != "google" {
			t.Fatalf("x-api-provider %q", got)
		}
	})

	t.Run("a Gemini request with no system instruction gets droid's line, one droid opened already goes on as it is", func(t *testing.T) {
		at := "/api/llm/g/v1/models/gemini-3.5-flash:streamGenerateContent"
		contents := geminiRequest["contents"]
		_, sent := sendGemini(t, at, obj{"contents": contents})
		same(t, "none", sent["systemInstruction"], obj{"parts": []any{obj{"text": DroidLine}}})
		own := obj{"contents": contents, "systemInstruction": obj{"parts": []any{obj{"text": DroidLine + "\nmine"}, obj{"inlineData": obj{"mimeType": "image/png", "data": "eA=="}}}}}
		_, sent = sendGemini(t, at, own)
		same(t, "droid's", sent, obj{"contents": contents, "systemInstruction": own["systemInstruction"], "model": "gemini-3.5-flash"})
		_, sent = sendGemini(t, at, obj{"contents": contents, "systemInstruction": "Plain."})
		same(t, "a string", sent["systemInstruction"], obj{"parts": []any{obj{"text": DroidLine + "\nPlain."}}})
		_, sent = sendGemini(t, at, obj{"contents": contents, "systemInstruction": obj{"parts": []any{obj{"text": "A"}, obj{"inlineData": obj{"mimeType": "image/png", "data": "eA=="}}}}})
		same(t, "other parts", sent["systemInstruction"], obj{"parts": []any{obj{"text": DroidLine + "\nA"}, obj{"inlineData": obj{"mimeType": "image/png", "data": "eA=="}}}})
	})

	t.Run("Gemini tool output and system instruction escape every refused phrase", func(t *testing.T) {
		env := toolOnlyPhrases[3]
		body := obj{
			"contents":          []any{obj{"role": "user", "parts": []any{obj{"functionResponse": obj{"name": "Read", "response": obj{"text": env + " / " + piOpeningSentence}}}}}},
			"systemInstruction": obj{"role": "user", "parts": []any{obj{"text": env}}},
		}
		_, sent := sendGemini(t, geminiArgon, body)
		same(t, "tool output", str(sent, "contents", 0, "parts", 0, "functionResponse", "response", "text"), escapeFirst(env)+" / "+escapedOpening)
		if !strings.Contains(str(sent, "systemInstruction", "parts", 0, "text"), escapeFirst(env)) {
			t.Fatalf("system instruction: %v", sent["systemInstruction"])
		}
		raw, _ := json.Marshal(sent)
		if strings.Contains(string(raw), env) || strings.Contains(string(raw), piOpeningSentence) {
			t.Fatal("a refused phrase is still in the body")
		}
	})

	t.Run("adapts ZCode generated Gemini system context while preserving values and user content", func(t *testing.T) {
		// Droi's half: a refused phrase in a user part is escaped, and the request sent again goes as it came.
		context := "# Environment\n" + toolOnlyPhrases[3] + "\n- Platform: darwin\n"
		url := "/api/llm/g/v1/models/gemini-3-flash-preview:generateContent"
		to, sent := sendGemini(t, url, obj{"systemInstruction": obj{"parts": []any{obj{"text": "Be brief."}}}, "contents": []any{obj{"role": "user", "parts": []any{obj{"text": context}}}}})
		same(t, "user parts", sent["contents"], []any{obj{"role": "user", "parts": []any{obj{"text": strings.Replace(context, toolOnlyPhrases[3], escapeFirst(toolOnlyPhrases[3]), 1)}}}})
		raw, _ := json.Marshal(sent)
		again, out, _ := Request(to, raw)
		var resent obj
		_ = json.Unmarshal(out, &resent)
		same(t, "sent again", resent, sent)
		same(t, "its route", again, to)
	})
}
