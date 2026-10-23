package diag

import (
	"bytes"
	"encoding/json"
	"strings"
	"sync"
	"time"
)

// TokenUsage is the token accounting reported by an upstream LLM response.
// Input includes cache reads and writes; the cache fields break that down.
type TokenUsage struct {
	Input      int64 `json:"input"`
	Output     int64 `json:"output"`
	CacheRead  int64 `json:"cacheRead,omitempty"`
	CacheWrite int64 `json:"cacheWrite,omitempty"`
	Reasoning  int64 `json:"reasoning,omitempty"`
}

func (u TokenUsage) IsZero() bool {
	return u.Input == 0 && u.Output == 0 && u.CacheRead == 0 && u.CacheWrite == 0 && u.Reasoning == 0
}

// merge keeps the per-field maximum. Streams report usage more than once
// (Anthropic's message_start carries a lower bound for output_tokens, the
// final message_delta the real count), so max is the safe combination.
func (u *TokenUsage) merge(o TokenUsage) {
	if o.Input > u.Input {
		u.Input = o.Input
	}
	if o.Output > u.Output {
		u.Output = o.Output
	}
	if o.CacheRead > u.CacheRead {
		u.CacheRead = o.CacheRead
	}
	if o.CacheWrite > u.CacheWrite {
		u.CacheWrite = o.CacheWrite
	}
	if o.Reasoning > u.Reasoning {
		u.Reasoning = o.Reasoning
	}
}

type rawUsage struct {
	// OpenAI chat
	PromptTokens     *int64 `json:"prompt_tokens"`
	CompletionTokens *int64 `json:"completion_tokens"`
	PromptDetails    *struct {
		CachedTokens *int64 `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
	CompletionDetails *struct {
		ReasoningTokens *int64 `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`

	// OpenAI responses + Anthropic
	InputTokens  *int64 `json:"input_tokens"`
	OutputTokens *int64 `json:"output_tokens"`
	InputDetails *struct {
		CachedTokens *int64 `json:"cached_tokens"`
	} `json:"input_tokens_details"`
	OutputDetails *struct {
		ReasoningTokens *int64 `json:"reasoning_tokens"`
	} `json:"output_tokens_details"`

	// Anthropic
	CacheCreationInputTokens *int64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     *int64 `json:"cache_read_input_tokens"`
}

func (r rawUsage) toTokenUsage() TokenUsage {
	var u TokenUsage
	switch {
	case r.PromptTokens != nil:
		u.Input = *r.PromptTokens
	case r.InputTokens != nil:
		// Anthropic's input_tokens excludes cached tokens; fold them back in so
		// Input is the full prompt size like OpenAI's prompt_tokens.
		u.Input = *r.InputTokens + deref(r.CacheCreationInputTokens) + deref(r.CacheReadInputTokens)
	}
	if r.CompletionTokens != nil {
		u.Output = *r.CompletionTokens
	} else if r.OutputTokens != nil {
		u.Output = *r.OutputTokens
	}
	switch {
	case r.CacheReadInputTokens != nil:
		u.CacheRead = *r.CacheReadInputTokens
	case r.PromptDetails != nil && r.PromptDetails.CachedTokens != nil:
		u.CacheRead = *r.PromptDetails.CachedTokens
	case r.InputDetails != nil && r.InputDetails.CachedTokens != nil:
		u.CacheRead = *r.InputDetails.CachedTokens
	}
	u.CacheWrite = deref(r.CacheCreationInputTokens)
	if r.CompletionDetails != nil && r.CompletionDetails.ReasoningTokens != nil {
		u.Reasoning = *r.CompletionDetails.ReasoningTokens
	} else if r.OutputDetails != nil && r.OutputDetails.ReasoningTokens != nil {
		u.Reasoning = *r.OutputDetails.ReasoningTokens
	}
	return u
}

func deref(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

// usageEnvelope finds a usage object at the top level or nested under
// "message" (Anthropic message_start) or "response" (OpenAI responses).
type usageEnvelope struct {
	Usage   *rawUsage `json:"usage"`
	Message *struct {
		Usage *rawUsage `json:"usage"`
	} `json:"message"`
	Response *struct {
		Usage *rawUsage `json:"usage"`
	} `json:"response"`
}

// UsageFromJSON extracts token usage from one JSON object (a full non-stream
// response body or a single SSE data frame).
func UsageFromJSON(body []byte) (TokenUsage, bool) {
	var env usageEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return TokenUsage{}, false
	}
	var raw *rawUsage
	switch {
	case env.Usage != nil:
		raw = env.Usage
	case env.Message != nil && env.Message.Usage != nil:
		raw = env.Message.Usage
	case env.Response != nil && env.Response.Usage != nil:
		raw = env.Response.Usage
	default:
		return TokenUsage{}, false
	}
	u := raw.toTokenUsage()
	return u, !u.IsZero()
}

// UsageObserver is an io.Writer tap over a proxied response body. It never
// alters the bytes; it collects token usage as they stream past, and when the
// first generated token came and how many have come since. Bodies are
// bounded so a runaway response cannot pin memory. Progress may be read
// while another goroutine writes.
type UsageObserver struct {
	mu    sync.Mutex
	sse   bool
	buf   bytes.Buffer
	usage TokenUsage
	found bool
	over  bool
	now   func() time.Time
	// first is when the first generated text, thinking or tool input came;
	// generated estimates the tokens generated since, from their text.
	first     time.Time
	generated float64
}

// Per-line cap for SSE frames and total cap for buffered JSON bodies.
const usageBufferLimit = 4 << 20

// NewUsageObserver picks the parsing mode from the upstream Content-Type.
func NewUsageObserver(contentType string) *UsageObserver {
	return &UsageObserver{sse: strings.HasPrefix(strings.ToLower(contentType), "text/event-stream"), now: time.Now}
}

// IsStream reports whether the response was server-sent events.
func (o *UsageObserver) IsStream() bool { return o.sse }

func (o *UsageObserver) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.over {
		return len(p), nil
	}
	if !o.sse {
		if o.buf.Len()+len(p) > usageBufferLimit {
			o.over = true
			o.buf.Reset()
			return len(p), nil
		}
		o.buf.Write(p)
		return len(p), nil
	}
	rest := p
	for {
		idx := bytes.IndexByte(rest, '\n')
		if idx < 0 {
			if o.buf.Len()+len(rest) > usageBufferLimit {
				o.buf.Reset()
			} else {
				o.buf.Write(rest)
			}
			return len(p), nil
		}
		o.buf.Write(rest[:idx])
		o.consumeLine(o.buf.Bytes())
		o.buf.Reset()
		rest = rest[idx+1:]
	}
}

func (o *UsageObserver) consumeLine(line []byte) {
	line = bytes.TrimSpace(line)
	if !bytes.HasPrefix(line, []byte("data:")) {
		return
	}
	payload := bytes.TrimSpace(line[len("data:"):])
	if len(payload) == 0 || bytes.Equal(payload, []byte("[DONE]")) {
		return
	}
	if u, ok := UsageFromJSON(payload); ok {
		o.usage.merge(u)
		o.found = true
	}
	if text := generatedText(payload); text != "" {
		if o.first.IsZero() {
			o.first = o.now()
		}
		o.generated += EstimateTokens(text)
	}
}

// Usage returns the merged token usage and whether any was seen. Call after
// the body has been fully copied.
func (o *UsageObserver) Usage() (TokenUsage, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.sse {
		if o.buf.Len() > 0 {
			o.consumeLine(o.buf.Bytes())
			o.buf.Reset()
		}
		return o.usage, o.found
	}
	if o.over || o.buf.Len() == 0 {
		return TokenUsage{}, false
	}
	return UsageFromJSON(o.buf.Bytes())
}

// Progress is a stream so far: when its first generated token came (zero
// before one did, and for a body that is not a stream), the tokens generated
// since (the upstream's count once it gave one, an estimate from the text
// until then) and the usage reported so far.
func (o *UsageObserver) Progress() (first time.Time, generated float64, usage TokenUsage) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.first, max(o.generated, float64(o.usage.Output)), o.usage
}

// deltaFrame is the generated text an SSE frame carries, in Anthropic's
// (content_block_delta), OpenAI's chat completions (choices[].delta) or
// OpenAI's responses (response.*.delta) shape.
type deltaFrame struct {
	Type    string          `json:"type"`
	Delta   json.RawMessage `json:"delta"`
	Choices []struct {
		Delta struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
			Reasoning        string `json:"reasoning"`
			ToolCalls        []struct {
				Function struct {
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
	} `json:"choices"`
}

func generatedText(payload []byte) string {
	var f deltaFrame
	if json.Unmarshal(payload, &f) != nil {
		return ""
	}
	var b strings.Builder
	switch {
	case f.Type == "content_block_delta" && len(f.Delta) > 0:
		var d struct {
			Text        string `json:"text"`
			Thinking    string `json:"thinking"`
			PartialJSON string `json:"partial_json"`
		}
		if json.Unmarshal(f.Delta, &d) == nil {
			b.WriteString(d.Text + d.Thinking + d.PartialJSON)
		}
	case strings.HasPrefix(f.Type, "response.") && strings.HasSuffix(f.Type, ".delta") && len(f.Delta) > 0:
		var s string
		if json.Unmarshal(f.Delta, &s) == nil {
			b.WriteString(s)
		}
	}
	for _, c := range f.Choices {
		b.WriteString(c.Delta.Content + c.Delta.ReasoningContent + c.Delta.Reasoning)
		for _, tc := range c.Delta.ToolCalls {
			b.WriteString(tc.Function.Arguments)
		}
	}
	return b.String()
}

// EstimateTokens guesses how many tokens text is, as the common tokenizers
// split it: about four characters of ASCII a token, and one for any other.
func EstimateTokens(text string) float64 {
	var n float64
	for _, r := range text {
		if r < 0x80 {
			n += 0.25
		} else {
			n++
		}
	}
	return n
}
