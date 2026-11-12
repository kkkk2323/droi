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

// droid counts a Gemini call's cache reads in its input as well, and prices
// them again at the full input weight; Factory bills them once. The footer
// takes that off with the request log's calls of the Session.
func TestSessionCreditsTakeOffDroidsGeminiRecount(t *testing.T) {
	h := newHarnessWith(t, oneSession(), "", func(cfg *Config) { cfg.Host = keyPoolTestHost(t) })
	h.openSession("One")
	id := h.a.Route().SessionID
	pool := h.a.keyPool()
	if pool == nil {
		t.Fatal("no Key Pool")
	}
	call := diag.RequestRecord{Time: time.Now().Add(-time.Minute), Model: "gemini-4-carbon", Provider: "google", SessionID: id, Status: 200,
		Usage: &diag.TokenUsage{Input: 430_000, CacheRead: 385_436, Output: 1_575}}
	line, _ := json.Marshal(call)
	_ = os.MkdirAll(filepath.Dir(pool.RequestLog()), 0o700)
	if err := os.WriteFile(pool.RequestLog(), append(line, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	billed, ok := llmusage.Credits(call.Model, *call.Usage, call.Time)
	if !ok {
		t.Fatal("Gemini 4 Carbon is unpriced")
	}
	// What droid reports: the cache reads again at the input weight.
	recount := 1.6 * 385_436
	daemon := math.Round(billed + recount)
	h.notifyCredits(id, protocol.AgentTurnCompletedNotification{
		Type: "agent_turn_completed", Reason: protocol.AgentTurnCompletionReasonCompleted, TurnID: "turn-1",
		TokenUsage: protocol.TokenUsage{FactoryCredits: &daemon}, DurationMs: ptr(120_000.0),
	})
	h.notifyCredits(id, protocol.SessionTokenUsageChangedNotification{
		Type: "session_token_usage_changed", SessionID: id,
		TokenUsage: protocol.TokenUsage{InputTokens: 430_000, CacheReadTokens: 385_436, OutputTokens: 1_575, FactoryCredits: &daemon},
	})
	want := llmusage.FormatCredits(daemon - recount)
	h.until("the footer without droid's recount", func() bool { return h.hasText(want + " Credits") })
	h.click("Session credits")
	h.until("the turn without droid's recount too", func() bool { return h.hasText("Last observed turn") && !h.hasText(groupDigits(daemon)) })
	for _, s := range []string{groupDigits(daemon - recount), "droid counts a Gemini call's cached input twice", groupDigits(recount)} {
		if !h.hasText(s) {
			t.Fatalf("credits lack %q: %q", s, h.tt.Texts())
		}
	}
}

// An Anthropic Session's input leaves the cache out: nothing is taken off,
// and the request log is not read for it.
func TestSessionCreditsKeepOtherProviders(t *testing.T) {
	h := newHarnessWith(t, oneSession(), "", func(cfg *Config) { cfg.Host = keyPoolTestHost(t) })
	h.openSession("One")
	id := h.a.Route().SessionID
	credits := 152_818.0
	h.notifyCredits(id, protocol.SessionTokenUsageChangedNotification{
		Type: "session_token_usage_changed", SessionID: id,
		TokenUsage: protocol.TokenUsage{InputTokens: 10, CacheReadTokens: 113_226, CacheCreationTokens: 66_723, OutputTokens: 1_287, FactoryCredits: &credits},
	})
	h.until("droid's figure", func() bool { return h.hasText(llmusage.FormatCredits(credits) + " Credits") })
	if h.a.usage.observe.log != nil {
		t.Fatal("the request log was read for a Session droid counts right")
	}
}
