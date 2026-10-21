import DroidProtocol
import Testing

@testable import DroiKit

private func task(_ content: String? = nil, isError: Bool = false, input: [String: JSONValue] = [:]) -> ToolCall {
    call("Task", input, result: content.map(JSONValue.string), isError: isError, id: "call")
}

private func run(_ status: SubagentStatus) -> SubagentRun {
    SubagentRun(status: status)
}

@Suite struct SubagentsTests {
    let main = summary("main", title: "Main")
    let older = summary("a", updatedAt: 1, callingSessionId: "main")
    let newer = summary("b", updatedAt: 2, callingSessionId: "main")
    let nested = summary("c", title: "Nested", callingSessionId: "b")
    var sessions: [SessionSummary] { [main, older, newer, nested] }

    @Test func onlyMainSessionsAreListed() {
        #expect(mainSessions(sessions) == [main])
    }

    @Test func subagentsOfListsTheDirectSubagentsNewestFirst() {
        #expect(subagentsOf(sessions, callers: ["main"]).map(\.sessionId) == ["b", "a"])
        #expect(subagentsOf(sessions, callers: ["b"]).map(\.sessionId) == ["c"])
    }

    @Test func callerTrailWalksUpToTheMainSession() {
        #expect(
            callerTrail(sessions, of: nested)
                == [SessionRef(sessionId: "main", title: "Main"), SessionRef(sessionId: "b", title: "t")])
        #expect(callerTrail(sessions, of: main).isEmpty)
        #expect(
            callerTrail([], sessionId: "x", callingSessionId: "gone")
                == [SessionRef(sessionId: "gone", title: "Main session")])
    }

    @Test func aCallerContinuedAfterACompactionLeadsBackToItsLatestLink() {
        let main2 = summary("main2", title: "Main", parentId: "main")
        let main3 = summary("main3", title: "Main again", parentId: "main2")
        let late = summary("d", updatedAt: 3, callingSessionId: "main3")
        let all = sessions + [main2, main3, late]
        #expect(
            callerTrail(all, of: nested)
                == [SessionRef(sessionId: "main3", title: "Main again"), SessionRef(sessionId: "b", title: "t")])
        #expect(subagentSiblings(all, of: older).map(\.sessionId) == ["d", "b", "a"])
        #expect(subagentSiblings(all, of: nested).map(\.sessionId) == ["c"])
        #expect(subagentSiblings(all, of: main).isEmpty)
    }

    @Test func aSubagentBelongsToItsMainSessionsRowAtTheLatestCompaction() {
        let continued = summary("main2", parentId: "main")
        #expect(listedSessionOf(sessions + [continued], "c") == "main2")
        #expect(listedSessionOf(sessions, "main") == "main")
    }

    @Test func runningSubagentsCountsPerListedRow() {
        let runs = ["a": run(.completed), "b": run(.running), "c": run(.pending)]
        #expect(runningSubagents(sessions, runs: runs) == ["main": 2])
    }

    @Test func aRunFromTheSummaryOrTheWorkingState() {
        #expect(subagentRun(status: nil, toolUseCount: nil, durationMs: nil, workingState: .executingTool)?.status == .running)
        #expect(subagentRun(status: nil, toolUseCount: nil, durationMs: nil, workingState: .idle) == nil)
        #expect(subagentRun(status: .completed, toolUseCount: 3, durationMs: 9, workingState: nil)?.toolUseCount == 3)
    }

    @Test func readsTheRequestFromTheInput() {
        #expect(
            taskRequest(task(input: ["subagent_type": "explorer", "description": "Map it", "prompt": "Go"]))
                == TaskRequest(subagentType: "explorer", description: "Map it", prompt: "Go"))
        #expect(subagentName("explorer") == "Explorer")
        #expect(subagentName("") == "Subagent")
    }

    @Test func aBackgroundLaunchNamesTheSubagentsSessionAndIsNoReport() {
        let launch = task("Task launched in background.\ntask_id: s-1\nsession_id: s-1\nsubagent_type: explorer")
        #expect(launchedSessionId(launch) == "s-1")
        #expect(taskState(launch, run: nil) == .launched)
        #expect(taskReport(launch) == "")
    }

    @Test func theDaemonsAccountWinsExceptForAFailedCall() {
        #expect(taskState(task(), run: nil) == .running)
        #expect(taskState(task("report"), run: nil) == .completed)
        #expect(taskReport(task("report")) == "report")
        #expect(taskState(task("Task launched in background."), run: run(.completed)) == .completed)
        #expect(taskState(task("boom", isError: true), run: run(.running)) == .failed)
    }

    @Test func formatsRunDurations() {
        #expect(formatRunDuration(4_200) == "4s")
        #expect(formatRunDuration(134_000) == "2m 14s")
        #expect(formatRunDuration(3_900_000) == "1h 5m")
    }

    @Test func namesTheTaskCallsTheListLacks() {
        func entry(_ call: ToolCall) -> TranscriptEntry {
            TranscriptEntry(id: "e", role: .assistant, blocks: [.subagent(id: "e:0", call: call)], createdAt: 0, isError: false)
        }
        let listed = ["call": summary("s")]
        #expect(unlistedTaskCalls([entry(task())], byToolUse: listed) == "")
        #expect(unlistedTaskCalls([entry(task())], byToolUse: [:]) == "call")
        #expect(unlistedTaskCalls([entry(task("session_id: s"))], byToolUse: [:]) == "call:answered")
        #expect(subagentsByToolUse([summary("s", callingToolUseId: "call")])["call"]?.sessionId == "s")
    }
}
