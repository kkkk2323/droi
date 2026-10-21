import XCTest

/// Opening a Session, long Sessions, streamed replies, tool calls and reasoning.
final class TranscriptTests: DroiTestCase {
    private static func conversation(_ count: Int) -> [(role: String, text: String)] {
        (0..<count).map { $0 % 2 == 0 ? ("user", "Question \($0)") : ("assistant", "Answer \($0)") }
    }

    /// Takes the tap on the status bar, which sends a scroll view to its top, until the element shows.
    @discardableResult
    private func scrollToOldest(_ element: XCUIElement, taps: Int = 30) -> XCUIElement {
        var left = taps
        while !element.exists, left > 0 {
            app.statusBars.firstMatch.tap()
            Thread.sleep(forTimeInterval: 0.4)
            left -= 1
        }
        return element
    }

    func testOpensOnTheLatestMessageAndTheEarlierOnesComeByScrolling() {
        startDaemon(sessions: [DroiTestCase.session("Long chat", in: "/Users/dev/acme-web", Self.conversation(120))])
        launchToList()
        tap("Long chat", timeout: 15)
        let latest = see(el("Answer 119"), timeout: 15)
        XCTAssertTrue(latest.isHittable)
        XCTAssertFalse(button("Scroll to latest").exists)

        see(scrollToOldest(el("Question 0")))
        see(button("Scroll to latest")).tap()
        eventually { self.el("Answer 119").isHittable }
        gone(button("Scroll to latest"))
    }

    func testASessionLongerThanOneLoadOffersThePreviousMessagesAtTheTop() {
        startDaemon(sessions: [DroiTestCase.session("Longer chat", in: "/Users/dev/acme-web", Self.conversation(450))])
        launchToList()
        tap("Longer chat", timeout: 15)
        see(el("Answer 449"), timeout: 15)
        // 400 come with the load, the 50 before them on request.
        XCTAssertEqual(daemon.waitForRequest("daemon.load_session")?["messageLimit"] as? Int, 400)
        let more = see(scrollToOldest(button("Load previous messages")), "the button for the previous messages")
        let reading = see(el("Question 50"))
        XCTAssertFalse(el("Answer 49").exists)
        let before = reading.frame.minY

        more.tap()
        let older = daemon.waitForRequest("daemon.get_session_messages")
        XCTAssertEqual(older?["limit"] as? Int, 100)
        XCTAssertNotNil(older?["cursor"] as? String)
        gone(button("Load previous messages"))
        // They come in above, out of sight; what was being read stays where it was.
        Thread.sleep(forTimeInterval: 0.6)
        XCTAssertEqual(el("Question 50").frame.minY, before, accuracy: 40)
        see(scrollToOldest(el("Question 0")))
    }

    func testASessionContinuedAfterTwoCompactionsOffersEachEarlierSessionInTurn() {
        let (firstId, secondId) = ("0c1d2e3f-0000-4000-8000-00000000c001", "0c1d2e3f-0000-4000-8000-00000000c002")
        func continues(_ parent: String) -> [String: Any] { ["tags": [["name": "droi.continues", "metadata": ["parent": parent]]]] }
        startDaemon(sessions: [
            DroiTestCase.session("Plan v1", in: "/Users/dev/acme-web", [("user", "first question"), ("assistant", "first answer")], extra: ["sessionId": firstId]),
            DroiTestCase.session("Plan v2", in: "/Users/dev/acme-web", [("user", "middle question"), ("assistant", "middle answer")], extra: continues(firstId).merging(["sessionId": secondId]) { $1 }),
            DroiTestCase.session("Plan v3", in: "/Users/dev/acme-web", [("user", "latest question")], extra: continues(secondId)),
        ])
        launchToList()
        XCTAssertFalse(button("Plan v1").exists)
        tap("Plan v3")
        see(el("latest question"), timeout: 15)
        XCTAssertFalse(el("middle question").exists)

        tap("Continued from \u{201C}Plan v2\u{201D} \u{00B7} Show earlier messages")
        see(el("middle answer"))
        XCTAssertFalse(el("first question").exists)
        let boundaries = app.descendants(matching: .any).matching(identifier: "Context compacted here")
        eventually { boundaries.count == 1 }

        tap("Continued from \u{201C}Plan v1\u{201D} \u{00B7} Show earlier messages")
        see(el("first question"))
        eventually { boundaries.count == 2 }
        XCTAssertFalse(button(containing: "Continued from").exists)
        let order = ["first question", "middle question", "latest question"].map { el($0).frame.minY }
        XCTAssertEqual(order, order.sorted())
    }

    // MARK: Streamed replies

    private let chat = DroiTestCase.session("Chat", in: "/Users/dev/acme-web", [("user", "hello"), ("assistant", "Hi. What should we build?")])

    func testAReplyRendersMarkdownAsItArrivesAndFollowsIt() {
        let filler = (0..<30).map { "- point \($0)\n" }.joined()
        startDaemon(sessions: [chat], turn: [
            "kind": "reply", "delayMs": 400,
            "deltas": [
                "## The plan\n\n", filler,
                "\n**Bold** move with `npm test` and [docs](https://example.com)\n\n```ts\nconst answer", " = 42\n```\n\n| a | b |\n| - | - |\n| 1 | 2 |\n\nThe end.",
            ],
        ])
        launchToList()
        openSession("Chat")
        send("plan it")
        see(el("The plan"), timeout: 20)
        // A fence still open while streaming already shows as code.
        see(el("ts code"), timeout: 20)
        let end = see(el("The end."), timeout: 20)
        eventually { end.isHittable }
        see(el("const answer = 42"))
        see(app.links["docs"])
        see(app.staticTexts["2"])
        let copy = see(button("Copy code"))
        copy.tap()
        see(button("Copied"))
    }

    func testReadingFurtherUpWhileAReplyStreamsLeavesTheViewWhereItIs() {
        let paragraphs = (0..<8).map { i in String(repeating: "Paragraph \(i) ", count: 20) + "\n\n" }
        startDaemon(
            sessions: [DroiTestCase.session("Chat", in: "/Users/dev/acme-web", Self.conversation(40))],
            turn: ["kind": "reply", "deltas": paragraphs, "delayMs": 500])
        launchToList()
        openSession("Chat")
        send("more")
        see(el(containing: "Paragraph 0"), timeout: 15)
        see(scrollToOldest(el("Question 0")))
        XCTAssertTrue(button("Scroll to latest").waitForExistence(timeout: 5))
        Thread.sleep(forTimeInterval: 5)
        XCTAssertTrue(el("Question 0").exists, "the view followed the stream")

        tap("Scroll to latest")
        eventually(10) { self.el(containing: "Paragraph 7").isHittable }
        gone(button("Scroll to latest"))
    }

    func testATableLinesItsColumnsUpAcrossRows() {
        startDaemon(sessions: [chat], turn: [
            "kind": "reply",
            "deltas": [
                "| \u{72B6}\u{6001} | \u{7BA1}\u{7406}\u{5458}\u{9080}\u{8BF7}\u{7801} | Other codes |\n| - | - | - |\n"
                    + "| \u{5E73}\u{65F6} | \u{53EF}\u{7528} | see the current setting |\n"
                    + "| \u{6536}\u{7D27}\u{6A21}\u{5F0F}\u{FF08}\u{65B0}\u{589E}\u{FF09} | \u{53EF}\u{7528} | \u{4E0D}\u{53EF}\u{7528} |\n"
            ],
        ])
        launchToList()
        openSession("Chat")
        send("table")
        let ordinary = see(el("\u{5E73}\u{65F6}"), timeout: 15)
        let tight = see(el("\u{6536}\u{7D27}\u{6A21}\u{5F0F}\u{FF08}\u{65B0}\u{589E}\u{FF09}"))
        XCTAssertEqual(ordinary.frame.minX, tight.frame.minX, accuracy: 1)
        let available = app.staticTexts.matching(identifier: "\u{53EF}\u{7528}")
        XCTAssertEqual(available.count, 2)
        XCTAssertEqual(available.element(boundBy: 0).frame.minX, available.element(boundBy: 1).frame.minX, accuracy: 1)
        XCTAssertEqual(el("see the current setting").frame.minX, el("\u{4E0D}\u{53EF}\u{7528}").frame.minX, accuracy: 1)
    }

    // MARK: Tool calls and reasoning

    private static let pixel = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="

    private static func toolTurn() -> [[String: Any]] {
        let call = DroiTestCase.toolCall
        let result = DroiTestCase.toolResult
        let edit: [String: Any] = [
            "success": true, "file_path": "src/auth.ts",
            "diffLines": [
                ["type": "unchanged", "content": "export async function run() {", "lineNumber": ["old": 1, "new": 1]],
                ["type": "removed", "content": "  login()", "lineNumber": ["old": 2]],
                ["type": "added", "content": "  await login()", "lineNumber": ["new": 2]],
                ["type": "unchanged", "content": "}", "lineNumber": ["old": 3, "new": 3]],
            ],
        ]
        func json(_ object: Any) -> String { String(decoding: try! JSONSerialization.data(withJSONObject: object), as: UTF8.self) }
        return [
            DroiTestCase.message("user", "Fix login and add a test"),
            DroiTestCase.message("assistant", content: [
                ["type": "thinking", "thinking": "I should read the file before changing it.", "signature": "", "durationMs": 1200],
                ["type": "tool_use", "id": "read", "name": "Read", "input": ["file_path": "src/auth.ts"]],
            ]),
            result("read", "export function login() {}"),
            call("shot", "Read", ["file_path": "/tmp/shot.png"]),
            result("shot", [
                ["type": "text", "text": "Image file: shot.png (original size: 1.2 KB)"],
                ["type": "image", "source": ["type": "base64", "mediaType": "image/png", "data": pixel]],
            ]),
            call("test", "Execute", ["command": "npm test", "summary": "Run the tests"]),
            result("test", "Error: 1 test failed", isError: true),
            call("edit", "Edit", ["file_path": "src/auth.ts", "old_str": "login()", "new_str": "await login()"]),
            result("edit", json(edit)),
            call("create", "Create", ["file_path": "src/auth.test.ts", "content": "import { login } from './auth'\ntest('logs in', () => login())\n"]),
            result("create", json(["success": true, "file_path": "src/auth.test.ts"])),
            DroiTestCase.message("assistant", "Fixed: `login` is awaited now."),
        ]
    }

    func testRowsShowSuccessAndFailureAndOpenToTheirDetails() {
        startDaemon(sessions: [DroiTestCase.session("Login fix", in: "/Users/dev/acme-web", messages: Self.toolTurn())])
        launchToList()
        openSession("Login fix")
        let read = see(button("Read: src/auth.ts"))
        XCTAssertTrue((read.value as? String ?? "").hasPrefix("Succeeded"), "\(read.value ?? "")")
        let tests = see(button("Execute: Run the tests"))
        XCTAssertTrue((tests.value as? String ?? "").hasPrefix("Failed"), "\(tests.value ?? "")")

        tests.tap()
        let details = see(el("Execute details"))
        XCTAssertTrue(details.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS 'npm test'")).firstMatch.exists)
        XCTAssertTrue(details.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS 'Error: 1 test failed'")).firstMatch.exists)

        // A picture a tool handed back shows in its detail, from the result itself.
        XCTAssertFalse(el("Image from Droid").exists)
        tap("Read: /tmp/shot.png")
        see(el("Image from Droid"))
        see(el("Image file: shot.png (original size: 1.2 KB)"))

        // The reply closes the turn; its time says how long the turn took
        // (the fixtures are a second apart: prompt to reply is 11 seconds).
        see(el(containing: "took 11s"))
    }

    func testAnEditShowsALineDiffAndACreateTheNewFile() {
        startDaemon(sessions: [DroiTestCase.session("Login fix", in: "/Users/dev/acme-web", messages: Self.toolTurn())])
        launchToList()
        openSession("Login fix")
        let edit = see(button("Edit: src/auth.ts"))
        XCTAssertTrue((edit.value as? String ?? "").contains("+1 \u{2212}1"), "\(edit.value ?? "")")
        edit.tap()
        let diff = see(el("Edit details"))
        see(diff.descendants(matching: .any)["Removed:   login()"])
        see(diff.descendants(matching: .any)["Added:   await login()"])

        let create = see(button("Create: src/auth.test.ts"))
        XCTAssertTrue((create.value as? String ?? "").contains("+2"), "\(create.value ?? "")")
        create.tap()
        see(el("Create details").descendants(matching: .any)["Added: import { login } from './auth'"])
    }

    func testReasoningIsFoldedAndOpensOnTap() {
        startDaemon(sessions: [DroiTestCase.session("Login fix", in: "/Users/dev/acme-web", messages: Self.toolTurn())])
        launchToList()
        openSession("Login fix")
        let reasoning = see(button("Thought for 1s"))
        XCTAssertEqual(reasoning.value as? String, "Collapsed")
        XCTAssertFalse(el(containing: "I should read the file").exists)
        reasoning.tap()
        see(el("I should read the file before changing it."))
    }

    func testOpeningAndClosingAFoldKeepsItsHeaderWhereItWasTapped() {
        let chatter = (0..<6).flatMap { i in [DroiTestCase.message("user", "Question \(i)"), DroiTestCase.message("assistant", String(repeating: "Answer \(i). ", count: 30))] }
        startDaemon(sessions: [DroiTestCase.session("A long chat, then the fix", in: "/Users/dev/acme-web", messages: chatter + Self.toolTurn())])
        launchToList()
        openSession("A long chat, then the fix")
        let create = see(button("Create: src/auth.test.ts"))
        Thread.sleep(forTimeInterval: 0.5)
        let before = create.frame.minY

        create.tap()
        see(el("Create details"))
        XCTAssertEqual(create.frame.minY, before, accuracy: 2)
        create.tap()
        gone(el("Create details"))
        XCTAssertEqual(create.frame.minY, before, accuracy: 2)
    }

    func testAnMCPToolShowsItsServerApartFromItsNameAndASkillLoadIsNamed() {
        let work = DroiTestCase.session("Remember how we test", in: "/Users/dev/acme-web", messages: [
            DroiTestCase.message("user", "Remember how we test"),
            DroiTestCase.message("assistant", content: [
                ["type": "tool_use", "id": "skill", "name": "Skill", "input": ["skill": "grilling"]],
                ["type": "tool_use", "id": "list", "name": "droi-memory___memory_list", "input": ["scope": "project", "category": "insight"]],
            ]),
            DroiTestCase.message("tool", content: [
                ["type": "tool_result", "toolUseId": "skill", "content": "Loaded skill grilling"],
                ["type": "tool_result", "toolUseId": "list", "content": "No entries in project Memory."],
            ]),
            DroiTestCase.message("assistant", "Noted."),
        ])
        let link = DroiTestCase.session("Links", in: "/Users/dev/acme-web", [
            ("user", "see https://example.com/" + String(repeating: "a-very-long-path-segment/", count: 12) + "index.html"),
        ])
        startDaemon(sessions: [work, link])
        launchToList()
        openSession("Remember how we test")
        let list = see(button(containing: "memory_list"))
        XCTAssertFalse(list.label.contains("___"), list.label)
        XCTAssertTrue(list.label.contains("scope: project \u{00B7} category: insight"), list.label)
        see(button("Skill: grilling"))

        back()
        openSession("Links")
        let bubble = see(el(containing: "example.com"))
        XCTAssertLessThanOrEqual(bubble.frame.maxX, app.frame.maxX)
    }
}
