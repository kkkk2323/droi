import XCTest

/// First launch, pasting a pairing link, the Session list's first view of a computer.
final class PairingTests: DroiTestCase {
    func testFirstLaunchShowsPairingAndSettingsShowTheVersion() {
        launch()
        see(app.staticTexts["Pair with a computer"])
        tap("Settings")
        see(button(containing: "Paired computers"))
        let version = ProcessInfo.processInfo.environment["DROI_APP_VERSION"] ?? ""
        see(el("Version"))
        see(app.staticTexts[version], timeout: 5, "the app's version \(version)")
        // The simulator's build has no provisioning profile.
        see(app.staticTexts["Not signed for a device"])
    }

    func testPastingALinkConnectsAndListsSessionsGroupedByWorkspace() {
        startDaemon(sessions: [
            Self.session("Deploy", in: "/Users/dev/acme-web", [("user", "ship it")]),
            Self.session("Notes", in: "/Users/dev/acme-web", [("user", "todo")]),
            Self.session("Docs", in: "/Users/dev/handbook", [("user", "write")]),
        ])
        launch()
        pasteLink(daemon.link)
        see(sessionList, timeout: 20)
        see(button("Switch computer, Test Mac"))
        XCTAssertEqual(see(el("Connection")).value as? String, "Connected")
        let web = see(app.otherElements["acme-web"])
        XCTAssertTrue(web.buttons["Deploy"].exists)
        XCTAssertTrue(web.buttons["Notes"].exists)
        XCTAssertTrue(app.otherElements["handbook"].buttons["Docs"].exists)
    }

    func testALinkWithoutTheAddressOrAnUnreachableComputerSaysSo() {
        launch()
        pasteLink("abc-123")
        see(el(containing: "whole pairing link"))
        clearLink()
        pasteLink("http://127.0.0.1:9/#pair=abc")
        see(el(containing: "Cannot reach http://127.0.0.1:9"), timeout: 20)
        see(app.staticTexts["Pair with a computer"])
    }

    func testAWorkingSessionAndOneWaitingOnAPromptShowDifferentStates() {
        startDaemon(sessions: [
            Self.session("Deploy", in: "/Users/dev/acme-web", [("user", "ship it")]),
            Self.session("Notes", in: "/Users/dev/acme-web", [("user", "todo")]),
        ])
        launchToList()
        // Opening a Session subscribes the phone to its notifications.
        openSession("Deploy")
        back()
        openSession("Notes")
        back()
        daemon.notify(daemon.session("Deploy"), ["type": "droid_working_state_changed", "newState": "streaming_assistant_message"])
        daemon.notify(daemon.session("Notes"), ["type": "droid_working_state_changed", "newState": "waiting_for_tool_confirmation"])
        let deploy = see(button(containing: "Deploy"))
        let notes = see(button(containing: "Notes"))
        eventually { deploy.label.contains("Working") }
        eventually { notes.label.contains("Needs input") }
        XCTAssertFalse(deploy.label.contains("Needs input"))
    }

    func testTypingInTheSearchBoxAsksTheDaemonAndListsTheHitsWithTheirSnippets() {
        startDaemon(sessions: [
            Self.session("Deploy", in: "/Users/dev/acme-web", [("user", "ship it")]),
            Self.session("Notes", in: "/Users/dev/acme-web", [("user", "todo")]),
        ])
        launchToList()
        let search = see(app.searchFields["Search sessions"])
        search.tap()
        search.typeText("ship")
        let results = see(el("Search results"))
        let searched = daemon.waitForRequest("daemon.search_sessions")
        XCTAssertEqual(searched?["query"] as? String, "ship")
        let hit = see(results.buttons.matching(NSPredicate(format: "label CONTAINS 'Deploy'")).firstMatch)
        XCTAssertTrue(hit.label.contains("ship it"), hit.label)
        XCTAssertFalse(app.otherElements["acme-web"].exists)
        hit.tap()
        see(messageField, timeout: 15)
        see(app.navigationBars["Deploy"])
    }

    func testLoadOlderSessionsBringsTheRest() {
        startDaemon(sessions: (1...105).map {
            Self.session("Task \(String(format: "%03d", $0))", in: "/Users/dev/acme-web", [("user", "go")])
        })
        launchToList()
        see(button("Task 105"))
        XCTAssertFalse(button("Task 001").exists)
        scrollTo(button("Load older sessions")).tap()
        XCTAssertNotNil(daemon.waitForRequest("daemon.list_available_sessions", nth: 2))
        gone(button("Load older sessions"))
    }

    // MARK: Helpers

    private func pasteLink(_ link: String) {
        let field = see(app.textFields["Pairing link"])
        field.tap()
        field.typeText(link + "\n")
    }

    private func clearLink() {
        let field = app.textFields["Pairing link"]
        field.tap()
        field.press(forDuration: 1)
        if app.menuItems["Select All"].waitForExistence(timeout: 2) { app.menuItems["Select All"].tap() }
        field.typeText(XCUIKeyboardKey.delete.rawValue)
    }
}
