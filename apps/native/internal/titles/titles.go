// Package titles names a conversation with one model call.
//
// A conversation starts with no title, and a list of "Untitled session" is a list nobody can pick from. The
// name comes from the conversation itself: its first question, its first answer, and `model.complete` — a
// call with no conversation behind it, so naming writes no entry, starts no run and shows up nowhere but in
// the list. The rules are in prompt.go; this file is when to ask and what to do when the answer is not a
// title.
package titles

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/kernel"
)

const (
	// defaultAttempts is how many times one conversation may be named, across rounds: a first turn that
	// failed, or a model that answered nothing usable, gets one more chance after the next one. Two is the
	// point where another try costs more than a list of one plainer title.
	defaultAttempts = 2
	// callTimeout bounds one naming call, model request included. A title is not worth a runaway request.
	callTimeout = 20 * time.Second
	// firstTurnLimit is how much of the transcript is read to find the first question and the first answer.
	// Forty entries cover a first turn that ran several tools; the question is normally the first entry.
	firstTurnLimit = 40
)

// Source is what naming needs of Kernel: the conversation's model, its first entries, the model call, and
// the write to the list. *kernel.Client is one.
type Source interface {
	Configure(ctx context.Context, p kernel.ConfigureParams) (kernel.ConfigureResult, error)
	Entries(ctx context.Context, p kernel.EntriesParams) (kernel.EntriesResult, error)
	UpdateIndex(ctx context.Context, p kernel.IndexUpdateParams) error
	ModelComplete(ctx context.Context, p kernel.ModelCompleteParams) (kernel.ModelCompleteResult, error)
}

// Config is what a Manager runs on.
type Config struct {
	Source Source
	// Source reports where a conversation's title came from, as the caller knows it: kernel.TitleSourceAuto
	// (Kernel's first line), kernel.TitleSourceChosen (somebody named it, or this package did, and it is never
	// replaced), or "" for no title yet. ok is false for a conversation the caller does not know, or
	// not one a person started; it is not named.
	TitleSource func(ctx context.Context, id kernel.ID) (source string, ok bool)
	// Logf hears what naming did, and why it did nothing. Optional.
	Logf func(format string, args ...any)
	// MaxAttempts overrides defaultAttempts. Optional.
	MaxAttempts int
}

// Manager names conversations. One lives with the Host, because a title is the list's and every client
// reads the same list.
type Manager struct {
	cfg Config
	wg  sync.WaitGroup

	mu       sync.Mutex
	attempts map[kernel.ID]int
	named    map[kernel.ID]bool
	running  map[kernel.ID]bool
}

// New makes a Manager. It starts nothing: Busy and Name do the work.
func New(cfg Config) *Manager {
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = defaultAttempts
	}
	if cfg.Logf == nil {
		cfg.Logf = func(string, ...any) {}
	}
	return &Manager{
		cfg:      cfg,
		attempts: map[kernel.ID]int{},
		named:    map[kernel.ID]bool{},
		running:  map[kernel.ID]bool{},
	}
}

// Busy takes a conversation.busy notification: the conversation's run started, or — run null — ended. Naming
// happens on the end of a round, which is when the first answer exists and the conversation can be called
// something. It returns at once; the call itself runs in the background.
//
// It is safe on every round: a conversation is named once, and at most MaxAttempts model calls are spent
// trying.
func (m *Manager) Busy(ctx context.Context, n kernel.BusyNotification) {
	if n.Busy() {
		return
	}
	m.mu.Lock()
	named := m.named[n.ID]
	m.mu.Unlock()
	if named {
		return
	}
	// Name itself refuses a conversation already being named, so two ends in flight together cost one call
	// at most.
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		if _, err := m.Name(ctx, n.ID); err != nil {
			m.cfg.Logf("naming conversation %d: %v", n.ID, err)
		}
	}()
}

// Wait waits for the naming Busy started. A Host calls it when it closes, so no call outlives it.
func (m *Manager) Wait() { m.wg.Wait() }

// Name names one conversation and answers the title it wrote, or "" when it wrote none. It does nothing, and
// answers nothing, when somebody chose its title, when it has no question yet, or when its attempts are
// spent.
//
// A model that fails, refuses, is not available, or answers something that is not a title costs no title:
// the conversation is named with the user's own first words instead, which is always a usable line and
// already in the user's language.
func (m *Manager) Name(ctx context.Context, id kernel.ID) (string, error) {
	if !m.enter(id) {
		return "", nil
	}
	defer m.leave(id)

	if m.namedByMe(id) || !m.mayName(ctx, id) {
		return "", nil
	}

	question, answer, ok := m.firstTurn(ctx, id)
	if !ok || strings.TrimSpace(question) == "" {
		return "", nil
	}
	if m.attemptsSpent(id) {
		return "", nil
	}

	title := m.ask(ctx, id, question, answer)
	if title == "" {
		title = FirstMessage(question)
	}
	if title == "" {
		return "", nil
	}
	// The user may have renamed it while the model answered. Kernel has no compare-and-set, so a rename in the
	// last moment can still be overwritten; this keeps the common case, a rename made while the call ran.
	if !m.mayName(ctx, id) {
		return "", nil
	}
	// Written as chosen: what remembers that a conversation is named is the list, not this Manager, which a
	// restart or another client starts without. An automatic title would be named again by each of them.
	if err := m.cfg.Source.UpdateIndex(ctx, kernel.IndexUpdateParams{ID: id, Title: kernel.TitleText(title), TitleSource: kernel.TitleSourceChosen}); err != nil {
		return "", err
	}
	m.markNamed(id)
	return title, nil
}

// mayName reports whether the conversation's title is still one naming may replace: Kernel's own first line, or
// none. One somebody chose is never replaced, and the conversation counts as named.
func (m *Manager) mayName(ctx context.Context, id kernel.ID) bool {
	source, ok := m.cfg.TitleSource(ctx, id)
	if !ok {
		return false
	}
	if source == kernel.TitleSourceChosen {
		m.markNamed(id)
		return false
	}
	return true
}

// enter takes the conversation, unless naming it is already under way.
func (m *Manager) enter(id kernel.ID) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.running[id] {
		return false
	}
	m.running[id] = true
	return true
}

func (m *Manager) leave(id kernel.ID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.running, id)
}

func (m *Manager) markNamed(id kernel.ID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.named[id] = true
}

func (m *Manager) namedByMe(id kernel.ID) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.named[id]
}

func (m *Manager) attemptsSpent(id kernel.ID) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.attempts[id] >= m.cfg.MaxAttempts
}

func (m *Manager) countAttempt(id kernel.ID) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.attempts[id] >= m.cfg.MaxAttempts {
		return false
	}
	m.attempts[id]++
	return true
}

// firstTurn is the conversation's first question and the first answer to it: the words a name can be made
// of. It answers false when the first round left no answer behind, which is a round still running or one
// that failed before the model said anything.
func (m *Manager) firstTurn(ctx context.Context, id kernel.ID) (question, answer string, ok bool) {
	page, err := m.cfg.Source.Entries(ctx, kernel.EntriesParams{
		ID:    id,
		Query: json.RawMessage(`{"order":"ascending"}`),
		Limit: firstTurnLimit,
	})
	if err != nil {
		m.cfg.Logf("naming conversation %d: reading its first turn: %v", id, err)
		return "", "", false
	}
	answered := false
	for _, entry := range page.Items {
		switch entry.Kind {
		case "pi.user":
			if question == "" {
				question = entryText(entry)
			}
		case "pi.assistant":
			answered = true
			if answer == "" {
				answer = entryText(entry)
			}
		}
	}
	return question, answer, answered
}

// ask spends one model call on a title, and answers "" when there is none to be had: no model, no call, a
// failure, or an answer that is not a title.
func (m *Manager) ask(ctx context.Context, id kernel.ID, question, answer string) string {
	if !m.countAttempt(id) {
		return ""
	}
	model, ok := m.model(ctx, id)
	if !ok {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	res, err := m.cfg.Source.ModelComplete(ctx, kernel.ModelCompleteParams{
		Model:         model,
		System:        System,
		Input:         Input(question, answer),
		Schema:        json.RawMessage(Schema),
		ThinkingLevel: "off",
		MaxTokens:     MaxTokens,
		TimeoutMs:     int(callTimeout / time.Millisecond),
	})
	if err != nil {
		// Not a title, and not an error worth surfacing: a conversation keeps working without one, and the
		// fallback below already names it.
		m.cfg.Logf("naming conversation %d: %v", id, err)
		return ""
	}
	raw := res.Text
	if len(res.JSON) > 0 {
		var parsed struct {
			Title string `json:"title"`
		}
		if json.Unmarshal(res.JSON, &parsed) == nil && parsed.Title != "" {
			raw = parsed.Title
		}
	}
	title, ok := Sanitize(raw, question)
	if !ok {
		m.cfg.Logf("naming conversation %d: %q is not a title", id, raw)
		return ""
	}
	return title
}

// model is the model the conversation runs on: a name in the words of the model that wrote it, and a
// provider already configured with its key.
func (m *Manager) model(ctx context.Context, id kernel.ID) (kernel.ModelRef, bool) {
	res, err := m.cfg.Source.Configure(ctx, kernel.ConfigureParams{ID: id, Change: &kernel.AgentChange{}})
	if err != nil {
		m.cfg.Logf("naming conversation %d: reading its model: %v", id, err)
		return kernel.ModelRef{}, false
	}
	if res.Agent.Model == nil || res.Agent.Model.ModelID == "" || res.Agent.Model.Provider == "" {
		return kernel.ModelRef{}, false
	}
	return *res.Agent.Model, true
}

// entryText is the text of one entry: Pi's message array, of which only text blocks name anything. An image
// is nothing to name, and a tool call is not the conversation.
func entryText(entry kernel.Entry) string {
	if len(entry.Model) == 0 {
		return ""
	}
	var messages []struct {
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal(entry.Model, &messages) != nil {
		return ""
	}
	var parts []string
	for _, message := range messages {
		var text string
		if json.Unmarshal(message.Content, &text) == nil {
			if strings.TrimSpace(text) != "" {
				parts = append(parts, text)
			}
			continue
		}
		var blocks []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if json.Unmarshal(message.Content, &blocks) != nil {
			continue
		}
		for _, block := range blocks {
			if block.Type == "text" && strings.TrimSpace(block.Text) != "" {
				parts = append(parts, block.Text)
			}
		}
	}
	return strings.Join(parts, "\n")
}
