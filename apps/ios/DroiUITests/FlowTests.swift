import XCTest

/// The main flows: the list, opening a Session, sending, and the reply streaming in.
final class FlowTests: DroiTestCase {
    private let reply = ["Paragraph 1 of the plan.\n\n", "Paragraph 2 of the plan.\n\n", "The loop ends it."]

    func testListShowsSessionsByWorkspace() {
        startDaemon(sessions: [
            Self.session("Fix the login bug", in: "/Users/test/webapp"),
            Self.session("Landing page copy", in: "/Users/test/site"),
        ])
        launchToList()
        see(button("Fix the login bug"), timeout: 15)
        XCTAssertTrue(button("Landing page copy").exists)
        let status = see(el("Connection"))
        sleep(6)
        // Still connected after the handshake's timeout has passed.
        XCTAssertEqual(status.value as? String, "Connected")
    }

    func testOpenSendAndStream() {
        startDaemon(turn: ["kind": "reply", "deltas": reply, "delayMs": 300])
        launchToList()
        openSession("Fix the login bug")
        see(el(containing: "SameSite"))
        send("Explain the plan")
        see(el(containing: "Paragraph 1"), timeout: 15)
        see(el(containing: "ends it"), timeout: 30)
        let sent = daemon.requests("daemon.add_user_message")
        XCTAssertEqual(sent.count, 1)
    }
}
