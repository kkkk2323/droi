package factory

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
)

// Request reads a body with a scanner of its own: on any JSON, on any route, it does not fail, it answers JSON, and
// canonical keeps the body's value and changes nothing the second time.
func FuzzRequest(f *testing.F) {
	for _, seed := range []string{
		`{"model":"claude-opus-5-5","system":"x","messages":[{"role":"user","content":"\u0068i \"q\" \\ \/"}],"a":{"2":1,"1":[1,2,{"b":null}]},"a":true}`,
		`{"model":"glm-5.3","messages":[{"role":"system","content":null},{"role":"tool","content":[{"type":"text","text":"x"}]}]}`,
		`{"model":"gpt-5.5","service_tier":"priority","input":[{"type":"function_call_output","output":"\ud83d\ude00"}],"temperature":1e-7}`,
		`{"contents":[{"parts":[{"inlineData":{"data":"x"}},{"functionResponse":{"response":{"output":"y"}}}]}],"systemInstruction":"s"}`,
		`[1, "two", {"three": [ ] }, -0.5e+3]`,
		`"just a string"`,
	} {
		for _, path := range []string{messages, count, chat, responses, gemini} {
			f.Add(path, []byte(seed))
		}
	}
	f.Fuzz(func(t *testing.T, path string, body []byte) {
		if !json.Valid(body) {
			return
		}
		_, out, _ := Request(path, body)
		if !json.Valid(out) {
			t.Fatalf("%s: not JSON: %s", path, out)
		}
		once := canonical(body)
		a, errA := decodeNumbers(body)
		b, errB := decodeNumbers(once)
		if errA != nil || errB != nil || !reflect.DeepEqual(a, b) {
			t.Fatalf("canonical changed the value:\n%s\n%s", body, once)
		}
		if twice := canonical(once); string(twice) != string(once) {
			t.Fatalf("canonical changed its own output:\n%s\n%s", once, twice)
		}
	})
}

// decodeNumbers decodes body with its numbers as they are spelled, so one past float64's range is a value too.
func decodeNumbers(body []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var v any
	err := dec.Decode(&v)
	return v, err
}
