import XCTest

/// The connection state machine as the reader sees it: a reset token, a computer
/// that is away, another Droi version, the app going to the background.
final class ConnectionTests: DroiTestCase {
    private let deploy = DroiTestCase.session("Deploy", in: "/Users/dev/acme-web", [("user", "ship it")])

    func testAResetPairingTokenSaysTheComputerIsNoLongerPairedAndPairingAgainRecovers() {
        startDaemon(sessions: [deploy])
        launchToList()
        let link = daemon.resetToken()
        relaunch()
        see(app.staticTexts["Test Mac is no longer paired"], timeout: 20)

        let field = see(app.textFields["Pairing link"])
        field.tap()
        field.typeText(link + "\n")
        see(sessionList, timeout: 20)
        XCTAssertEqual(see(el("Connection")).value as? String, "Connected")
        tap("Settings")
        see(button(containing: "Paired computers")).tap()
        XCTAssertEqual(app.buttons.matching(NSPredicate(format: "label CONTAINS 'Test Mac'")).count, 1)
    }

    func testAnUnreachableComputerShowsItsAddressAndWhatToCheckThenConnectsWhenBack() {
        startDaemon(sessions: [deploy])
        launchToList()
        daemon.command("goDown")
        relaunch()
        see(app.staticTexts["Can't reach Test Mac"], timeout: 20)
        see(app.staticTexts[daemon.url])
        let checks = see(el("What to check"))
        XCTAssertTrue(checks.staticTexts.matching(NSPredicate(format: "label CONTAINS 'Remote Access'")).firstMatch.exists)
        XCTAssertEqual(checks.staticTexts.matching(NSPredicate(format: "label BEGINSWITH[c] 'The' OR label BEGINSWITH 'Remote'")).count, 3)

        daemon.command("comeBack")
        see(button("Deploy"), timeout: 20)
        gone(app.staticTexts["Can't reach Test Mac"])
    }

    func testAnotherDroiVersionOnTheComputerShowsANoticeThatCanBeDismissed() {
        startDaemon(sessions: [deploy], appVersion: "0.9.0")
        launchToList()
        let notice = see(el(containing: "runs Droi 0.9.0"))
        XCTAssertTrue(notice.exists)
        tap("Dismiss")
        gone(el(containing: "runs Droi 0.9.0"))
        see(button("Deploy"))

        relaunch()
        see(sessionList, timeout: 20)
        see(button("Deploy"))
        sleep(2)
        XCTAssertFalse(el(containing: "runs Droi").exists)
    }

    func testComingBackToTheForegroundRechecksThePairing() {
        startDaemon(sessions: [deploy])
        launchToList()
        openSession("Deploy")
        XCUIDevice.shared.press(.home)
        _ = daemon.resetToken()
        app.activate()
        see(app.staticTexts["Test Mac is no longer paired"], timeout: 20)
    }

    func testTheBackgroundLetsTheSocketGoAndTheForegroundReconnectsToTheSameScreen() {
        startDaemon(sessions: [deploy], turn: ["kind": "reply", "deltas": ["All ", "green."], "delayMs": 100])
        launchToList()
        openSession("Deploy")
        XCTAssertNotNil(daemon.waitForRequest("daemon.load_session"))
        see(messageField).tap()
        messageField.typeText("half a thought")

        XCUIDevice.shared.press(.home)
        sleep(2)
        app.activate()
        XCTAssertNotNil(daemon.waitForRequest("daemon.load_session", nth: 2))
        gone(el("Reconnecting"), timeout: 15)
        see(app.navigationBars["Deploy"])
        see(el(containing: "ship it"))
        XCTAssertEqual(messageField.value as? String, "half a thought")
        // Notifications flow again on the new socket.
        messageField.tap()
        messageField.typeText(" more")
        tap("Send")
        see(el(containing: "All green."), timeout: 15)
        back()
        XCTAssertEqual(see(el("Connection")).value as? String, "Connected")
    }

    func testADroppedSocketReconnectsWithABannerAndKeepsTheOpenSession() {
        startDaemon(sessions: [deploy])
        launchToList()
        openSession("Deploy")
        XCTAssertNotNil(daemon.waitForRequest("daemon.load_session"))
        daemon.command("goDown")
        see(el("Reconnecting"), timeout: 15)
        see(app.navigationBars["Deploy"])
        daemon.command("comeBack")
        gone(el("Reconnecting"), timeout: 30)
        see(app.navigationBars["Deploy"])
        XCTAssertNotNil(daemon.waitForRequest("daemon.load_session", nth: 2, timeout: 15))
    }
}
