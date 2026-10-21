import XCTest

/// Sending, streaming, cancelling and queueing; the draft; the task list and the context meter.
final class ComposerTests: DroiTestCase {
    private let chat = DroiTestCase.session("Chat", in: "/Users/dev/acme-web", [("user", "hello"), ("assistant", "Hi! How can I help?")])
    private let notes = DroiTestCase.session("Notes", in: "/Users/dev/acme-web", [("user", "todo")])
    private let slowReply: [String: Any] = ["kind": "reply", "deltas": ["Part one. ", "Part two. ", "Part three."], "delayMs": 4000]

    func testSendsStreamsAndCancels() {
        startDaemon(sessions: [chat, notes], turn: slowReply)
        launchToList()
        openSession("Chat")
        XCTAssertFalse(button("Send").isEnabled)
        send("Tell me a story")
        let sent = daemon.waitForRequest("daemon.add_user_message")
        XCTAssertEqual(sent?["text"] as? String, "Tell me a story")
        XCTAssertNotEqual(messageField.value as? String, "Tell me a story")
        see(el(containing: "Tell me a story"))
        see(el(containing: "Part one."), timeout: 15)
        tap("Cancel")
        XCTAssertNotNil(daemon.waitForRequest("daemon.interrupt_session"))
        see(button("Send"))
        sleep(1)
        see(el(containing: "Part one."))
        XCTAssertFalse(el(containing: "Part three.").exists)
    }

    func testThreeQueuedMessagesFoldIntoOneRowThatOpensAndOneCanBeRemoved() {
        startDaemon(sessions: [chat, notes], turn: ["kind": "reply", "deltas": ["Working. "], "delayMs": 60000])
        launchToList()
        openSession("Chat")
        send("first")
        see(button("Cancel"))
        XCTAssertEqual(messageField.placeholderValue, "Queue a message")
        for text in ["one", "two", "three"] {
            messageField.tap()
            messageField.typeText(text)
            tap("Queue")
        }
        let fourth = daemon.waitForRequest("daemon.add_user_message", nth: 4)
        XCTAssertEqual(fourth?["text"] as? String, "three")
        XCTAssertEqual(fourth?["queuePlacement"] as? String, "end_of_loop")

        let fold = see(button("Queued messages, 3"))
        XCTAssertEqual(fold.value as? String, "Collapsed")
        XCTAssertFalse(button("Remove queued message").exists)
        fold.tap()
        let removals = app.buttons.matching(identifier: "Remove queued message")
        eventually { removals.count == 3 }
        removals.element(boundBy: 1).tap()
        let removed = daemon.waitForRequest("daemon.resolve_queued_user_message")
        XCTAssertEqual(removed?["action"] as? String, "delete")
        eventually { removals.count == 2 }
        XCTAssertFalse(el("Queued messages, 3").exists)
        XCTAssertFalse(el(containing: "two").exists)
    }

    func testCancellingKeepsAQueuedMessageHerePausedToPutBackInTheComposer() {
        startDaemon(sessions: [chat, notes], turn: ["kind": "reply", "deltas": ["Working. "], "delayMs": 60000])
        launchToList()
        openSession("Chat")
        send("first")
        see(button("Cancel"))
        messageField.tap()
        messageField.typeText("keep me")
        tap("Queue")
        XCTAssertNotNil(daemon.waitForRequest("daemon.add_user_message", nth: 2))
        let queued = see(el("Queued messages"))
        see(queued.staticTexts["keep me"])

        tap("Cancel")
        XCTAssertNotNil(daemon.waitForRequest("daemon.interrupt_session"))
        see(queued.staticTexts["Paused"])
        tap("Edit queued message")
        eventually { self.messageField.value as? String == "keep me" }
        gone(el("Queued messages"))
    }

    func testADraftSurvivesSwitchingSessionsAndARelaunch() {
        startDaemon(sessions: [chat, notes])
        launchToList()
        openSession("Chat")
        messageField.tap()
        messageField.typeText("half a thought")
        back()
        openSession("Notes")
        XCTAssertNotEqual(messageField.value as? String, "half a thought")
        back()
        openSession("Chat")
        XCTAssertEqual(messageField.value as? String, "half a thought")
        relaunch()
        see(messageField, timeout: 20)
        XCTAssertEqual(messageField.value as? String, "half a thought")
    }

    func testTheTaskListAndTheContextMeterFollowTheDaemonsUpdates() {
        startDaemon(
            sessions: [chat],
            turn: [
                "kind": "todo", "reply": "Working through it.",
                "todos": [
                    ["id": "1", "content": "Read the config", "status": "completed"],
                    ["id": "2", "content": "Write the migration", "status": "in_progress"],
                    ["id": "3", "content": "Run the tests", "status": "pending"],
                ],
            ],
            input: ["contextUsedTokens": 50_000])
        launchToList()
        openSession("Chat")
        let meter = see(el("Context used"))
        XCTAssertEqual(meter.value as? String, "25 percent")

        send("go")
        let summary = see(button("Tasks, 1 of 3 done"), timeout: 15)
        XCTAssertTrue(summary.staticTexts["Write the migration"].exists)
        summary.tap()
        see(el("Done: Read the config"))
        see(el("In progress: Write the migration"))
        see(el("Pending: Run the tests"))
        eventually { (self.el("Context used").value as? String) == "5 percent" }
    }

    func testTheComposerStillTakesAMessageFromASessionTheDaemonLetGo() {
        startDaemon(sessions: [chat], turn: ["kind": "reply", "deltas": ["Back again."]])
        launchToList()
        openSession("Chat")
        XCTAssertTrue(messageField.isEnabled)
        let listed = daemon.requests("daemon.list_available_sessions").count
        daemon.notify(daemon.session("Chat"), [
            "type": "session_inactivity", "message": "Session inactive",
            "timestamp": Date().timeIntervalSince1970 * 1000, "timeoutSeconds": 1800,
        ])
        daemon.notify(daemon.session("Chat"), ["type": "session_title_updated", "title": "Chat"])
        XCTAssertNotNil(daemon.waitForRequest("daemon.list_available_sessions", nth: listed + 1))

        send("still there?")
        XCTAssertNotNil(daemon.waitForRequest("daemon.add_user_message"))
        see(el(containing: "Back again."), timeout: 15)
        XCTAssertEqual(daemon.requests("daemon.load_session").count, 2)
    }
}
