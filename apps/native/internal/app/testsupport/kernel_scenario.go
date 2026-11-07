// Package testsupport holds what the native app's tests need while they migrate from the Fake Daemon to
// the Fake Kernel: today, the one place the two Scenario shapes meet.
//
// Only the app's _test.go files may import it, so neither the Fake Daemon's types nor the Fake Kernel's
// reach the app binary. Delete it with the Fake Daemon (M5).
package testsupport

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/kkkk2323/droid-sdk-go/fakedaemon"

	"github.com/kkkk2323/droi/apps/native/pkg/kernel/fakekernel"
)

// The window's view tests were written against the Fake Daemon's Scenario, and most of them still are: they
// describe a Session's history and what the model answers, which the Fake Kernel describes too. KernelScenario
// is the one place the two shapes meet, so a test that only says "a Session with these messages, and the model
// replies like this" moves without touching its body.
//
// It is a migration crutch and says so: the parts of the Fake Daemon's Scenario that the Kernel has no
// counterpart for yet are refused by name, with the plan item that will give them one, rather than being
// dropped. A scenario that quietly lost its history would leave a view test asserting against an empty
// transcript — which passes and proves nothing.

// UnsupportedScenarioError says which part of a Fake Daemon Scenario the Fake Kernel cannot take yet, and what
// will let it: the plan item, so that a test hitting one is a note about the plan rather than a mystery.
type UnsupportedScenarioError struct {
	// Feature is the part of the Scenario, in the Scenario's own words.
	Feature string
	// Needs is what will make it translatable.
	Needs string
	// Detail is what the scenario actually held, for the failure message.
	Detail string
}

func (e *UnsupportedScenarioError) Error() string {
	msg := fmt.Sprintf("the Fake Kernel cannot take %s yet (%s)", e.Feature, e.Needs)
	if e.Detail != "" {
		msg += ": " + e.Detail
	}
	return msg
}

// turnNeeds is the milestone that gives each Fake Daemon Turn kind a Kernel counterpart; "" means the kind
// translates as it is.
var turnNeeds = map[string]string{
	"reply":             "",
	"permission":        "approvals land in M3 (3A), as approval.requested and a droi.approval entry",
	"askUser":           "asks land in M3 (3B), as ask.requested and a droi.ask entry",
	"queuedAskUser":     "a non-blocking ask lands in M3 (3B)",
	"backgroundProcess": "background processes land in M3 (3F), as bash_background and a droi.process entry",
	"todo":              "the Kernel has no todo turn; §11 of the plan does not keep it",
	"structured":        "structured output lands in M4 (4K), with model.complete and Memory 2.0",
	"spec":              "Spec mode is replaced by plan mode (Q4, M3)",
}

// KernelScenario translates a Fake Daemon Scenario into the Fake Kernel's script, or says which part of it is
// not translatable yet.
//
// One semantic difference the caller must know: the Fake Daemon's Turn answers every message a test sends,
// while the Fake Kernel's Steps are consumed one per model request and a request that matches no step fails
// the test. So one Turn becomes exactly one Step, and a test that sends two messages is asked to script both.
// That is deliberate: a reused reply would let a test's second turn go unasserted.
func KernelScenario(sc fakedaemon.Scenario) (fakekernel.Scenario, error) {
	var out fakekernel.Scenario
	if len(sc.Sessions) > 0 {
		return out, &UnsupportedScenarioError{
			Feature: "Scenario.Sessions",
			Needs:   "a way to write history, which conversation.import gives (M2, shared/interfaces/conversation-import.md)",
			Detail:  titlesOf(sc.Sessions),
		}
	}
	if len(sc.Input) > 0 {
		return out, &UnsupportedScenarioError{
			Feature: "Scenario.Input",
			Needs:   "the Kernel's own answers for skills, models, commands, files and crons (M3 3D, M4 4F)",
			Detail:  keysOf(sc.Input),
		}
	}
	if len(sc.ValidDirectories) > 0 {
		return out, &UnsupportedScenarioError{
			Feature: "Scenario.ValidDirectories",
			Needs:   "nothing: a working directory is checked with os.Stat, not asked of a kernel (gap-analysis §1.2 #13)",
			Detail:  strings.Join(sc.ValidDirectories, ", "),
		}
	}
	if sc.Turn == nil {
		// The Fake Daemon answers nothing without a turn; so does the Fake Kernel, strictly: a request that
		// matches no step fails the test rather than being answered with silence.
		return out, nil
	}
	step, err := kernelStep(*sc.Turn)
	if err != nil {
		return out, err
	}
	out.Steps = []fakekernel.Step{step}
	return out, nil
}

// kernelStep translates one Turn.
func kernelStep(turn fakedaemon.Turn) (fakekernel.Step, error) {
	needs, known := turnNeeds[turn.Kind]
	if !known {
		return fakekernel.Step{}, &UnsupportedScenarioError{
			Feature: "Turn.Kind " + turn.Kind,
			Needs:   "a Kernel counterpart, which no milestone names yet",
		}
	}
	if needs != "" {
		return fakekernel.Step{}, &UnsupportedScenarioError{Feature: "Turn.Kind " + turn.Kind, Needs: needs}
	}
	return fakekernel.Step{
		// The Fake Daemon's turn answers any message; the Fake Kernel's step is taken by the first request of
		// that message, which is the same thing for a test that sends one.
		Chunks: turn.Deltas,
		Text:   strings.Join(turn.Deltas, ""),
		Delay:  time.Duration(turn.DelayMs) * time.Millisecond,
	}, nil
}

func titlesOf(sessions []fakedaemon.SessionSpec) string {
	titles := make([]string, len(sessions))
	for i, s := range sessions {
		titles[i] = s.Title
	}
	return strings.Join(titles, ", ")
}

func keysOf(input map[string]any) string {
	keys := make([]string, 0, len(input))
	for k := range input {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}
