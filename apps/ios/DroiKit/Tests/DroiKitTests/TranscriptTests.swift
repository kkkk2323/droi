import DroidProtocol
import Foundation
import Testing

@testable import DroiKit

private func kinds(_ entry: TranscriptEntry) -> [String] {
    entry.blocks.map { block in
        switch block {
        case .text: "text"
        case .image: "image"
        case .thinking: "thinking"
        case .tools: "tools"
        case .subagent: "subagent"
        }
    }
}

private func calls(_ block: TranscriptBlock?) -> [ToolCall] {
    guard case .tools(_, let calls)? = block else {
        Issue.record("expected a tools block")
        return []
    }
    return calls
}

@Suite struct BuildTranscriptTests {
    @Test func attachesToolResultsToTheirCallsAndDropsToolMessages() {
        let entries = buildTranscript([
            message("user", [text("run it")]),
            message("assistant", [
                ["type": "thinking", "thinking": "plan", "signature": "", "durationMs": 42],
                toolUse("call-1", "Execute", ["command": "ls"]),
            ]),
            message("tool", [toolResult("call-1", "a\nb")]),
            message("assistant", [text("done")]),
        ])
        #expect(entries.map(\.role) == [.user, .assistant])
        #expect(kinds(entries[1]) == ["thinking", "tools", "text"])
        #expect(toolResultText(calls(entries[1].blocks[1])[0].result) == "a\nb")
    }

    @Test func inlineImagesBecomeImageBlocks() {
        let entries = buildTranscript([
            message("user", [
                text("see"),
                ["type": "image", "source": ["type": "base64", "mediaType": "image/png", "data": "AAAA"]],
            ])
        ])
        #expect(kinds(entries[0]) == ["text", "image"])
        guard case .image(_, let image) = entries[0].blocks[1] else {
            Issue.record("expected an image")
            return
        }
        #expect(image.dataURL == "data:image/png;base64,AAAA")
        #expect(image.data == Data([0, 0, 0]))
    }

    @Test func aTaskCallStandsOnItsOwnAndSplitsTheToolRun() {
        let entries = buildTranscript([
            message("assistant", [
                toolUse("a", "Read"),
                toolUse("t", "Task", ["subagent_type": "explorer"]),
                toolUse("b", "Grep"),
            ]),
            message("tool", [toolResult("t", "report")]),
        ])
        #expect(kinds(entries[0]) == ["tools", "subagent", "tools"])
        guard case .subagent(_, let task) = entries[0].blocks[1] else {
            Issue.record("expected a subagent block")
            return
        }
        #expect(toolResultText(task.result) == "report")
    }

    @Test func aScriptsCallsGoUnderTheLatestCallWatchingItsRun() {
        func nested(_ id: String, _ name: String) -> FactoryDroidMessage {
            message("assistant", [
                toolUse(id, name, extra: ["scriptExecution": ["runId": "run", "outerToolUseId": "run"]])
            ])
        }
        let entries = buildTranscript([
            message("user", [text("go")]),
            message("assistant", [toolUse("run", "Script", ["script": ""]), toolUse("other", "ToolSearch")]),
            nested("run-1", "Read"),
            message("tool", [toolResult("run-1", "text")]),
            nested("run-3", "Execute"),
            message("tool", [toolResult("run", "{\"toolCallId\":\"run\",\"status\":\"running\"}")]),
            message("assistant", [text("Still going; waiting.")]),
            message("assistant", [toolUse("wait", "WaitForScript", ["toolCallId": "run"])]),
            nested("run-5", "Execute"),
        ])
        #expect(entries.map(\.role) == [.user, .assistant])
        #expect(kinds(entries[1]) == ["tools", "text", "tools"])
        let first = calls(entries[1].blocks[0])
        let second = calls(entries[1].blocks[2])
        #expect(first.map(\.use.id) == ["run", "other"])
        #expect(first[0].nested?.map(\.use.id) == ["run-1", "run-3"])
        #expect(toolResultText(first[0].nested?[0].result) == "text")
        #expect(first[1].nested == nil)
        #expect(second.map(\.use.id) == ["wait"])
        #expect(second[0].nested?.map(\.use.id) == ["run-5"])
    }

    @Test func aScriptsCallWhoseRunIsNotInTheTranscriptStaysACallOfItsOwn() {
        let entries = buildTranscript([
            message("assistant", [
                toolUse("n", "Read", extra: ["scriptExecution": ["runId": "gone", "outerToolUseId": "gone"]])
            ])
        ])
        #expect(calls(entries[0].blocks[0]).map(\.use.id) == ["n"])
    }

    @Test func consecutiveToolCallsFormOneClusterAndTextSplitsClusters() {
        let entries = buildTranscript([
            message("assistant", [
                toolUse("a", "Read"), toolUse("b", "Grep"), text("found it"), toolUse("c", "Edit"),
            ])
        ])
        #expect(kinds(entries[0]) == ["tools", "text", "tools"])
        #expect(calls(entries[0].blocks[0]).count == 2)
        #expect(calls(entries[0].blocks[2]).count == 1)
    }

    @Test func consecutiveAssistantMessagesMergeAndAUserTurnSplitsThem() {
        let entries = buildTranscript([
            message("assistant", [toolUse("a", "Read")], ["createdAt": 1]),
            message("assistant", [toolUse("b", "Grep")], ["createdAt": 2]),
            message("assistant", [text("done")], ["createdAt": 3]),
            message("user", [text("thanks")]),
            message("assistant", [text("welcome")]),
        ])
        #expect(entries.map(\.role) == [.assistant, .user, .assistant])
        #expect(kinds(entries[0]) == ["tools", "text"])
        #expect(calls(entries[0].blocks[0]).count == 2)
        #expect(entries[0].createdAt == 3)
    }

    @Test func skipsEmptyAndHiddenMessages() {
        let entries = buildTranscript([
            message("assistant", []),
            message("user", [text("hidden")], ["isUserVisible": false]),
            message("assistant", [text("")]),
            // The Daemon's record of a hook run: a user message with nothing in it.
            message("user", [], ["hookEventName": "UserPromptSubmit", "hookStatus": "completed"]),
        ])
        #expect(entries.isEmpty)
    }

    @Test(arguments: ["system", "user"])
    func theDaemonsUserOnlyNoticesReadAsTheDroids(role: String) {
        func notice() -> FactoryDroidMessage {
            message(role, [text("You've reached your usage limit.")], ["visibility": "user_only"])
        }
        let entries = buildTranscript([
            message("user", [text("hi")]),
            message("user", [], ["hookEventName": "UserPromptSubmit", "visibility": "user_only"]),
            notice(),
            notice(),
        ])
        #expect(entries.map(\.role) == [.user, .assistant])
        #expect(kinds(entries[1]) == ["text", "text"])
        #expect(turnEnds(entries, running: false)[entries[1].id] != nil)
        #expect(buildTranscript([message("system", [text("x")])]).isEmpty)
    }

    @Test func aHookRecordBetweenTwoAssistantMessagesDoesNotSplitTheEntry() {
        let entries = buildTranscript([
            message("assistant", [toolUse("a", "Read")]),
            message("user", [], ["hookEventName": "UserPromptSubmit"]),
            message("assistant", [text("done")]),
        ])
        #expect(entries.map(\.role) == [.assistant])
    }

    @Test func toolResultsMayBeBlockArrays() {
        #expect(toolResultText(ToolResult(toolUseId: "x", content: [text("hi")])) == "hi")
    }

    @Test func aToolResultCarriesItsPicturesAndTheTextWithoutThem() {
        let result = ToolResult(
            toolUseId: "x",
            content: [
                text("Image file: shot.png"),
                ["type": "image", "source": ["type": "base64", "mediaType": "image/png", "data": "AAAA"]],
                ["type": "image", "source": ["type": "url", "url": "https://example.com/a.png"]],
            ])
        #expect(toolResultText(result) == "Image file: shot.png")
        #expect(toolResultImages(result).map(\.dataURL) == ["data:image/png;base64,AAAA"])
        #expect(toolResultImages(ToolResult(toolUseId: "x", content: "x")).isEmpty)
        #expect(toolResultImages(nil).isEmpty)
    }
}

@Suite struct TurnEndsTests {
    private func at(_ role: String, _ createdAt: Double, _ content: [JSONValue]) -> FactoryDroidMessage {
        message(role, content, ["createdAt": .number(createdAt), "updatedAt": .number(createdAt)])
    }

    @Test func aReplyFollowedByAUserMessageClosesTheTurn() {
        let entries = buildTranscript([
            at("user", 1_000, [text("go")]),
            at("assistant", 5_000, [text("done")]),
            at("user", 9_000, [text("thanks")]),
            at("assistant", 9_500, [text("welcome")]),
        ])
        let ends = turnEnds(entries, running: true)
        #expect(ends[entries[1].id] == TurnEnd(endedAt: 5_000, startedAt: 1_000))
        // The last entry is still being written.
        #expect(ends[entries[3].id] == nil)
        #expect(turnEnds(entries, running: false)[entries[3].id] == TurnEnd(endedAt: 9_500, startedAt: 9_000))
    }

    @Test func aUserMessageThatSteersATurnInProgressDoesNotCloseIt() {
        let entries = buildTranscript([
            at("user", 1_000, [text("go")]),
            at("assistant", 2_000, [toolUse("a", "Read")]),
            at("user", 3_000, [text("also check b")]),
            at("assistant", 8_000, [toolUse("b", "Read"), text("both done")]),
        ])
        let ends = turnEnds(entries, running: false)
        #expect(ends[entries[1].id] == nil)
        // The turn's duration counts from the prompt that started it.
        #expect(ends[entries[3].id] == TurnEnd(endedAt: 8_000, startedAt: 1_000))
    }

    @Test func theLastEntryClosesOnceTheDaemonRests() {
        let entries = buildTranscript([at("user", 1_000, [text("go")]), at("assistant", 2_000, [toolUse("a", "Read")])])
        #expect(turnEnds(entries, running: true).isEmpty)
        #expect(turnEnds(entries, running: false)[entries[1].id] == TurnEnd(endedAt: 2_000, startedAt: 1_000))
    }

    @Test func aTurnWithNoPromptHasATimeButNoDuration() {
        let entries = buildTranscript([at("assistant", 2_000, [text("hi")])])
        #expect(turnEnds(entries, running: false)[entries[0].id] == TurnEnd(endedAt: 2_000, startedAt: nil))
    }
}

@Suite struct FormatTests {
    private let zone = TimeZone(identifier: "Europe/Berlin")!
    private let locale = Locale(identifier: "en_GB")

    private func date(_ day: Int, _ hour: Int, _ minute: Int) -> Date {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = zone
        return calendar.date(from: DateComponents(year: 2026, month: 9, day: day, hour: hour, minute: minute))!
    }

    @Test func theTimeAloneWithoutAStartOrUnderASecond() {
        let now = date(29, 23, 30)
        let endedAt = date(29, 23, 13).timeIntervalSince1970 * 1000
        let format = { (end: TurnEnd) in formatTurnEnd(end, now: now, locale: locale, timeZone: zone) }
        #expect(format(TurnEnd(endedAt: endedAt, startedAt: nil)) == "23:13")
        #expect(format(TurnEnd(endedAt: endedAt, startedAt: endedAt - 400)) == "23:13")
        #expect(format(TurnEnd(endedAt: endedAt, startedAt: endedAt - 252_000)) == "23:13 · took 4m 12s")
    }

    @Test func anotherDayComesWithTheDate() {
        let stamp = formatTimestamp(
            date(27, 9, 5).timeIntervalSince1970 * 1000, now: date(29, 12, 0), locale: locale, timeZone: zone)
        #expect(stamp.hasPrefix("27 Sep") && stamp.hasSuffix(" 09:05"), "\(stamp)")
    }

    @Test func durationsPickTheUnitForTheSize() {
        #expect(formatDuration(640) == "640 ms")
        #expect(formatDuration(12_400) == "12s")
        #expect(formatDuration(252_000) == "4m 12s")
        #expect(formatDuration(3_780_000) == "1h 03m")
    }

    @Test func workingLabels() {
        #expect(workingLabel(.idle) == "")
        #expect(workingLabel(.waitingForToolConfirmation) == "Waiting for your approval")
        #expect(workingLabel("something_new") == "something_new")
    }
}

@Suite struct ReuseUnchangedTests {
    private let image: JSONValue = [
        "type": "image", "source": ["type": "base64", "mediaType": "image/png", "data": "AAAA"],
    ]

    private func imageOf(_ entries: [TranscriptEntry]) -> TranscriptImage? {
        for case .image(_, let image) in entries.first?.blocks ?? [] { return image }
        return nil
    }

    @Test func keepsEveryEntryWhenNothingChanged() {
        let messages = [
            message("user", [text("look"), image]), message("assistant", [toolUse("call-1", "Read")]),
            message("tool", [toolResult("call-1", "x")]),
        ]
        let first = buildTranscript(messages)
        let again = reuseUnchanged(first, buildTranscript(messages))
        #expect(again == first)
        #expect(imageOf(again) === imageOf(first))
    }

    @Test func replacesOnlyTheEntryThatChanged() {
        let asked = message("user", [text("look"), image])
        let reading = message("assistant", [toolUse("call-1", "Read")])
        let answered = message("tool", [toolResult("call-1", "x")])
        let first = buildTranscript([asked, reading])
        let next = reuseUnchanged(first, buildTranscript([asked, reading, answered]))
        #expect(next != first)
        #expect(imageOf(next) === imageOf(first))
        #expect(calls(next[1].blocks[0])[0].result != nil)
    }

    @Test func aStreamedTextKeepsEarlierEntriesAndRenewsTheGrowingOne() {
        let asked = message("user", [text("look"), image])
        func reply(_ body: String) -> FactoryDroidMessage { message("assistant", [text(body)], ["id": "reply"]) }
        let first = buildTranscript([asked, reply("Hel")])
        let next = reuseUnchanged(first, buildTranscript([asked, reply("Hello")]))
        #expect(imageOf(next) === imageOf(first))
        #expect(next[1].blocks[0] == .text(id: "reply:0", text: "Hello"))
        #expect(next[1].id == first[1].id)
    }
}

@Suite struct TranscriptRowsTests {
    @Test func rowsAreNewestFirstWithOneRowPerAssistantBlock() {
        let entries = buildTranscript([
            message("user", [text("go")], ["id": "u"]),
            message("assistant", [text("a"), toolUse("x", "Read"), text("b")], ["id": "r"]),
        ])
        let rows = transcriptRows(entries)
        #expect(rows.map(\.id) == ["r:2", "r:1", "r:0", "u"])
        #expect(rows.map(\.first) == [false, false, true, true])
        #expect(rows.map(\.last) == [true, false, false, true])
        #expect(streamingEntryId(entries, workingState: .streamingAssistantMessage) == "r")
        #expect(streamingEntryId(entries, workingState: .executingTool) == nil)
    }
}
