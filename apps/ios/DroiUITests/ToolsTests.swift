import XCTest

/// The Session's skills and MCP servers, behind the wrench in the header; and the Memory Server.
final class ToolsTests: DroiTestCase {
    private let chat = DroiTestCase.session("Chat", in: "/Users/dev/acme-web", [("user", "hi")])

    private func openTools() {
        openSession("Chat")
        tap("Skills and MCP servers")
        see(button("Skills"))
    }

    private func start(input: [String: Any], sessions: [[String: Any]]? = nil) {
        startDaemon(sessions: sessions ?? [chat], input: input)
        launchToList()
        openTools()
    }

    func testASkillIsSwitchedOffAndComesBack() {
        start(input: [
            "skills": [
                ["name": "review", "description": "Review a branch", "location": "builtin"],
                ["name": "mine", "description": "My own", "location": "personal"],
                ["name": "locked", "location": "builtin", "disabledAt": ["org"]],
            ],
        ])
        let review = see(app.switches["review enabled"])
        XCTAssertFalse(app.switches["mine enabled"].exists)
        see(el("Disabled by organization"))
        XCTAssertFalse(app.switches["locked enabled"].isEnabled)

        XCTAssertEqual(review.value as? String, "1")
        review.tap()
        let request = daemon.waitForRequest("daemon.set_skill_disabled")
        XCTAssertEqual(request?["skillName"] as? String, "review")
        XCTAssertEqual(request?["disabled"] as? Bool, true)
        XCTAssertEqual(request?["settingsLevel"] as? String, "user")
        eventually { self.app.switches["review enabled"].value as? String == "0" }
        see(el("review").staticTexts["Disabled"])

        app.switches["review enabled"].tap()
        XCTAssertNotNil(daemon.waitForRequest("daemon.set_skill_disabled", nth: 2))
        eventually { self.app.switches["review enabled"].value as? String == "1" }
        gone(el("review").staticTexts["Disabled"])
    }

    func testServersSwitchShowToolsAskToSignInOnTheComputerAndAreAdded() {
        start(input: [
            "mcpServers": [
                ["name": "files", "type": "stdio", "tools": [["name": "read_file"], ["name": "write_file"]]],
                ["name": "linear", "type": "http", "requiresAuth": true],
                ["name": "company-wiki", "type": "http", "source": "org", "toolCount": 3],
            ],
            "mcpRegistry": [
                [
                    "name": "adyen", "description": "Payment processing for Adyen", "type": "stdio", "command": "npx",
                    "args": ["-y", "@adyen/mcp", "--adyenApiKey=ADYEN_API_KEY"], "note": "Replace ADYEN_API_KEY before use.",
                ],
                ["name": "github", "description": "GitHub", "type": "http", "url": "https://api.githubcopilot.com/mcp/"],
            ],
        ])
        tap("MCP servers")
        let files = see(el("files"))
        see(files.descendants(matching: .any)["Connected"])
        XCTAssertFalse(app.switches["company-wiki enabled"].isEnabled)

        app.switches["files enabled"].tap()
        let toggled = daemon.waitForRequest("daemon.toggle_mcp_server")
        XCTAssertEqual(toggled?["serverName"] as? String, "files")
        XCTAssertEqual(toggled?["enabled"] as? Bool, false)
        see(el("files").descendants(matching: .any)["Disabled"])
        app.switches["files enabled"].tap()
        see(el("files").descendants(matching: .any)["Connected"])

        tap("2 tools")
        let write = see(app.switches["write_file enabled"])
        write.tap()
        let tool = daemon.waitForRequest("daemon.toggle_mcp_tool")
        XCTAssertEqual(tool?["serverName"] as? String, "files")
        XCTAssertEqual(tool?["toolName"] as? String, "write_file")
        XCTAssertEqual(tool?["enabled"] as? Bool, false)

        tap("Sign in")
        let notice = see(el("Sign in to linear"))
        see(notice.staticTexts["The sign-in finishes on the computer running Droi: open the page there."])
        tap("Copy sign-in link")
        see(button("Link copied"))
        tap("Cancel")
        gone(el("Sign in to linear"))

        // The catalogue is a view of its own: search, then one tap adds.
        tap("Add server")
        let search = see(app.textFields["Search the catalogue"])
        search.tap()
        search.typeText("git")
        gone(button("Set up adyen"))
        tap("Add github")
        let added = daemon.waitForRequest("daemon.add_mcp_server")
        XCTAssertEqual(added?["name"] as? String, "github")
        XCTAssertEqual(added?["type"] as? String, "http")
        see(el("github"))
        gone(el("Catalogue"))

        // An entry with a value to fill in opens the form, filled, with its note.
        tap("Add server")
        tap("Set up adyen")
        see(el("Replace ADYEN_API_KEY before use."))
        XCTAssertEqual(see(app.textFields["Arguments"]).value as? String, "-y @adyen/mcp --adyenApiKey=ADYEN_API_KEY")
        // The page change animates, and the catalogue's Back is still on screen for a moment.
        eventually { self.app.buttons.matching(identifier: "Back").count == 1 }
        tap("Back")
        tap("Add a server by hand")
        see(el("Add a server by hand"))
        XCTAssertNotEqual(see(app.textFields["Server name"]).value as? String, "adyen")
    }

    func testTheMemoryServerShowsAsDroisWithNoSwitchAndAPointerToTheComputer() {
        let work = DroiTestCase.session("Invoice export", in: "/Users/dev/billing-service", [("user", "a")])
        // The Desktop Shell's own model work; no Client lists it.
        let memory = DroiTestCase.session("Memory: extract from 0427515a", in: "/Users/dev/billing-service", [("user", "{}")], extra: ["tags": [["name": "droi.memory"]]])
        startDaemon(
            sessions: [work, memory],
            input: [
                "mcpServers": [
                    ["name": "files", "type": "stdio", "tools": [["name": "read_file"]]],
                    ["name": "droi-memory", "type": "stdio", "tools": [["name": "memory_search"]]],
                ],
            ])
        launchToList()
        see(button("Invoice export"))
        XCTAssertFalse(button(containing: "Memory: extract").exists)

        openSession("Invoice export")
        tap("Skills and MCP servers")
        tap("MCP servers")
        let row = see(el("droi-memory"))
        see(row.staticTexts["Droi"])
        see(row.staticTexts["Droi\u{2019}s Memory. Turn it on or off on the computer in Settings \u{2192} Memory."])
        XCTAssertFalse(app.switches["droi-memory enabled"].exists)
        XCTAssertTrue(see(app.switches["files enabled"]).isEnabled)
    }
}
