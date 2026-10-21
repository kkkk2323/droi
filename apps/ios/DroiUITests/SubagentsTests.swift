import XCTest

/// Subagents stay out of the Session list. They hang off the Session that called them:
/// a card per Task call, a menu in the header, and a way back from the subagent to its caller.
final class SubagentsTests: DroiTestCase {
    private static let mainId = "0c1d2e3f-0000-4000-8000-00000000a001"
    private static let explorerId = "0c1d2e3f-0000-4000-8000-00000000e001"
    private static let reviewerId = "0c1d2e3f-0000-4000-8000-00000000e002"
    private static let cwd = "/Users/dev/acme-web"

    private static func subagent(_ caller: String, tool: String, type: String, description: String, status: String, extra: [String: Any] = [:]) -> [String: Any] {
        ["callingSessionId": caller, "callingToolUseId": tool, "subagentType": type, "description": description, "status": status].merging(extra) { $1 }
    }

    private var sessions: [[String: Any]] {
        let main = DroiTestCase.session("Plan the release", in: Self.cwd, messages: [
            DroiTestCase.message("user", "Map the mobile app, then review the diff"),
            DroiTestCase.toolCall("toolu_explore", "Task", [
                "subagent_type": "explorer", "description": "Map the mobile app",
                "prompt": "Explore apps/mobile and report its screens.",
            ]),
            DroiTestCase.toolResult("toolu_explore", "The app has **three screens**: list, session, settings."),
            DroiTestCase.toolCall("toolu_review", "Task", [
                "subagent_type": "reviewer", "description": "Review the diff", "prompt": "Review the uncommitted changes.",
            ]),
            DroiTestCase.toolResult(
                "toolu_review",
                "Task launched in background.\ntask_id: \(Self.reviewerId)\nsession_id: \(Self.reviewerId)\nsubagent_type: reviewer\ndescription: Review the diff"),
            DroiTestCase.message("assistant", "The Explorer is done; the Reviewer is still reading."),
        ], extra: ["sessionId": Self.mainId])
        let explorer = DroiTestCase.session("Explorer: Map the mobile app", in: Self.cwd, [
            ("user", "Explore apps/mobile and report its screens."),
            ("assistant", "The app has three screens: list, session, settings."),
        ], extra: [
            "sessionId": Self.explorerId,
            "subagent": Self.subagent(Self.mainId, tool: "toolu_explore", type: "explorer", description: "Map the mobile app", status: "completed", extra: ["toolUseCount": 12, "durationMs": 134_000]),
        ])
        let reviewer = DroiTestCase.session("Reviewer: Review the diff", in: Self.cwd, [("user", "Review the uncommitted changes.")], extra: [
            "sessionId": Self.reviewerId,
            "subagent": Self.subagent(Self.mainId, tool: "toolu_review", type: "reviewer", description: "Review the diff", status: "running"),
        ])
        let other = DroiTestCase.session("Fix the login bug", in: Self.cwd, [("user", "Why does login fail?")])
        return [main, explorer, reviewer, other]
    }

    func testSubagentsAreNotListedAndTheCallingSessionShowsEachAsACard() {
        startDaemon(sessions: sessions)
        launchToList()
        see(button(containing: "Plan the release"))
        XCTAssertFalse(button(containing: "Explorer:").exists)
        XCTAssertFalse(button(containing: "Reviewer:").exists)

        tap("Plan the release")
        see(messageField, timeout: 15)
        see(el("Map the mobile app"))
        see(el("12 tool calls \u{00B7} 2m 14s"))
        let report = see(button("Report"))
        report.tap()
        see(el(containing: "three screens"))
        XCTAssertFalse(el(containing: "**").exists)
        see(el("Review the diff"))
        see(app.descendants(matching: .any).matching(identifier: "Running").firstMatch)

        tap("Open Map the mobile app")
        see(el(containing: "The app has three screens: list, session, settings."))
        see(button("Back to Plan the release"))
    }

    func testTheHeaderLeadsToEachSubagentAndBackToTheCallingSession() {
        startDaemon(sessions: sessions)
        launchToList()
        openSession("Plan the release")
        let menu = see(app.buttons["Subagents"])
        XCTAssertEqual(menu.value as? String, "2")
        menu.tap()
        tap("Reviewer: Review the diff")
        let toCaller = see(button("Back to Plan the release"))
        see(app.staticTexts["Reviewer: Review the diff"])
        toCaller.tap()
        gone(button("Back to Plan the release"))
        see(app.staticTexts["Plan the release"])

        // A subagent opened from its card is pushed; going back returns to its caller.
        tap("Open Map the mobile app")
        see(button("Back to Plan the release"))
        back()
        gone(button("Back to Plan the release"))
        see(app.staticTexts["Plan the release"])
    }

    func testTheCallingSessionsRowSaysHowManySubagentsAreRunning() {
        startDaemon(sessions: sessions)
        launchToList()
        openSession("Plan the release")
        back()
        see(el("1 subagent running"))
    }
}
