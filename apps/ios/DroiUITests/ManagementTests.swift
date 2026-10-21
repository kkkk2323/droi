import XCTest

/// What a long press on a Session or a Workspace offers, and what the phone keeps itself.
final class ManagementTests: DroiTestCase {
    private let sessions = [
        DroiTestCase.session("Deploy", in: "/Users/dev/acme-web", [("user", "ship it")]),
        DroiTestCase.session("Notes", in: "/Users/dev/acme-web", [("user", "todo")]),
        DroiTestCase.session("Budget", in: "/Users/dev/finance", [("user", "sum")]),
    ]

    func testArchiveUnarchiveAndRenameGoToTheDaemon() {
        startDaemon(sessions: sessions)
        launchToList()
        press(button("Deploy"), choose: "Archive")
        XCTAssertNotNil(daemon.waitForRequest("daemon.archive_session"))
        gone(button("Deploy"))

        tap("Settings")
        let archived = see(app.switches["Show archived sessions"])
        archived.switches.firstMatch.tap()
        back()
        let row = see(button(containing: "Deploy"))
        eventually { row.label.contains("Archived") }
        press(row, choose: "Unarchive")
        XCTAssertNotNil(daemon.waitForRequest("daemon.unarchive_session"))
        eventually { !self.button(containing: "Deploy").label.contains("Archived") }

        press(button("Notes"), choose: "Rename")
        let alert = see(app.alerts["Rename session"])
        let field = alert.textFields["Session title"]
        field.tap()
        field.typeText(String(repeating: XCUIKeyboardKey.delete.rawValue, count: 5) + "Release notes")
        alert.buttons["Save"].tap()
        let renamed = daemon.waitForRequest("daemon.rename_session")
        XCTAssertEqual(renamed?["sessionId"] as? String, daemon.session("Notes").id)
        XCTAssertEqual(renamed?["title"] as? String, "Release notes")
        see(button("Release notes"))
    }

    func testPinsAndFoldsAreKeptOnThePhoneAcrossARelaunch() {
        startDaemon(sessions: sessions)
        launchToList()
        press(button("Deploy"), choose: "Pin")
        eventually { self.button(containing: "Deploy").label.contains("Pinned") }

        press(button("finance"), choose: "Pin workspace")
        let finance = see(app.otherElements["finance"])
        eventually { finance.frame.minY < self.app.otherElements["acme-web"].frame.minY }

        button("acme-web").tap()
        eventually { self.button("acme-web").value as? String == "Collapsed" }
        gone(button(containing: "Deploy"))

        relaunch()
        see(sessionList, timeout: 20)
        eventually { self.app.otherElements["finance"].frame.minY < self.app.otherElements["acme-web"].frame.minY }
        XCTAssertEqual(see(button("acme-web")).value as? String, "Collapsed")
        button("acme-web").tap()
        eventually { self.button(containing: "Deploy").label.contains("Pinned") }
        XCTAssertTrue(daemon.requests("daemon.update_session_settings").isEmpty)
    }

    func testASessionRowIsAFullSizeTouchTarget() {
        startDaemon(sessions: sessions)
        launchToList()
        XCTAssertGreaterThanOrEqual(see(button("Deploy")).frame.height, 44)
    }
}
