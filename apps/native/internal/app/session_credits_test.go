package app

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kkkk2323/droid-sdk-go/protocol"
	"github.com/kkkk2323/droid-sdk-go/session"

	"github.com/kkkk2323/droi/apps/native/internal/llmusage"
	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/diag"
	"github.com/kkkk2323/droi/apps/native/pkg/prefs"
)

func (h *harness) notifyCredits(id string, n any) {
	h.t.Helper()
	// openSession only clicks the row. Do not race the load's initial zero
	// snapshot with a notification injected by the test.
	h.until("the Session is loaded before its usage changes", func() bool {
		s := h.ctl.Store().Session(id)
		return s != nil && s.LoadState() == session.Loaded
	})
	b, err := json.Marshal(n)
	if err != nil {
		h.t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		h.t.Fatal(err)
	}
	if err := h.d.Notify(id, raw); err != nil {
		h.t.Fatal(err)
	}
}

func TestSessionCreditsComeFromTheDaemonNotModelWeights(t *testing.T) {
	h := newHarness(t, oneSession(), "")
	h.openSession("One")
	id := h.a.Route().SessionID
	usage := func(credits float64) protocol.TokenUsage {
		return protocol.TokenUsage{InputTokens: 9000, OutputTokens: 100, FactoryCredits: &credits}
	}
	notify := func(n any) {
		t.Helper()
		h.notifyCredits(id, n)
	}
	notify(protocol.SessionTokenUsageChangedNotification{
		Type: "session_token_usage_changed", SessionID: id,
		TokenUsage: usage(12345), InclusiveTokenUsage: ptr(usage(14567)),
	})
	h.until("the Daemon's credits without a model-speed measurement", func() bool { return h.hasText("14.6K Credits") })
	h.click("Session credits")
	for _, want := range []string{"This Session", "12,345", "Including subagents", "14,567", "Reported by the Daemon.", "Already counted at each model's multiplier and promotion, as the quota counts them."} {
		if !h.hasText(want) {
			t.Fatalf("credits lack %q: %q", want, h.tt.Texts())
		}
	}
	if h.hasText("Last observed turn") {
		t.Fatal("no turn credits have been reported yet")
	}
	// Turn usage is its own figure, not a delta of Session usage, and does
	// not replace the Session's cumulative total.
	notify(protocol.AgentTurnCompletedNotification{
		Type: "agent_turn_completed", Reason: protocol.AgentTurnCompletionReasonCompleted,
		TurnID: "turn-1", TokenUsage: usage(321),
	})
	h.until("the turn's own credits", func() bool { return h.hasText("Last observed turn") && h.hasText("321") })
	notify(protocol.AgentTurnCompletedNotification{
		Type: "agent_turn_completed", Reason: protocol.AgentTurnCompletionReasonCompleted,
		TurnID: "turn-1", TokenUsage: usage(321),
	})
	h.settle()
	if !h.hasText("321") || !h.hasText("14.6K Credits") || h.hasText("642") {
		t.Fatalf("a replayed notification changed the totals: %q", h.tt.Texts())
	}
	// Zero is reported, not unknown. A missing inclusive credit field must
	// not hide an own-session credit field that is present.
	notify(protocol.SessionTokenUsageChangedNotification{
		Type: "session_token_usage_changed", SessionID: id,
		TokenUsage: usage(0), InclusiveTokenUsage: &protocol.TokenUsage{InputTokens: 10000},
	})
	h.until("a reported zero, with an older Daemon's incomplete rollup", func() bool { return h.hasText("0 Credits") })
	// A newer turn without a credit field must not leave the old one shown
	// as if it belonged to the new turn.
	notify(protocol.AgentTurnCompletedNotification{
		Type: "agent_turn_completed", Reason: protocol.AgentTurnCompletionReasonCompleted,
		TurnID: "turn-2", TokenUsage: protocol.TokenUsage{},
	})
	h.until("the absent turn credit is not a stale 321", func() bool { return !h.hasText("Last observed turn") })
	notify(protocol.SessionTokenUsageChangedNotification{
		Type: "session_token_usage_changed", SessionID: id, TokenUsage: protocol.TokenUsage{},
	})
	h.until("missing credits do not appear as zero", func() bool { return !h.hasButton("Session credits") })
}

func TestCreditValueDoesNotInventCharges(t *testing.T) {
	if creditValue(nil) != nil || creditValue(&protocol.TokenUsage{}) != nil {
		t.Fatal("an absent credit field became a charge")
	}
	for _, n := range []float64{-1, math.NaN(), math.Inf(1), math.Inf(-1)} {
		if creditValue(&protocol.TokenUsage{FactoryCredits: &n}) != nil {
			t.Errorf("accepted invalid credits %v", n)
		}
	}
	for _, n := range []float64{0, 1, 12345.5} {
		got := creditValue(&protocol.TokenUsage{FactoryCredits: &n})
		if got == nil || *got != n {
			t.Errorf("lost reported credits %v: %v", n, got)
		}
	}
}

func TestSessionCreditsPresentation(t *testing.T) {
	for _, theme := range []string{"light", "dark"} {
		t.Run(theme, func(t *testing.T) {
			h := newHarnessWith(t, oneSession(), theme, func(cfg *Config) {
				prefs.Language.Set(cfg.Prefs, string(l10n.Chinese))
			})
			t.Cleanup(func() { l10n.Set("", "") })
			h.openSession("One")
			id := h.a.Route().SessionID
			h.notifyCredits(id, protocol.SessionTokenUsageChangedNotification{
				Type: "session_token_usage_changed", SessionID: id,
				TokenUsage:          protocol.TokenUsage{FactoryCredits: ptr(12345.0)},
				InclusiveTokenUsage: &protocol.TokenUsage{FactoryCredits: ptr(14567.0)},
			})
			h.notifyCredits(id, protocol.AgentTurnCompletedNotification{
				Type: "agent_turn_completed", Reason: protocol.AgentTurnCompletionReasonCompleted,
				TokenUsage:      protocol.TokenUsage{FactoryCredits: ptr(321.0)},
				ChildTokenUsage: &protocol.TokenUsage{FactoryCredits: ptr(45.0)},
			})
			h.until("the Daemon's session total", func() bool { return h.hasText("14.6K Credits") })
			h.click("会话 Credits")
			h.until("the turn notification is shown too", func() bool { return h.hasText("最近记录的回合") && h.hasText("该回合的子代理") })
			for _, want := range []string{"本会话", "12,345", "含子代理", "14,567", "最近记录的回合", "321", "该回合的子代理", "45", "回合记录于", "来自 Daemon 的用量报告。", "已按各模型的倍率和促销折算，和额度扣减的口径一致。"} {
				if !h.hasText(want) {
					t.Fatalf("credits lack %q: %q", want, h.tt.Texts())
				}
			}
			h.shoot("session-credits-" + theme)
		})
	}
}

// writeCalls puts calls in the Key Pool's request log.
func writeCalls(t *testing.T, h *harness, calls ...diag.RequestRecord) {
	t.Helper()
	pool := h.a.keyPool()
	if pool == nil {
		t.Fatal("no Key Pool")
	}
	var b []byte
	for _, c := range calls {
		line, _ := json.Marshal(c)
		b = append(append(b, line...), '\n')
	}
	_ = os.MkdirAll(filepath.Dir(pool.RequestLog()), 0o700)
	if err := os.WriteFile(pool.RequestLog(), b, 0o600); err != nil {
		t.Fatal(err)
	}
}

// droid's own figure misses what Factory bills: for Gemini it counts the
// cache reads twice. When the request log holds every call of the Session,
// the footer shows the log's figure, priced from what the responses
// reported, for the Session and for the turn.
func TestSessionCreditsComeFromTheRequestLog(t *testing.T) {
	h := newHarnessWith(t, oneSession(), "", func(cfg *Config) { cfg.Host = keyPoolTestHost(t) })
	h.openSession("One")
	id := h.a.Route().SessionID
	call := diag.RequestRecord{Time: time.Now().Add(-time.Minute), Model: "gemini-4-carbon", Provider: "google", SessionID: id, Status: 200,
		Usage: &diag.TokenUsage{Input: 430_000, CacheRead: 385_436, Output: 1_575}}
	writeCalls(t, h, call)
	billed, ok := llmusage.Credits(call.Model, *call.Usage, call.Time)
	if !ok {
		t.Fatal("Gemini 4 Carbon is unpriced")
	}
	// What droid reports: the cache reads again at the input weight.
	daemon := math.Round(billed + 1.6*385_436)
	h.notifyCredits(id, protocol.AgentTurnCompletedNotification{
		Type: "agent_turn_completed", Reason: protocol.AgentTurnCompletionReasonCompleted, TurnID: "turn-1",
		TokenUsage: protocol.TokenUsage{InputTokens: 430_000, CacheReadTokens: 385_436, OutputTokens: 1_575, FactoryCredits: &daemon}, DurationMs: ptr(120_000.0),
	})
	h.notifyCredits(id, protocol.SessionTokenUsageChangedNotification{
		Type: "session_token_usage_changed", SessionID: id,
		TokenUsage: protocol.TokenUsage{InputTokens: 430_000, CacheReadTokens: 385_436, OutputTokens: 1_575, FactoryCredits: &daemon},
	})
	h.until("the log's figure", func() bool { return h.hasText(llmusage.FormatCredits(billed) + " Credits") })
	h.click("Session credits")
	// The Session's and the turn's rows show the log's figure; droid's is
	// only in the note that names it.
	h.until("the turn's from the log too", func() bool {
		n := 0
		for _, s := range h.tt.Texts() {
			if s == groupDigits(billed) {
				n++
			}
			if s == groupDigits(daemon) {
				return false
			}
		}
		return n == 2 && h.hasText("Last observed turn")
	})
	for _, s := range []string{groupDigits(billed), "Priced from the request log", "droid reports " + groupDigits(daemon)} {
		if !h.hasText(s) {
			t.Fatalf("credits lack %q: %q", s, h.tt.Texts())
		}
	}
	if h.hasText("Reported by the Daemon.") {
		t.Fatalf("the Daemon is named as the source: %q", h.tt.Texts())
	}
}

// A log that lacks calls of the Session (its tokens fall short of the
// Daemon's) leaves the Daemon's figure.
func TestSessionCreditsKeepTheDaemonsWithoutTheWholeLog(t *testing.T) {
	h := newHarnessWith(t, oneSession(), "", func(cfg *Config) { cfg.Host = keyPoolTestHost(t) })
	h.openSession("One")
	id := h.a.Route().SessionID
	writeCalls(t, h, diag.RequestRecord{Time: time.Now().Add(-time.Minute), Model: "claude-opus-5-5", Provider: "anthropic", SessionID: id, Status: 200,
		Usage: &diag.TokenUsage{Input: 179_959, CacheRead: 113_226, CacheWrite: 66_723, Output: 287}})
	credits := 152_818.0
	h.notifyCredits(id, protocol.SessionTokenUsageChangedNotification{
		Type: "session_token_usage_changed", SessionID: id,
		TokenUsage: protocol.TokenUsage{InputTokens: 10, CacheReadTokens: 113_226, CacheCreationTokens: 66_723, OutputTokens: 1_287, FactoryCredits: &credits},
	})
	h.until("the log was read", func() bool { return !h.a.views[id].logged.at.IsZero() })
	h.settle()
	if !h.hasText(llmusage.FormatCredits(credits) + " Credits") {
		t.Fatalf("the Daemon's figure is not shown: %q", h.tt.Texts())
	}
	h.click("Session credits")
	h.until("the Daemon named as the source", func() bool { return h.hasText("Reported by the Daemon.") })
}
