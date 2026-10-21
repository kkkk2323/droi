import XCTest

/// Starting a Session: where, with which model, and the Draft Session behind the page.
final class NewSessionTests: DroiTestCase {
    private let older = DroiTestCase.session("Old work", in: "/Users/dev/billing-service", [("user", "a")])
    private let newer = DroiTestCase.session("Recent work", in: "/Users/dev/acme-web", [("user", "b")])
    private let reply: [String: Any] = ["kind": "reply", "deltas": ["On it."]]

    private func startOnTheNewestWorkspace(skills: [[String: Any]] = []) {
        startDaemon(
            sessions: [older, newer], turn: reply, validDirectories: ["/Users/dev/fresh-project"],
            input: ["skills": skills])
        launchToList()
    }

    private func openNewSession() {
        tap("New session")
        see(button("Workspace"))
    }

    func testStartsASessionInARecentWorkspaceWithTheChosenModel() {
        startOnTheNewestWorkspace()
        openNewSession()
        let workspace = button("Workspace")
        eventually { workspace.value as? String == "acme-web?" }
        let firstDraft = daemon.requests("daemon.initialize_session")
        XCTAssertEqual(firstDraft.count, 1)
        XCTAssertEqual(firstDraft.first?["cwd"] as? String, "/Users/dev/acme-web")
        XCTAssertEqual((firstDraft.first?["tags"] as? [[String: Any]])?.compactMap { $0["name"] as? String }, ["droi.draft"])

        workspace.tap()
        see(app.navigationBars["Workspace"])
        button(containing: "billing-service").tap()
        eventually { workspace.value as? String == "billing-service?" }
        let draft = daemon.waitForRequest("daemon.initialize_session", nth: 2)
        XCTAssertEqual(draft?["cwd"] as? String, "/Users/dev/billing-service")
        let closed = daemon.waitForRequest("daemon.close_session")
        let firstId = daemon.requests("daemon.close_session").first?["sessionId"] as? String
        XCTAssertNotNil(closed)
        XCTAssertNotNil(firstId)

        tap("Model")
        see(app.navigationBars["Choose a model"])
        button(containing: "GPT-5").tap()
        eventually { self.button("Model").value as? String == "GPT-5" }

        messageField.tap()
        messageField.typeText("Refactor the invoices")
        tap("Start session")
        let takeover = daemon.waitForRequest("daemon.update_session_settings")
        XCTAssertEqual(takeover?["modelId"] as? String, "gpt-5")
        XCTAssertEqual((takeover?["tags"] as? [Any])?.count, 0)
        let sent = daemon.waitForRequest("daemon.add_user_message")
        XCTAssertEqual(sent?["text"] as? String, "Refactor the invoices")
        XCTAssertEqual(sent?["sessionId"] as? String, takeover?["sessionId"] as? String)
        see(el(containing: "On it."), timeout: 15)
        XCTAssertEqual(daemon.requests("daemon.initialize_session").count, 2)
    }

    func testATypedPathIsCheckedByTheDaemonAndStartsASessionThere() {
        startOnTheNewestWorkspace()
        openNewSession()
        XCTAssertNotNil(daemon.waitForRequest("daemon.initialize_session"))
        button("Workspace").tap()
        see(app.navigationBars["Workspace"])
        button(containing: "Other folder").tap()
        see(el("Where should Droid work?"))
        let path = see(app.textFields["Workspace path"])
        path.tap()
        path.typeText("/nope/missing")
        tap("Start")
        see(el(containing: "Directory does not exist: /nope/missing"))

        path.clearAndType("/Users/dev/fresh-project")
        tap("Start")
        see(messageField, timeout: 15)
        // The Daemon lists a Session once it has its first message.
        send("Hello")
        see(el(containing: "On it."), timeout: 15)
        back()
        see(button("fresh-project"))
        XCTAssertNotNil(daemon.waitForRequest("daemon.close_session"))
    }

    func testSlashListsTheWorkspacesSkillsBeforeTheFirstSend() {
        startOnTheNewestWorkspace(skills: [["name": "handoff", "description": "Compact the conversation"]])
        openNewSession()
        XCTAssertNotNil(daemon.waitForRequest("daemon.list_skills"))
        messageField.tap()
        messageField.typeText("/")
        see(el("Commands and skills"))
        see(el(containing: "handoff"))
    }

    func testLeavingThePageClosesTheDraftWhichNeverShowsInTheList() {
        startOnTheNewestWorkspace()
        openNewSession()
        let draft = daemon.waitForRequest("daemon.initialize_session")
        back()
        see(sessionList)
        see(button("Recent work"))
        see(button("Old work"))
        XCTAssertEqual(daemon.waitForRequest("daemon.close_session")?["sessionId"] as? String, draft?["sessionId"] as? String)
        XCTAssertTrue(button("acme-web").exists)
        XCTAssertTrue(button("billing-service").exists)
        XCTAssertEqual(app.buttons.matching(NSPredicate(format: "label == 'acme-web' OR label == 'billing-service'")).count, 2)
    }

    func testScriptPickedForToolCallsStartsASessionThatCallsToolsThroughScript() {
        startOnTheNewestWorkspace()
        openNewSession()
        let draft = daemon.waitForRequest("daemon.initialize_session")
        XCTAssertNil(draft?["toolExecutionMode"])
        let tools = see(button("Tool calls"))
        XCTAssertEqual(tools.value as? String, "Direct")
        tools.tap()
        see(app.navigationBars["Tool calls"])
        XCTAssertTrue(button(containing: "Direct").exists)
        button(containing: "Script").tap()
        eventually { self.button("Tool calls").value as? String == "Script" }

        tap("Start session")
        // A Session's tool mode is fixed when it is created, so the draft gives way to a new one.
        let created = daemon.waitForRequest("daemon.initialize_session", nth: 2)
        XCTAssertEqual(created?["cwd"] as? String, "/Users/dev/acme-web")
        XCTAssertEqual(created?["toolExecutionMode"] as? String, "script_only")
        XCTAssertEqual(daemon.waitForRequest("daemon.close_session")?["sessionId"] as? String, draft?["sessionId"] as? String)
    }
}

extension XCUIElement {
    /// Replaces the text of a field.
    func clearAndType(_ text: String) {
        tap()
        if let current = value as? String, !current.isEmpty {
            typeText(String(repeating: XCUIKeyboardKey.delete.rawValue, count: current.count))
        }
        typeText(text)
    }
}
