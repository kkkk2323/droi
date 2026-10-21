import DroidProtocol
import Foundation
import Testing

@testable import DroiKit

@Suite struct AlertTests {
    @Test func aTurnThatEndsIsOneCompletionIdleWithoutWorkIsNothing() {
        var tracker = AlertTracker()
        #expect(tracker.update("a", workingState: .idle) == nil)
        #expect(tracker.update("a", workingState: .thinking) == nil)
        #expect(tracker.update("a", workingState: .streamingAssistantMessage) == nil)
        #expect(tracker.update("a", workingState: .idle) == .completion)
        #expect(tracker.update("a", workingState: .idle) == nil)
    }

    @Test func waitingForAnAnswerAlertsOncePerWaitThenTheTurnStillCompletes() {
        var tracker = AlertTracker()
        #expect(tracker.update("a", workingState: .executingTool) == nil)
        #expect(tracker.update("a", workingState: .waitingForToolConfirmation) == .awaitingInput)
        #expect(tracker.update("a", workingState: .waitingForToolConfirmation) == nil)
        #expect(tracker.update("a", workingState: .executingTool) == nil)
        #expect(tracker.update("a", workingState: .idle) == .completion)
        // A new turn that waits again is a new wait.
        #expect(tracker.update("a", workingState: .waitingForToolConfirmation) == .awaitingInput)
    }

    @Test func sessionsAreTrackedApartAndAForgottenOneDoesNotComplete() {
        var tracker = AlertTracker()
        _ = tracker.update("a", workingState: .thinking)
        _ = tracker.update("b", workingState: .thinking)
        tracker.forget("a")
        #expect(tracker.update("a", workingState: .idle) == nil)
        #expect(tracker.update("b", workingState: .idle) == .completion)
    }

    @Test func byDefaultEachEventPlaysItsOwnSoundAndHaptic() {
        let both = feedbackFor(AlertSwitches(), .completion)
        #expect(both.sound == .finished && both.haptic == .finished)
        let waiting = feedbackFor(AlertSwitches(), .awaitingInput)
        #expect(waiting.sound == .needsInput && waiting.haptic == .needsInput)
    }

    @Test func aSwitchTurnsOffOnlyItsOwnEventAndKind() {
        let switches = AlertSwitches(finishedSound: false, needsInputHaptic: false)
        let finished = feedbackFor(switches, .completion)
        #expect(finished.sound == nil && finished.haptic == .finished)
        let waiting = feedbackFor(switches, .awaitingInput)
        #expect(waiting.sound == .needsInput && waiting.haptic == nil)
    }

    @Test func storedSwitchesAreKeptAndAnythingMalformedFallsBackToOn() throws {
        let stored = AlertSwitches(needsInputSound: false)
        #expect(try JSONDecoder().decode(AlertSwitches.self, from: JSONEncoder().encode(stored)) == stored)
        let partial = Data(#"{"finishedSound":"no","needsInputHaptic":false}"#.utf8)
        #expect(try JSONDecoder().decode(AlertSwitches.self, from: partial) == AlertSwitches(needsInputHaptic: false))
        #expect(try JSONDecoder().decode(AlertSwitches.self, from: Data("null".utf8)) == AlertSwitches())
    }
}

@Suite struct ActivityTests {
    @Test func tellsCompactingFromWorkingAndWaiting() {
        #expect(activityOf(.compactingConversation) == .compacting)
        #expect(activityOf(.executingTool) == .working)
        #expect(activityOf(.waitingForToolConfirmation) == .needsInput)
        #expect(activityOf(.idle) == nil)
        #expect(activityOf(nil) == nil)
    }

    @Test func countsWorkingApartFromWaiting() {
        let activity: [String: SessionActivity] = ["a": .working, "b": .needsInput, "c": .compacting]
        #expect(countBusy(["a", "b", "c", "d"], activity: activity, subagentsRunning: ["d": 2]) == Busy(working: 3, needsInput: 1))
        #expect(countBusy(["a", "b", "c", "d"], activity: [:], subagentsRunning: ["a": 0]) == nil)
    }

    @Test func aLoadedSessionKnowsBestAndACompactOnTop() {
        let merged = sessionActivity(
            reported: ["a": .executingTool, "b": .executingTool, "c": .idle],
            loaded: ["b": .idle, "d": .waitingForToolConfirmation], compacting: ["c"])
        #expect(merged == ["a": .working, "c": .compacting, "d": .needsInput])
    }
}

@Suite struct ToolModeTests {
    @Test func asksForNothingAndShowsTheDraftWhenDroidDecides() {
        let mode = newSessionToolMode(choice: nil, fallback: nil, draft: .scriptOnly)
        #expect(mode.shown == .scriptOnly && mode.requested == nil)
    }

    @Test func asksForTheDeviceDefaultAndShowsItBeforeTheDraftAnswers() {
        let mode = newSessionToolMode(choice: nil, fallback: .scriptOnly, draft: nil)
        #expect(mode.shown == .scriptOnly && mode.requested == .scriptOnly)
    }

    @Test func thePageChoiceWins() {
        let mode = newSessionToolMode(choice: .directOnly, fallback: .scriptOnly, draft: .scriptOnly)
        #expect(mode.shown == .directOnly && mode.requested == .directOnly)
        let none = newSessionToolMode(choice: nil, fallback: nil, draft: nil)
        #expect(none.shown == nil && none.requested == nil)
    }

    @Test func theStoredDefaultReadsOnlyTheModesDroidKnows() {
        // droid 0.244 no longer has direct_and_script, which Droi once offered.
        let storage = MemoryPreferenceStorage(["droi.toolExecutionMode": "direct_and_script"])
        #expect(ToolMode(ToolExecutionMode.scriptOnly) == .scriptOnly)
        #expect(ToolMode(ToolExecutionMode.directAndScript) == nil)
        #expect(Preferences.defaultToolMode.get(storage) == nil)
        Preferences.defaultToolMode.set(.scriptOnly, in: storage)
        #expect(storage.string(forKey: "droi.toolExecutionMode") == "script_only")
        #expect(Preferences.defaultToolMode.get(storage) == .scriptOnly)
        Preferences.defaultToolMode.set(nil, in: storage)
        #expect(storage.string(forKey: "droi.toolExecutionMode") == "")
    }

    @Test func textSizesScaleTheText() {
        #expect(TextSize.allCases.map(\.label) == ["Small", "Default", "Large", "Largest"])
        #expect(TextSize.largest.scale == 1.1875)
        #expect(Preferences.textSize.get(MemoryPreferenceStorage(["droi.textSize": "huge"])) == .default)
    }
}
