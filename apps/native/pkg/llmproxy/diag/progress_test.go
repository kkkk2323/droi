package diag

import (
	"testing"
	"time"
)

// A stream's first generated token sets its time, and the tokens generated
// are estimated from the text until the upstream counts them.
func TestUsageObserver_ProgressOfAnAnthropicStream(t *testing.T) {
	o := NewUsageObserver("text/event-stream")
	at := time.Unix(100, 0)
	o.now = func() time.Time { return at }
	o.Write([]byte(`data: {"type":"message_start","message":{"usage":{"input_tokens":10,"cache_read_input_tokens":90,"output_tokens":1}}}` + "\n\n"))
	if first, _, u := o.Progress(); !first.IsZero() || u.Input != 100 || u.CacheRead != 90 {
		t.Fatalf("before any text: first %v, usage %+v", first, u)
	}
	o.Write([]byte(`data: {"type":"content_block_delta","delta":{"type":"thinking_delta","thinking":"abcdefgh"}}` + "\n\n"))
	at = time.Unix(105, 0)
	o.Write([]byte(`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"你好"}}` + "\n\n"))
	first, generated, _ := o.Progress()
	if !first.Equal(time.Unix(100, 0)) {
		t.Fatalf("first token at %v, want the thinking's time", first)
	}
	if generated != 4 {
		t.Fatalf("generated %v, want 2 for 8 ASCII characters and 2 for 2 Chinese ones", generated)
	}
	o.Write([]byte(`data: {"type":"message_delta","usage":{"output_tokens":57}}` + "\n\n"))
	if _, generated, _ := o.Progress(); generated != 57 {
		t.Fatalf("generated %v after the count, want 57", generated)
	}
}

func TestGeneratedTextOfEachShape(t *testing.T) {
	for frame, want := range map[string]string{
		`{"type":"content_block_delta","delta":{"type":"input_json_delta","partial_json":"{\"a\":"}}`: `{"a":`,
		`{"choices":[{"delta":{"content":"hi","tool_calls":[{"function":{"arguments":"{}"}}]}}]}`:     "hi{}",
		`{"choices":[{"delta":{"reasoning_content":"think"}}]}`:                                       "think",
		`{"type":"response.output_text.delta","delta":"out"}`:                                         "out",
		`{"type":"response.function_call_arguments.delta","delta":"{}"}`:                              "{}",
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}`:     "",
		`{"type":"response.completed","response":{"usage":{"input_tokens":1}}}`:                       "",
	} {
		if got := generatedText([]byte(frame)); got != want {
			t.Errorf("generatedText(%s) = %q, want %q", frame, got, want)
		}
	}
}

// The Recorder sums each Session's calls that answered, and leaves out cache
// warming and failed calls.
func TestRecorderSumsASessionsCalls(t *testing.T) {
	r := NewRecorder(10)
	r.RecordRequest(RequestRecord{SessionID: "s1", Status: 200, Model: "m", DurationMs: 3000, TTFTMs: 1000,
		Usage: &TokenUsage{Input: 1000, Output: 400, CacheRead: 900}})
	r.RecordRequest(RequestRecord{SessionID: "s1", Status: 200, CallType: "title", DurationMs: 500, TTFTMs: 450,
		Usage: &TokenUsage{Input: 100, Output: 10}})
	r.RecordRequest(RequestRecord{SessionID: "s1", Status: 429, DurationMs: 50})
	r.RecordRequest(RequestRecord{SessionID: "s1", Status: 200, Kind: "cache_warm", Usage: &TokenUsage{Input: 1000, CacheRead: 1000}})
	r.RecordRequest(RequestRecord{SessionID: "s2", Status: 200, Usage: &TokenUsage{Input: 5}})
	s, ok := r.Session("s1")
	if !ok || s.Calls != 2 || s.Usage.Input != 1100 || s.Usage.Output != 410 || s.Usage.CacheRead != 900 {
		t.Fatalf("s1 = %+v (%v)", s, ok)
	}
	if s.Last.CallType != "title" || s.DurationMs != 3500 || s.MeanTTFTMs() != 725 {
		t.Fatalf("s1's last call, time or first token: %+v, mean %v", s, s.MeanTTFTMs())
	}
	// The title's 50 ms of output is too short to give a speed: 400 tokens in 2 s.
	if got := s.TokensPerSecond(); got != 200 {
		t.Fatalf("s1's speed %v, want 200", got)
	}
	if got := s.Usage.CacheHit(); got < 0.81 || got > 0.82 {
		t.Fatalf("s1's cache hit %v, want 900/1100", got)
	}
	if _, ok := r.Session("s3"); ok {
		t.Fatal("a Session without calls has none")
	}
}

func TestRecorderForgetsTheSessionUpdatedLongestAgo(t *testing.T) {
	r := NewRecorder(10)
	for i := range maxSessions + 1 {
		r.RecordRequest(RequestRecord{SessionID: "s" + string(rune('a'+i%26)) + string(rune('a'+i/26)), Status: 200})
	}
	if _, ok := r.Session("saa"); ok {
		t.Fatal("the oldest Session is still kept")
	}
	if len(r.sessions) != maxSessions {
		t.Fatalf("%d Sessions kept, want %d", len(r.sessions), maxSessions)
	}
}
