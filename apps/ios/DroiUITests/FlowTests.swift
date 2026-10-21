import XCTest

/// The main flows against a running Fake Daemon. `scripts/test-ios.sh`
/// starts it and passes its pairing link as DROI_PAIRING_LINK.
final class FlowTests: XCTestCase {
    private var link: String {
        ProcessInfo.processInfo.environment["DROI_PAIRING_LINK"] ?? ""
    }

    override func setUp() {
        continueAfterFailure = false
    }

    private func launch() -> XCUIApplication {
        XCTAssertFalse(link.isEmpty, "DROI_PAIRING_LINK is not set")
        let app = XCUIApplication()
        app.launchArguments = ["-DroiPairingLink", link]
        app.launch()
        return app
    }

    private func shot(_ app: XCUIApplication, _ name: String) {
        let attachment = XCTAttachment(screenshot: app.screenshot())
        attachment.name = name
        attachment.lifetime = .keepAlways
        add(attachment)
        if let dir = ProcessInfo.processInfo.environment["DROI_SHOTS"] {
            try? app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: "\(dir)/\(name).png"))
        }
    }

    private func text(_ app: XCUIApplication, containing value: String) -> XCUIElement {
        app.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", value)).firstMatch
    }

    func testListShowsSessionsByWorkspace() {
        let app = launch()
        XCTAssertTrue(app.buttons["Fix the login bug"].waitForExistence(timeout: 15))
        XCTAssertTrue(app.buttons["Landing page copy"].exists)
        let status = app.descendants(matching: .any)["Connection"]
        XCTAssertTrue(status.waitForExistence(timeout: 5))
        sleep(6)
        // Still connected after the handshake's timeout has passed.
        XCTAssertEqual(status.value as? String, "Connected")
        shot(app, "list")
    }

    func testOpenSendAndStream() {
        let app = launch()
        let row = app.buttons["Fix the login bug"]
        XCTAssertTrue(row.waitForExistence(timeout: 15))
        row.tap()
        XCTAssertTrue(text(app, containing: "SameSite").waitForExistence(timeout: 10))
        shot(app, "session")
        let field = app.descendants(matching: .any)["Message"]
        XCTAssertTrue(field.waitForExistence(timeout: 5))
        field.tap()
        field.typeText("Explain the plan")
        app.buttons["Send"].tap()
        XCTAssertTrue(text(app, containing: "Paragraph 1").waitForExistence(timeout: 15))
        shot(app, "streaming")
        XCTAssertTrue(text(app, containing: "ends it").waitForExistence(timeout: 30))
        sleep(1)
        shot(app, "streamed")
    }
}
