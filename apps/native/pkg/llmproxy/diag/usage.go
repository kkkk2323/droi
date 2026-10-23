package diag

import (
	"bytes"
	"encoding/json"
	"strings"
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
// alters the bytes; it collects token usage as they stream past. Bodies are
// bounded so a runaway response cannot pin memory.
type UsageObserver struct {
	sse   bool
	buf   bytes.Buffer
	usage TokenUsage
	found bool
	over  bool
}

// Per-line cap for SSE frames and total cap for buffered JSON bodies.
const usageBufferLimit = 4 << 20

// NewUsageObserver picks the parsing mode from the upstream Content-Type.
func NewUsageObserver(contentType string) *UsageObserver {
	return &UsageObserver{sse: strings.HasPrefix(strings.ToLower(contentType), "text/event-stream")}
}

// IsStream reports whether the response was server-sent events.
func (o *UsageObserver) IsStream() bool { return o.sse }

func (o *UsageObserver) Write(p []byte) (int, error) {
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
}

// Usage returns the merged token usage and whether any was seen. Call after
// the body has been fully copied.
func (o *UsageObserver) Usage() (TokenUsage, bool) {
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
