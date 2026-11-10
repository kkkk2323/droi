package app

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/kkkk2323/droid-sdk-go/protocol"
	"github.com/kkkk2323/droid-sdk-go/session"

	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
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
	h.until("the Daemon's credits without a model-speed measurement", func() bool { return h.hasText("14,567 Credits") })
	h.click("Session credits")
	for _, want := range []string{"This Session", "12,345", "Including subagents", "14,567", "Reported by the Daemon."} {
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
	if !h.hasText("321") || !h.hasText("14,567 Credits") || h.hasText("642") {
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
			h.until("the Daemon's session total", func() bool { return h.hasText("14,567 Credits") })
			h.click("会话 Credits")
			h.until("the turn notification is shown too", func() bool { return h.hasText("最近记录的回合") && h.hasText("该回合的子代理") })
			for _, want := range []string{"本会话", "12,345", "含子代理", "14,567", "最近记录的回合", "321", "该回合的子代理", "45", "回合记录于", "来自 Daemon 的用量报告。"} {
				if !h.hasText(want) {
					t.Fatalf("credits lack %q: %q", want, h.tt.Texts())
				}
			}
			h.shoot("session-credits-" + theme)
		})
	}
}
