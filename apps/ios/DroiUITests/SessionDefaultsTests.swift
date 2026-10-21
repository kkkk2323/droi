import XCTest

/// Settings > Session defaults: what new Sessions on the selected computer start with.
final class SessionDefaultsTests: DroiTestCase {
    private let deploy = DroiTestCase.session("Deploy", in: "/Users/dev/acme-web", [("user", "ship it")])

    private func saved() -> [String: Any]? { daemon.requests("daemon.update_session_defaults").last }

    private func openDefaults() {
        tap("Settings")
        see(button(containing: "Session defaults")).tap()
        see(app.navigationBars.firstMatch)
        see(el("Default model"))
    }

    /// Opens a row's menu and picks one of its choices.
    private func choose(_ row: String, _ option: String) {
        let picker = scrollTo(el(row))
        picker.tap()
        see(app.buttons[option], timeout: 5, "\(option) in the menu of \(row)").tap()
        eventually { (self.el(row).value as? String) == option }
    }

    func testTheSelectedComputersDefaultsChangeOnItsDaemon() {
        startDaemon(sessions: [deploy])
        launchToList()
        openDefaults()
        XCTAssertEqual(el("Default model").value as? String, "Auto Model")

        choose("Default model", "GPT-5")
        eventually { (self.saved()?["modelId"] as? String) == "gpt-5" }
        XCTAssertEqual(saved()?["reasoningEffort"] as? String, "low")
        eventually { (self.el("Reasoning level").value as? String) == "Low" }

        choose("Interaction mode", "Spec")
        eventually { (self.saved()?["interactionMode"] as? String) == "spec" }
        XCTAssertEqual(saved()?.count, 1)

        choose("Spec folder", "Project")
        eventually { (self.saved()?["specSaveDir"] as? String) == ".factory/docs" }

        choose("Token limit", "400K")
        eventually { (self.saved()?["compactionTokenLimit"] as? Double) == 400_000 }
        let compact = scrollTo(app.switches["Compact automatically"])
        compact.tap()
        eventually { (self.saved()?["compactionThresholdCheckEnabled"] as? Bool) == false }

        choose("Heavy task model", "Claude Opus 4.1")
        eventually { ((self.saved()?["subagentModelSettings"] as? [String: Any])?["heavyModel"] as? String) == "claude-opus-4-1" }
        choose("Heavy task reasoning", "High")
        eventually {
            ((self.saved()?["subagentModelSettings"] as? [String: Any])?["heavyReasoningEffort"] as? String) == "high"
        }

        // Out and in again reads them from the Daemon.
        back()
        see(button(containing: "Session defaults")).tap()
        XCTAssertEqual(see(el("Default model")).value as? String, "GPT-5")
        XCTAssertEqual(scrollTo(el("Heavy task reasoning")).value as? String, "High")
    }

    func testDefaultsTheOrganizationManagesShowTheirValueButOpenNoMenu() {
        startDaemon(
            sessions: [deploy],
            input: ["defaults": ["management": ["modelId": ["disabled": true, "source": "org"]]]])
        launchToList()
        openDefaults()
        XCTAssertEqual(el("Default model").value as? String, "Auto Model")
        XCTAssertFalse(app.buttons["Default model"].exists)
        XCTAssertTrue(see(app.buttons["Reasoning level"]).exists)
    }

    func testTheToolCallsDefaultIsKeptOnThePhoneAndGoesIntoNewSessions() {
        startDaemon(sessions: [deploy])
        launchToList()
        openDefaults()
        XCTAssertEqual(scrollTo(el("Mode")).value as? String, "Follow droid settings")
        choose("Mode", "Script")
        // It is not a Daemon setting: nothing is sent for it.
        XCTAssertNil(saved())

        back()
        back()
        tap("New session")
        see(button("Workspace"))
        XCTAssertEqual(see(button("Tool calls")).value as? String, "Script")
        let draft = daemon.waitForRequest("daemon.initialize_session")
        XCTAssertEqual(draft?["toolExecutionMode"] as? String, "script_only")
    }
}
