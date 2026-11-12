package diag

import "testing"

func TestUsageFromJSON_AnthropicFoldsCacheIntoInput(t *testing.T) {
	body := `{"id":"msg_1","usage":{"input_tokens":25,"cache_creation_input_tokens":100,"cache_read_input_tokens":2000,"output_tokens":40}}`
	u, ok := UsageFromJSON([]byte(body))
	if !ok {
		t.Fatal("expected usage")
	}
	want := TokenUsage{Input: 2125, Output: 40, CacheRead: 2000, CacheWrite: 100}
	if u != want {
		t.Fatalf("got %+v want %+v", u, want)
	}
}

// Anthropic says how many of the cache writes are kept for an hour, which
// cost more (a Haiku 5.5 message_start through Factory, 2026-11-12).
func TestUsageFromJSON_AnthropicHourLongCacheWrites(t *testing.T) {
	body := `{"type":"message_start","message":{"usage":{"input_tokens":4,"cache_creation_input_tokens":12914,"cache_read_input_tokens":0,"cache_creation":{"ephemeral_5m_input_tokens":2515,"ephemeral_1h_input_tokens":10399},"output_tokens":2}}}`
	u, ok := UsageFromJSON([]byte(body))
	if want := (TokenUsage{Input: 12918, Output: 2, CacheWrite: 12914, CacheWrite1h: 10399}); !ok || u != want {
		t.Fatalf("got %+v want %+v", u, want)
	}
	// message_delta repeats the totals without the breakdown: the merge
	// keeps it.
	o := NewUsageObserver("text/event-stream")
	_, _ = o.Write([]byte("data: " + body + "\n\ndata: {\"type\":\"message_delta\",\"usage\":{\"input_tokens\":4,\"cache_creation_input_tokens\":12914,\"cache_read_input_tokens\":0,\"output_tokens\":194}}\n\n"))
	if u, _ := o.Usage(); u.CacheWrite1h != 10399 || u.Output != 194 {
		t.Fatalf("stream lost the hour-long writes: %+v", u)
	}
}

func TestUsageFromJSON_OpenAIChatAndResponses(t *testing.T) {
	chat := `{"usage":{"prompt_tokens":500,"completion_tokens":80,"prompt_tokens_details":{"cached_tokens":300},"completion_tokens_details":{"reasoning_tokens":20}}}`
	u, ok := UsageFromJSON([]byte(chat))
	if !ok || u != (TokenUsage{Input: 500, Output: 80, CacheRead: 300, Reasoning: 20}) {
		t.Fatalf("chat usage wrong: %+v ok=%v", u, ok)
	}

	responses := `{"type":"response.completed","response":{"usage":{"input_tokens":700,"input_tokens_details":{"cached_tokens":100},"output_tokens":90,"output_tokens_details":{"reasoning_tokens":30}}}}`
	u, ok = UsageFromJSON([]byte(responses))
	if !ok || u != (TokenUsage{Input: 700, Output: 90, CacheRead: 100, Reasoning: 30}) {
		t.Fatalf("responses usage wrong: %+v ok=%v", u, ok)
	}

	// The prompt counts include the cache writes, as droid reads them.
	written := `{"type":"response.completed","response":{"usage":{"input_tokens":157634,"input_tokens_details":{"cached_tokens":113427,"cache_write_tokens":44192},"output_tokens":742}}}`
	u, ok = UsageFromJSON([]byte(written))
	if !ok || u != (TokenUsage{Input: 157634, Output: 742, CacheRead: 113427, CacheWrite: 44192}) {
		t.Fatalf("responses cache writes wrong: %+v ok=%v", u, ok)
	}
	chatWritten := `{"usage":{"prompt_tokens":500,"completion_tokens":80,"prompt_tokens_details":{"cached_tokens":300,"cache_write_tokens":150}}}`
	u, ok = UsageFromJSON([]byte(chatWritten))
	if !ok || u != (TokenUsage{Input: 500, Output: 80, CacheRead: 300, CacheWrite: 150}) {
		t.Fatalf("chat cache writes wrong: %+v ok=%v", u, ok)
	}

	if _, ok := UsageFromJSON([]byte(`{"type":"content_block_delta","delta":{"text":"hi"}}`)); ok {
		t.Fatal("frame without usage must not report usage")
	}
}

func TestUsageObserver_AnthropicSSEMergesStartAndDelta(t *testing.T) {
	o := NewUsageObserver("text/event-stream; charset=utf-8")
	if !o.IsStream() {
		t.Fatal("expected stream mode")
	}
	frames := "event: message_start\n" +
		`data: {"type":"message_start","message":{"usage":{"input_tokens":10,"cache_read_input_tokens":90,"output_tokens":1}}}` + "\n\n" +
		"event: content_block_delta\n" +
		`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"hello"}}` + "\n\n" +
		"event: message_delta\n" +
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":57}}` + "\n\n" +
		"event: message_stop\n" + `data: {"type":"message_stop"}` + "\n"
	// Write in awkward chunks to exercise line reassembly.
	for i := 0; i < len(frames); i += 7 {
		end := i + 7
		if end > len(frames) {
			end = len(frames)
		}
		if _, err := o.Write([]byte(frames[i:end])); err != nil {
			t.Fatal(err)
		}
	}
	u, ok := o.Usage()
	if !ok {
		t.Fatal("expected usage from stream")
	}
	want := TokenUsage{Input: 100, Output: 57, CacheRead: 90}
	if u != want {
		t.Fatalf("got %+v want %+v", u, want)
	}
}

func TestUsageObserver_OpenAIChatStreamFinalChunk(t *testing.T) {
	o := NewUsageObserver("text/event-stream")
	o.Write([]byte(`data: {"choices":[{"delta":{"content":"a"}}]}` + "\n\n"))
	o.Write([]byte(`data: {"choices":[],"usage":{"prompt_tokens":12,"completion_tokens":3}}` + "\n\n"))
	o.Write([]byte("data: [DONE]\n\n"))
	u, ok := o.Usage()
	if !ok || u != (TokenUsage{Input: 12, Output: 3}) {
		t.Fatalf("got %+v ok=%v", u, ok)
	}
}

func TestUsageObserver_JSONBodyAndOversize(t *testing.T) {
	o := NewUsageObserver("application/json")
	o.Write([]byte(`{"id":"x","usage":{"input_tokens":5,`))
	o.Write([]byte(`"output_tokens":6}}`))
	u, ok := o.Usage()
	if !ok || u != (TokenUsage{Input: 5, Output: 6}) {
		t.Fatalf("got %+v ok=%v", u, ok)
	}

	big := NewUsageObserver("application/json")
	big.Write(make([]byte, usageBufferLimit+1))
	if _, ok := big.Usage(); ok {
		t.Fatal("oversize body must not yield usage")
	}
}

// A stream that ends in a failure frame answers HTTP 200, so only the frame
// says it failed.
func TestUsageObserver_ReportsAStreamThatFailed(t *testing.T) {
	cases := map[string]struct {
		frames string
		want   string
	}{
		"responses without an error": {
			frames: `data: {"type":"response.output_text.delta","delta":"a"}` + "\n\n" +
				"event: response.failed\n" +
				`data: {"type":"response.failed","response":{"id":"r1","status":"failed","error":null,"output":[{"type":"function_call","arguments":"secret"}]}}`,
			want: "response.failed",
		},
		"responses with an error": {
			frames: `data: {"type":"response.failed","response":{"status":"failed","error":{"code":"server_error","message":"The model failed"}}}` + "\n\n",
			want:   "response.failed: server_error: The model failed",
		},
		"responses error event": {
			frames: `data: {"type":"error","code":"rate_limit_exceeded","message":"Slow down","param":null}` + "\n\n",
			want:   "error: rate_limit_exceeded: Slow down",
		},
		"anthropic error event": {
			frames: "event: error\n" + `data: {"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}` + "\n\n",
			want:   "error: overloaded_error: Overloaded",
		},
		"chat completions error": {
			frames: `data: {"error":{"code":502,"message":"Provider returned error"}}` + "\n\n",
			want:   "error: 502: Provider returned error",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			o := NewUsageObserver("text/event-stream")
			o.Write([]byte(c.frames))
			if got, failed := o.Failure(); !failed || got != c.want {
				t.Fatalf("got %q failed=%v, want %q", got, failed, c.want)
			}
		})
	}

	ok := NewUsageObserver("text/event-stream")
	ok.Write([]byte(`data: {"type":"response.completed","response":{"status":"completed","error":null,"usage":{"input_tokens":3,"output_tokens":1}}}` + "\n\n"))
	if got, failed := ok.Failure(); failed {
		t.Fatalf("a completed stream failed: %q", got)
	}
	body := NewUsageObserver("application/json")
	body.Write([]byte(`{"type":"error","error":{"type":"x","message":"y"}}`))
	if _, failed := body.Failure(); failed {
		t.Fatal("a JSON body is not a stream")
	}
}
