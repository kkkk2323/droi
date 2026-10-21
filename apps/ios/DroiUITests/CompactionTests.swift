import XCTest

/// A `/compact`: the Fake Daemon answers it as a handoff, the summary starting a new Session.
final class CompactionTests: DroiTestCase {
    func testCompactHandsOffToANewSessionAndReportsWhatItFoldedAway() {
        startDaemon(sessions: [
            DroiTestCase.session("Long chat", in: "/Users/dev/acme-web", [
                ("user", "first question"), ("assistant", "first answer"),
                ("user", "second question"), ("assistant", "second answer"),
            ]),
        ])
        launchToList()
        openSession("Long chat")
        messageField.tap()
        messageField.typeText("/compact ")
        see(el("Command compact"))
        tap("Send")
        let compact = daemon.waitForRequest("daemon.compact_session")
        XCTAssertEqual(compact?["sessionId"] as? String, daemon.session("Long chat").id)
        see(el(containing: "Summary of the earlier conversation (4 messages)."), timeout: 15)
        see(el("Compacted, 4 messages summarised"))
        see(button("Send"))
        back()
        see(sessionList)
        // The compacted Session folds into the one that continues it.
        XCTAssertEqual(app.buttons.matching(NSPredicate(format: "label CONTAINS 'Long chat'")).count, 1)
        XCTAssertFalse(el("Compacting").exists)
        openSession("Long chat")
        see(button(containing: "Continued from \u{201C}Long chat\u{201D}"))
    }
}
