package testsupport_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/kkkk2323/droid-sdk-go/fakedaemon"

	"github.com/kkkk2323/droi/apps/native/internal/app/testsupport"
	"github.com/kkkk2323/droi/apps/native/pkg/kernel/fakekernel"
)

// TestKernelScenarioKeepsTheResponseAsTheTestWroteIt: a reply's deltas are the chunk boundaries the window's
// tests assert on, so the translation hands them over verbatim instead of letting the Fake Kernel split the
// text its own way. The delay comes along, which is how the tests of incremental rendering get their pauses.
func TestKernelScenarioKeepsTheResponseAsTheTestWroteIt(t *testing.T) {
	turn := fakedaemon.Turn{Kind: "reply", Deltas: []string{"one ", "two ", "three"}, DelayMs: 300}

	got, err := testsupport.KernelScenario(fakedaemon.Scenario{Turn: &turn})
	if err != nil {
		t.Fatalf("a reply turn did not translate: %v", err)
	}
	if len(got.Steps) != 1 {
		t.Fatalf("%d steps, want 1", len(got.Steps))
	}
	step := got.Steps[0]
	if strings.Join(step.Chunks, "|") != "one |two |three" {
		t.Errorf("chunks %q, want the deltas' boundaries", step.Chunks)
	}
	if step.Text != "one two three" {
		t.Errorf("text %q, want the deltas joined", step.Text)
	}
	if step.Delay.Milliseconds() != 300 {
		t.Errorf("delay %v, want 300ms", step.Delay)
	}
	if step.Match != "" {
		t.Errorf("match %q, want a step that answers any message, as the Fake Daemon's turn did", step.Match)
	}
}

// TestKernelScenarioOfNoTurnScriptsNothing: a Scenario with no Turn answers nothing in the Fake Daemon, and
// the Fake Kernel's strict script says the same by having no step — a request that comes anyway fails the
// test instead of being answered with silence.
func TestKernelScenarioOfNoTurnScriptsNothing(t *testing.T) {
	got, err := testsupport.KernelScenario(fakedaemon.Scenario{})
	if err != nil {
		t.Fatalf("an empty Scenario did not translate: %v", err)
	}
	if len(got.Steps) != 0 {
		t.Fatalf("%d steps, want none: %+v", len(got.Steps), got.Steps)
	}
}

// TestEveryTurnKindTheFakeDaemonDocumentsIsEitherTranslatedOrNamed: the translator knows the whole vocabulary
// the Fake Daemon's Turn documents. A kind left out would be refused as "no milestone names yet" — a useful
// signal — but a kind that quietly translated into the wrong thing would not, so each one is checked against
// what its milestone says here.
func TestEveryTurnKindTheFakeDaemonDocumentsIsEitherTranslatedOrNamed(t *testing.T) {
	cases := []struct {
		kind  string
		needs string // a substring of the reason; "" means it translates
	}{
		{"reply", ""},
		{"permission", "approvals"},
		{"askUser", "asks land in M3"},
		{"queuedAskUser", "non-blocking ask"},
		{"backgroundProcess", "background processes"},
		{"todo", "no todo turn"},
		{"structured", "structured output"},
		{"spec", "plan mode"},
	}
	for _, c := range cases {
		t.Run(c.kind, func(t *testing.T) {
			turn := fakedaemon.Turn{Kind: c.kind, Command: "rm -rf build", Question: "Which?", Reply: "done"}
			_, err := testsupport.KernelScenario(fakedaemon.Scenario{Turn: &turn})
			if c.needs == "" {
				if err != nil {
					t.Fatalf("%s should translate, got: %v", c.kind, err)
				}
				return
			}
			var unsupported *testsupport.UnsupportedScenarioError
			if !errors.As(err, &unsupported) {
				t.Fatalf("%s: err = %v, want an UnsupportedScenarioError", c.kind, err)
			}
			if !strings.Contains(unsupported.Feature, c.kind) {
				t.Errorf("%s: the error names %q, want the turn kind", c.kind, unsupported.Feature)
			}
			if !strings.Contains(unsupported.Needs, c.needs) {
				t.Errorf("%s: the reason is %q, want it to mention %q", c.kind, unsupported.Needs, c.needs)
			}
		})
	}
}

// TestAnUnknownTurnKindIsRefusedRatherThanGuessed: the Fake Daemon's Turn has no closed vocabulary, so a
// kind this translator has never seen must be refused — never translated into a plain reply, which is what a
// zero-value Turn would look like.
func TestAnUnknownTurnKindIsRefusedRatherThanGuessed(t *testing.T) {
	turn := fakedaemon.Turn{Kind: "teleport"}
	got, err := testsupport.KernelScenario(fakedaemon.Scenario{Turn: &turn})
	var unsupported *testsupport.UnsupportedScenarioError
	if !errors.As(err, &unsupported) {
		t.Fatalf("err = %v, want an UnsupportedScenarioError", err)
	}
	if !strings.Contains(unsupported.Feature, "teleport") {
		t.Errorf("the error names %q, want the kind", unsupported.Feature)
	}
	if len(got.Steps) != 0 {
		t.Errorf("%d steps for an unknown kind, want none", len(got.Steps))
	}
}

// TestKernelScenarioRefusesThePartsItDoesNotHave: history, the Fake Daemon's Input map and its directory
// validation each have their own answer in the new design, and the translator names it. The important half is
// the refusal: a scenario whose Sessions were dropped would leave the view test asserting on an empty
// transcript.
func TestKernelScenarioRefusesThePartsItDoesNotHave(t *testing.T) {
	cases := []struct {
		name     string
		scenario fakedaemon.Scenario
		feature  string
		needs    string
		detail   string
	}{
		{
			name: "Sessions",
			scenario: fakedaemon.Scenario{Sessions: []fakedaemon.SessionSpec{
				{Title: "Fix the login race", Cwd: "/Users/dev/acme-web"},
				{Title: "Add dark mode", Cwd: "/Users/dev/acme-web"},
			}},
			feature: "Scenario.Sessions",
			needs:   "conversation.import",
			detail:  "Fix the login race, Add dark mode",
		},
		{
			name:     "Input",
			scenario: fakedaemon.Scenario{Input: map[string]any{"skills": []any{}, "models": []any{}, "crons": []any{}}},
			feature:  "Scenario.Input",
			needs:    "skills, models",
			detail:   "crons, models, skills",
		},
		{
			name:     "ValidDirectories",
			scenario: fakedaemon.Scenario{ValidDirectories: []string{"/Users/dev/acme-web"}},
			feature:  "Scenario.ValidDirectories",
			needs:    "os.Stat",
			detail:   "/Users/dev/acme-web",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := testsupport.KernelScenario(c.scenario)
			var unsupported *testsupport.UnsupportedScenarioError
			if !errors.As(err, &unsupported) {
				t.Fatalf("err = %v, want an UnsupportedScenarioError", err)
			}
			if unsupported.Feature != c.feature {
				t.Errorf("feature %q, want %q", unsupported.Feature, c.feature)
			}
			if !strings.Contains(unsupported.Needs, c.needs) {
				t.Errorf("needs %q, want it to mention %q", unsupported.Needs, c.needs)
			}
			if unsupported.Detail != c.detail {
				t.Errorf("detail %q, want %q", unsupported.Detail, c.detail)
			}
			// The message carries both halves, so a failing test says what to do without reading this file.
			if !strings.Contains(err.Error(), c.feature) || !strings.Contains(err.Error(), c.needs) {
				t.Errorf("message %q does not name the feature and what it needs", err)
			}
		})
	}
}

// TestKernelScenarioHandsTheFakeKernelAWorkingScenario: the translated Scenario is one the Fake Kernel
// accepts, which is the seam's real contract. No Kernel is started — that is the migrated harness's job — so
// this checks the shape the package takes rather than a conversation.
func TestKernelScenarioHandsTheFakeKernelAWorkingScenario(t *testing.T) {
	turn := fakedaemon.Turn{Kind: "reply", Deltas: []string{"hi"}}
	got, err := testsupport.KernelScenario(fakedaemon.Scenario{Turn: &turn})
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	var sc fakekernel.Scenario = got
	if sc.AllowUnmatched {
		t.Error("AllowUnmatched is on, so a request no step describes would pass instead of failing")
	}
	if len(sc.Steps) != 1 || sc.Steps[0].Text != "hi" {
		t.Errorf("the Fake Kernel would get %+v", sc.Steps)
	}
}
