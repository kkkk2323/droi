import XCTest

/// Several Paired Computers: adding, switching, renaming, removing, and what a relaunch reopens.
final class ComputersTests: DroiTestCase {
    private let deploy = DroiTestCase.session("Deploy", in: "/Users/dev/acme-web", [("user", "ship it")])
    private let budget = DroiTestCase.session("Budget", in: "/Users/dev/finance", [("user", "sum it")])

    func testPairingTheSameComputerAgainUpdatesItInsteadOfAddingOne() {
        startDaemon(sessions: [deploy])
        launchToList()
        let moved = daemon.link.replacingOccurrences(of: "127.0.0.1", with: "localhost")
        addComputer(moved)
        see(sessionList, timeout: 20)
        XCTAssertEqual(see(el("Connection")).value as? String, "Connected")
        openComputers()
        let rows = app.buttons.matching(NSPredicate(format: "label CONTAINS 'Test Mac'"))
        see(rows.firstMatch)
        XCTAssertEqual(rows.count, 1)
        XCTAssertTrue(rows.firstMatch.label.contains("localhost:"), rows.firstMatch.label)
    }

    func testTwoComputersSwitchAndTheOtherShowsItsLastKnownListFirst() {
        startDaemon(sessions: [deploy])
        let office = startDaemon(sessions: [budget], name: "Office Mac")
        launchToList()
        addComputer(office.link)
        see(button("Budget"), timeout: 20)
        see(button("Switch computer, Office Mac"))

        switchTo("Test Mac")
        see(button("Deploy"), timeout: 20)
        XCTAssertFalse(button("Budget").exists)

        office.command("goDown")
        switchTo("Office Mac")
        see(button("Budget"), timeout: 20)
        eventually(20) { (self.el("Connection").value as? String) != "Connected" }
    }

    func testAPairedComputerCanBeRenamedReAddressedAndRemoved() {
        startDaemon(sessions: [deploy])
        launchToList()
        openComputers()
        see(button(containing: "Test Mac")).tap()
        let name = see(app.textFields["Name"])
        name.tap()
        replaceText(of: name, with: "Desk")
        let address = app.textFields["Address"]
        address.tap()
        replaceText(of: address, with: "not a url at all")
        tap("Save")
        see(el(containing: "Enter an address"))
        address.tap()
        let port = URL(string: daemon.url)!.port!
        replaceText(of: address, with: "localhost:\(port)")
        tap("Save")
        let row = see(button(containing: "Desk"))
        XCTAssertTrue(row.label.contains("localhost:\(port)"), row.label)

        row.tap()
        tap("Remove computer")
        tap("Remove")
        gone(button(containing: "Desk"))
        relaunch()
        see(app.staticTexts["Pair with a computer"])
    }

    func testRelaunchingReopensTheLastComputerAndSession() {
        startDaemon(sessions: [deploy])
        let office = startDaemon(sessions: [budget], name: "Office Mac")
        launchToList()
        addComputer(office.link)
        see(button("Budget"), timeout: 20)
        switchTo("Test Mac")
        see(button("Deploy"), timeout: 20)
        openSession("Deploy")

        relaunch()
        see(app.navigationBars["Deploy"], timeout: 20)
        back()
        see(button("Switch computer, Test Mac"))
    }

    // MARK: Helpers

    private func addComputer(_ link: String) {
        tap("Switch computer, Test Mac")
        tap("Add a computer")
        let field = see(app.textFields["Pairing link"])
        field.tap()
        field.typeText(link + "\n")
    }

    private func switchTo(_ name: String) {
        let title = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Switch computer'")).firstMatch
        see(title).tap()
        tap(name)
        see(sessionList)
    }

    private func openComputers() {
        tap("Settings")
        see(button(containing: "Paired computers")).tap()
    }

    private func replaceText(of field: XCUIElement, with text: String) {
        field.press(forDuration: 1)
        if app.menuItems["Select All"].waitForExistence(timeout: 2) { app.menuItems["Select All"].tap() }
        field.typeText(text)
    }
}
