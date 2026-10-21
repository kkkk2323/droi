import XCTest

/// The session settings pills, the model picker, and the phone's own Settings.
final class SettingsTests: DroiTestCase {
    private let first = DroiTestCase.session(
        "First session", in: "/Users/dev/acme-web", [("user", "hi")],
        extra: ["settings": ["modelId": "claude-opus-4-1", "reasoningEffort": "medium", "autonomyLevel": "low"]])

    private func lastSettings() -> [String: Any]? { daemon.requests("daemon.update_session_settings").last }

    private func start() {
        startDaemon(sessions: [first])
        launchToList()
        openSession("First session")
    }

    /// Opens a pill's radio sheet and picks one of its choices.
    private func choose(_ pill: String, _ option: String) {
        tap(pill)
        see(app.navigationBars[pill])
        see(button(containing: option)).tap()
        gone(app.navigationBars[pill])
    }

    func testModelReasoningEffortAndAutonomyChangeTheSessionOnTheDaemon() {
        start()
        XCTAssertEqual(see(button("Model")).value as? String, "Claude Opus 4.1")
        XCTAssertEqual(see(button("Reasoning effort")).value as? String, "Medium")
        XCTAssertEqual(see(button("Autonomy")).value as? String, "Low autonomy")

        tap("Model")
        see(app.navigationBars["Choose a model"])
        see(button(containing: "Claude Opus 4.1"))
        XCTAssertTrue(button(containing: "Claude Opus 4.1").label.contains("1.6"), button(containing: "Claude Opus 4.1").label)
        XCTAssertTrue(button(containing: "Auto Model").label.contains("Router"), button(containing: "Auto Model").label)

        let filters = see(el("Filter models"))
        filters.buttons["OpenAI"].tap()
        gone(button(containing: "Claude Opus 4.1"))
        see(button(containing: "GPT-5"))
        filters.buttons["OpenAI"].tap()
        see(button(containing: "Claude Opus 4.1"))

        let search = see(app.searchFields["Search models"])
        search.tap()
        search.typeText("gpt")
        gone(button(containing: "Auto Model"))
        button(containing: "GPT-5").tap()
        gone(app.navigationBars["Choose a model"])
        eventually { (self.lastSettings()?["modelId"] as? String) == "gpt-5" }
        eventually { (self.button("Model").value as? String) == "GPT-5" }

        choose("Reasoning effort", "Extra high")
        eventually { (self.lastSettings()?["reasoningEffort"] as? String) == "xhigh" }
        eventually { (self.button("Reasoning effort").value as? String) == "Extra high" }

        choose("Autonomy", "High autonomy")
        eventually { (self.lastSettings()?["autonomyLevel"] as? String) == "high" }
        eventually { (self.button("Autonomy").value as? String) == "High autonomy" }
    }

    func testFavouriteModelsAreKeptOnThePhone() {
        start()
        tap("Model")
        tap("Star GPT-5")
        tap("Favorites")
        see(button(containing: "GPT-5"))
        gone(button(containing: "Claude Opus 4.1"))

        relaunch()
        see(sessionList, timeout: 20)
        openSession("First session")
        tap("Model")
        see(button("Unstar GPT-5"))
        XCTAssertTrue(daemon.requests("daemon.update_session_settings").isEmpty)
    }
}
