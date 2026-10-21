import XCTest

/// What every UI test needs: a Fake Daemon of its own, the app launched on a
/// phone that was never paired, and finders by accessibility label.
class DroiTestCase: XCTestCase {
    var app: XCUIApplication!
    var daemon: FakeDaemon!

    override func setUp() {
        continueAfterFailure = false
    }

    // MARK: Scenario

    /// A Session of the Fake Daemon's scenario.
    static func session(_ title: String, in cwd: String = "/Users/test/project", _ messages: [(role: String, text: String)] = [], extra: [String: Any] = [:]) -> [String: Any] {
        var session: [String: Any] = [
            "title": title, "cwd": cwd,
            "messages": messages.map { ["role": $0.role, "text": $0.text] },
        ]
        if !extra.isEmpty { session["extra"] = extra }
        return session
    }

    /// Starts a Fake Daemon (stopped when the test ends) and makes it `daemon`.
    @discardableResult
    func startDaemon(
        sessions: [[String: Any]] = [DroiTestCase.session("Fix the login bug", [("user", "Why does login fail?"), ("assistant", "The SameSite cookie is dropped.")])],
        turn: [String: Any]? = nil,
        validDirectories: [String]? = nil,
        input: [String: Any]? = nil
    ) -> FakeDaemon {
        var scenario: [String: Any] = ["sessions": sessions]
        if let turn { scenario["turn"] = turn }
        if let validDirectories { scenario["validDirectories"] = validDirectories }
        if let input { scenario["input"] = input }
        let started = FakeDaemonHost.start(scenario)
        daemon = started
        addTeardownBlock { started.stop() }
        return started
    }

    // MARK: Launching

    /// Launches the app on a clean phone, paired with the Fake Daemon through its pairing link.
    @discardableResult
    func launch(pairedWith daemon: FakeDaemon? = nil, arguments: [String] = []) -> XCUIApplication {
        let app = XCUIApplication()
        app.launchArguments = ["-DroiResetState", "YES"] + arguments
        if let daemon { app.launchArguments += ["-DroiPairingLink", daemon.link] }
        app.launch()
        self.app = app
        return app
    }

    /// Quits the app and opens it again with what it kept; a phone launches at the app's root.
    @discardableResult
    func relaunch(arguments: [String] = []) -> XCUIApplication {
        app.terminate()
        app.launchArguments = arguments
        app.launch()
        return app
    }

    /// Launches paired and waits for the Session list.
    @discardableResult
    func launchToList(arguments: [String] = []) -> XCUIApplication {
        launch(pairedWith: daemon, arguments: arguments)
        XCTAssertTrue(sessionList.waitForExistence(timeout: 20), "the Session list never showed")
        return app
    }

    // MARK: Finders

    /// Whatever has this accessibility label or identifier.
    func el(_ label: String) -> XCUIElement {
        app.descendants(matching: .any)[label]
    }

    /// Whatever has a label containing the text.
    func el(containing text: String) -> XCUIElement {
        app.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", text)).firstMatch
    }

    func button(_ label: String) -> XCUIElement { app.buttons[label] }

    func button(containing text: String) -> XCUIElement {
        app.buttons.matching(NSPredicate(format: "label CONTAINS %@", text)).firstMatch
    }

    var sessionList: XCUIElement { app.descendants(matching: .any)["Sessions"] }

    var messageField: XCUIElement { app.descendants(matching: .any)["Message"] }

    /// Waits for the element and fails the test with its description when it does not show.
    @discardableResult
    func see(_ element: XCUIElement, timeout: TimeInterval = 10, _ message: String = "", file: StaticString = #filePath, line: UInt = #line) -> XCUIElement {
        XCTAssertTrue(element.waitForExistence(timeout: timeout), "not on screen: \(message.isEmpty ? element.description : message)", file: file, line: line)
        return element
    }

    /// Waits until the element is gone.
    func gone(_ element: XCUIElement, timeout: TimeInterval = 10, _ message: String = "", file: StaticString = #filePath, line: UInt = #line) {
        let gone = XCTNSPredicateExpectation(predicate: NSPredicate(format: "exists == false"), object: element)
        XCTAssertEqual(XCTWaiter().wait(for: [gone], timeout: timeout), .completed, "still on screen: \(message.isEmpty ? element.description : message)", file: file, line: line)
    }

    /// Waits until the closure holds.
    func eventually(_ timeout: TimeInterval = 10, _ message: String = "", file: StaticString = #filePath, line: UInt = #line, _ condition: () -> Bool) {
        let deadline = Date().addingTimeInterval(timeout)
        while Date() < deadline {
            if condition() { return }
            Thread.sleep(forTimeInterval: 0.1)
        }
        XCTAssertTrue(condition(), message, file: file, line: line)
    }

    @discardableResult
    func tap(_ label: String, timeout: TimeInterval = 10) -> XCUIElement {
        let element = see(button(label), timeout: timeout)
        element.tap()
        return element
    }

    /// Opens a Session from the list by its title.
    func openSession(_ title: String) {
        tap(title, timeout: 15)
        see(messageField, timeout: 15, "the composer of \(title)")
    }

    /// Goes back one screen.
    func back() {
        app.navigationBars.buttons.element(boundBy: 0).tap()
    }

    /// Types into the composer and sends.
    func send(_ text: String) {
        see(messageField).tap()
        messageField.typeText(text)
        tap("Send")
    }

    func shot(_ name: String) {
        let attachment = XCTAttachment(screenshot: app.screenshot())
        attachment.name = name
        attachment.lifetime = .keepAlways
        add(attachment)
        if let dir = ProcessInfo.processInfo.environment["DROI_SHOTS"] {
            try? app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: "\(dir)/\(name).png"))
        }
    }
}
