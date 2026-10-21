import XCTest

/// What every UI test needs: a Fake Daemon of its own, the app launched on a
/// phone that was never paired, and finders by accessibility label.
@MainActor
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
        input: [String: Any]? = nil,
        name: String? = nil,
        appVersion: String? = nil
    ) -> FakeDaemon {
        var scenario: [String: Any] = ["sessions": sessions]
        if let turn { scenario["turn"] = turn }
        if let validDirectories { scenario["validDirectories"] = validDirectories }
        if let input { scenario["input"] = input }
        let started = FakeDaemonHost.start(scenario, name: name, appVersion: appVersion)
        if daemon == nil { daemon = started }
        addTeardownBlock { started.stop() }
        return started
    }

    /// A Session whose messages are given as the Daemon stores them (content blocks, tool calls and results).
    static func session(_ title: String, in cwd: String = "/Users/test/project", messages: [[String: Any]], extra: [String: Any] = [:]) -> [String: Any] {
        var session: [String: Any] = ["title": title, "cwd": cwd, "messages": messages]
        if !extra.isEmpty { session["extra"] = extra }
        return session
    }

    static func message(_ role: String, _ text: String) -> [String: Any] { ["role": role, "text": text] }

    static func message(_ role: String, content: [[String: Any]]) -> [String: Any] { ["role": role, "content": content] }

    static func toolCall(_ id: String, _ name: String, _ input: [String: Any]) -> [String: Any] {
        message("assistant", content: [["type": "tool_use", "id": id, "name": name, "input": input]])
    }

    static func toolResult(_ id: String, _ content: Any, isError: Bool = false) -> [String: Any] {
        var block: [String: Any] = ["type": "tool_result", "toolUseId": id, "content": content]
        if isError { block["isError"] = true }
        return message("tool", content: [block])
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
        app.descendants(matching: .any).matching(identifier: label).firstMatch
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
        let found = element.waitForExistence(timeout: timeout)
        if !found { dumpTree() }
        XCTAssertTrue(found, "not on screen: \(message.isEmpty ? element.description : message)", file: file, line: line)
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

    /// Swipes the screen up until the element is on it (a lazy list builds its rows as they scroll in).
    @discardableResult
    func scrollTo(_ element: XCUIElement, swipes: Int = 12) -> XCUIElement {
        var left = swipes
        while !element.exists || !element.isHittable, left > 0 {
            app.swipeUp()
            left -= 1
        }
        return element
    }

    /// Opens a Session from the list by its title.
    func openSession(_ title: String) {
        tap(title, timeout: 15)
        see(messageField, timeout: 15, "the composer of \(title)")
    }

    /// A long press opens a row's actions; choose one.
    func press(_ element: XCUIElement, choose action: String) {
        see(element).press(forDuration: 1.0)
        see(app.buttons[action], timeout: 5, "the action \(action)").tap()
    }

    /// Drags the sheet with this navigation bar title down off the screen.
    func dismissSheet(titled title: String) {
        let grabber = see(app.navigationBars[title]).coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.5))
        grabber.press(forDuration: 0.05, thenDragTo: app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.99)))
        gone(app.navigationBars[title])
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

    /// What is on screen, in the test log, for a test that could not find its element.
    func dumpTree() {
        print("---- screen ----\n\(String(app.debugDescription.prefix(12000)))\n---- end ----")
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
