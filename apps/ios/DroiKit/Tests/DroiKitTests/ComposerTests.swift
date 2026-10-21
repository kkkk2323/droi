import DroidProtocol
import Foundation
import Testing

@testable import DroiKit

private func item(_ name: String, _ description: String = "", _ kind: SlashItem.Kind = .command) -> SlashItem {
    SlashItem(name: name, description: description, kind: kind)
}

@Suite struct ComposerTests {
    @Test func fitWithinScalesTheLongestEdgeDownAndNeverUp() {
        #expect(fitWithin(width: 4000, height: 2000, max: 1568) == (1568, 784))
        #expect(fitWithin(width: 500, height: 3136, max: 1568) == (250, 1568))
        #expect(fitWithin(width: 800, height: 600, max: 1568) == (800, 600))
    }

    @Test func slashQueryIsTheWordAfterALeadingSlashWhileTheCaretIsInsideIt() {
        #expect(slashQuery("/", caret: 1) == "")
        #expect(slashQuery("/han", caret: 4) == "han")
        #expect(slashQuery("/han", caret: 2) == "h")
    }

    @Test func slashQueryIsNilOnceArgumentsStartOrWhenTheSlashIsNotFirst() {
        #expect(slashQuery("/handoff ", caret: 9) == nil)
        #expect(slashQuery("/handoff foo", caret: 12) == nil)
        #expect(slashQuery("hello /x", caret: 8) == nil)
        #expect(slashQuery("", caret: 0) == nil)
    }

    @Test func prefixMatchesFirstThenNamesContainingTheQuery() {
        let items = [
            item("opsx-propose", "Propose a change"), item("handoff", "Compact the conversation", .skill),
            item("review", "Review the diff for a proposal", .skill),
        ]
        #expect(filterSlashItems(items, query: "pro").map(\.name) == ["opsx-propose"])
        #expect(filterSlashItems(items, query: "rev").map(\.name) == ["review"])
        #expect(filterSlashItems(items, query: "op").map(\.name) == ["opsx-propose"])
        #expect(filterSlashItems(items, query: "").map(\.name) == ["opsx-propose", "handoff", "review"])
        #expect(filterSlashItems(items, query: "off").map(\.name) == ["handoff"])
    }

    @Test func capsTheList() {
        #expect(filterSlashItems((0..<20).map { item("cmd\($0)") }, query: "cmd").count == 8)
    }

    @Test func builtinsComeFirstAndShadowADaemonItemOfTheSameName() {
        let merged = mergeSlashItems([item("compact", "builtin")], [item("compact", "workspace"), item("handoff", "", .skill)])
        #expect(merged.map(\.name) == ["compact", "handoff"])
        #expect(merged[0].description == "builtin")
        #expect(mergeSlashItems([], [item("handoff", "", .skill)]).map(\.name) == ["handoff"])
        #expect(mergeSlashItems([], []).isEmpty)
    }

    @Test func theDaemonsCommandsThenTheSkillsAUserMayInvoke() {
        let items = slashItems(
            commands: [
                CustomCommandInfo(name: "deploy", description: "Ship it", argumentHint: "<env>"),
                CustomCommandInfo(name: "deploy", description: "again"),
            ],
            skills: [
                SkillInfo(name: "review", description: "Review", location: .builtin, filePath: "/r"),
                SkillInfo(name: "deploy", location: .project, filePath: "/d"),
                SkillInfo(name: "hidden", location: .builtin, filePath: "/h", userInvocable: false),
                SkillInfo(name: "off", location: .builtin, filePath: "/o", enabled: false),
            ])
        #expect(
            items == [
                SlashItem(name: "deploy", description: "Ship it", argumentHint: "<env>", kind: .command),
                SlashItem(name: "review", description: "Review", kind: .skill),
            ])
    }

    @Test func pickedSlashItemIsTheKnownNameAMessageStartsWithOnceASpaceEndsIt() {
        let items = [item("handoff", "", .skill), item("compact")]
        let picked = pickedSlashItem("/handoff carry on", items: items)
        #expect(picked?.item == items[0])
        #expect(picked?.rest == "carry on")
        #expect(pickedSlashItem("/compact ", items: items)?.rest == "")
        #expect(pickedSlashItem("/handoff", items: items) == nil)
        #expect(pickedSlashItem("/nope carry on", items: items) == nil)
        #expect(pickedSlashItem("say /handoff ", items: items) == nil)
    }

    @Test func queuedKindsSayWhoHoldsThemAndWhetherTheySteer() {
        #expect(QueuedMessageKind.daemonQueuedDiscardable.isHeldByDaemon)
        #expect(QueuedMessageKind.daemonQueuedDiscardable.isSteering)
        #expect(QueuedMessageKind.daemonQueuedEndOfLoop.isHeldByDaemon)
        #expect(!QueuedMessageKind.daemonQueuedEndOfLoop.isSteering)
        #expect(QueuedMessageKind.localDeferredAfterEsc.isSteering)
        #expect(!QueuedMessageKind.localDeferredAfterEsc.isHeldByDaemon)
        #expect(QueuedMessageKind.localPausedAfterEsc.isPaused)
        #expect(
            [QueuedMessageKind.localPausedAfterEsc, .localDeferredAfterEsc, .daemonQueuedEndOfLoop].map(\.label)
                == ["Paused", "Next", "Queued"])
    }

    @Test func aQueuedMessageReadsAsTextAndComesBackWithItsImages() throws {
        let content = try ([
            text("first"), text(""),
            ["type": "image", "source": ["type": "base64", "mediaType": "image/png", "data": "AAAA"]],
            text("second"),
        ] as JSONValue).decode([ContentBlock].self)
        #expect(queuedText(content) == "first [image] second")
        let back = queuedContent(content)
        #expect(back.text == "first\nsecond")
        #expect(back.images.map(\.mediaType) == [.png])
        #expect(back.images.map(\.data) == ["AAAA"])
        let draft = QueuedContent(text: "typed", images: [])
        #expect(insertTaken(back, into: draft).text == "first\nsecond\ntyped")
        #expect(insertTaken(back, into: QueuedContent(text: "  ", images: [])).text == "first\nsecond")
    }

    @Test func promptsAnswerWithTheTypedTextOrTheChoices() {
        #expect(isAlreadyResolved("No pending permission request"))
        #expect(isAlreadyResolved("Request NOT FOUND"))
        #expect(!isAlreadyResolved("Daemon unavailable"))
        #expect(askUserAnswer(index: 0, question: "Q", chosen: ["a", "b"], typed: " ").answer == "a, b")
        #expect(askUserAnswer(index: 1, question: "Q", chosen: ["a"], typed: "mine").answer == "mine")
    }

    @Test func newIdsAreLowerCaseV4() {
        let id = newID()
        #expect(id == id.lowercased())
        #expect(id.count == 36)
        #expect(Array(id)[14] == "4")
        #expect(id != newID())
    }
}

@Suite struct SearchTests {
    @Test func splitsASnippetIntoPlainAndMatchedRuns() {
        #expect(
            snippetRuns("…the <mark>sidebar</mark> width is off") == [
                SnippetRun(text: "…the ", match: false, offset: 0),
                SnippetRun(text: "sidebar", match: true, offset: 5),
                SnippetRun(text: " width is off", match: false, offset: 12),
            ])
    }

    @Test func dropsOtherTagsAndCollapsesLineBreaks() {
        #expect(
            snippetRuns("a\n  <b>b</b> <mark>c</mark>") == [
                SnippetRun(text: "a b ", match: false, offset: 0), SnippetRun(text: "c", match: true, offset: 4),
            ])
        #expect(snippetRuns("1 < 2 <mark>x</mark>").first?.text == "1 < 2 ")
    }

    @Test func anEmptySnippetHasNoRuns() {
        #expect(snippetRuns("").isEmpty)
    }

    @Test func aHitReadsItsTitleTimeAndBestSnippet() throws {
        let result = try ([
            "sessionId": "s", "title": " ", "updatedAt": 1_700_000_123_456,
            "hits": [["docId": "d", "kind": "message_text", "snippets": ["<mark>a</mark>"]]],
        ] as JSONValue).decode(DaemonSearchSessionsSessionResult.self)
        let hit = SessionSearchHit(result)
        #expect(hit.title == "Untitled session")
        #expect(hit.updatedAt == 1_700_000_123)
        #expect(hit.snippet == [SnippetRun(text: "a", match: true, offset: 0)])
        #expect(isSearchable(" ab ") && !isSearchable(" a "))
        #expect(searchParams(" ab ").query == "ab")
    }
}
