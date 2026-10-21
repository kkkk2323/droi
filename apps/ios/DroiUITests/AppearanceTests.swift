import XCTest

/// The phone's Settings: theme, text size, archived sessions, and the alert switches. What the colours,
/// font sizes, sounds and haptics look, sound or feel like is not observable to a UI test; the choices are.
final class AppearanceTests: DroiTestCase {
    private let deploy = DroiTestCase.session("Deploy", in: "/Users/dev/acme-web", [("user", "ship it")])
    private let notes = DroiTestCase.session("Notes", in: "/Users/dev/acme-web", [("user", "todo")])

    private func isSelected(_ label: String) -> Bool { app.buttons[label].isSelected }

    func testThemeAndTextSizeAreChosenInSettingsAndKeptAcrossARelaunch() {
        startDaemon(sessions: [deploy])
        launchToList()
        tap("Settings")
        eventually { self.isSelected("System") }
        for theme in ["Light", "Dark", "Solarized Light+"] {
            tap(theme)
            eventually { self.isSelected(theme) }
            XCTAssertFalse(isSelected("System"))
        }
        eventually { self.isSelected("Default") }
        scrollTo(button("Largest")).tap()
        eventually { self.isSelected("Largest") }
        XCTAssertFalse(isSelected("Default"))

        relaunch()
        see(sessionList, timeout: 20)
        tap("Settings")
        eventually { self.isSelected("Solarized Light+") }
        scrollTo(button("Largest"))
        XCTAssertTrue(isSelected("Largest"))
        tap("System")
        eventually { self.isSelected("System") }
    }

    func testTheAlertSwitchesAreKeptAcrossARelaunch() {
        startDaemon(sessions: [deploy])
        launchToList()
        tap("Settings")
        let finished = scrollTo(app.switches["Sound when finished"])
        let needsInput = scrollTo(app.switches["Haptic when it needs input"])
        XCTAssertEqual(finished.value as? String, "1")
        XCTAssertEqual(needsInput.value as? String, "1")
        finished.tap()
        needsInput.tap()
        eventually { (finished.value as? String) == "0" && (needsInput.value as? String) == "0" }

        relaunch()
        see(sessionList, timeout: 20)
        tap("Settings")
        XCTAssertEqual(scrollTo(app.switches["Sound when finished"]).value as? String, "0")
        XCTAssertEqual(scrollTo(app.switches["Haptic when it needs input"]).value as? String, "0")
        XCTAssertEqual(scrollTo(app.switches["Haptic when finished"]).value as? String, "1")
    }

    func testASessionThatFinishesOffScreenIsUnreadUntilOpened() {
        startDaemon(sessions: [deploy, notes])
        launchToList()
        openSession("Deploy")
        back()
        openSession("Notes")
        // Notes is on screen: it stays read. Deploy finishes in the background and becomes unread.
        daemon.notify(daemon.session("Notes"), ["type": "droid_working_state_changed", "newState": "streaming_assistant_message"])
        daemon.notify(daemon.session("Notes"), ["type": "droid_working_state_changed", "newState": "idle"])
        daemon.notify(daemon.session("Deploy"), ["type": "droid_working_state_changed", "newState": "streaming_assistant_message"])
        daemon.notify(daemon.session("Deploy"), ["type": "droid_working_state_changed", "newState": "idle"])
        back()
        eventually { self.button(containing: "Deploy").label.contains("Unread") }
        XCTAssertFalse(button(containing: "Notes").label.contains("Unread"))

        openSession("Deploy")
        back()
        eventually { !self.button(containing: "Deploy").label.contains("Unread") }
    }
}
