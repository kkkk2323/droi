import XCTest

/// Sessions without a Workspace: None starts one in a fresh Scratch Workspace the Gateway makes, listed under Recents.
final class ScratchTests: DroiTestCase {
    private static let scratchFolder = "/Users/test/.droi/chats"
    private static let scratchTag = ["name": "droi.scratch"]

    private func tagNames(_ params: [String: Any]?) -> [String] {
        (params?["tags"] as? [[String: Any]])?.compactMap { $0["name"] as? String } ?? []
    }

    /// The paths the Gateway was asked to move to the Trash.
    private var trashed: [String] {
        daemon.gatewayRequests.filter { $0.path == "/scratch-workspaces/trash" }.compactMap { $0.query["path"] }
    }

    func testNoneIsPreselectedWithNoWorkspaceYetAndTheSessionLandsUnderRecents() {
        startDaemon(sessions: [], turn: ["kind": "reply", "deltas": ["Sure."]])
        launchToList()
        tap("New session")
        see(el("What\u{2019}s on your mind?"))
        XCTAssertEqual(button("Workspace").value as? String, "Workspace: None")
        let draft = daemon.waitForRequest("daemon.initialize_session")
        let folder = draft?["cwd"] as? String ?? ""
        XCTAssertTrue(folder.hasPrefix(ScratchTests.scratchFolder + "/"), folder)
        XCTAssertEqual(tagNames(draft), ["droi.draft", "droi.scratch"])
        XCTAssertTrue(daemon.gatewayRequests.contains { $0.method == "POST" && $0.path == "/scratch-workspaces" })

        messageField.tap()
        messageField.typeText("A pasta recipe for two")
        tap("Start session")
        XCTAssertEqual(tagNames(daemon.waitForRequest("daemon.update_session_settings")), ["droi.scratch"])
        see(el(containing: "Sure."), timeout: 15)
        back()
        see(button("Recents"))
    }

    func testRecentsSitsLastAndNoneMakesAFolderAndLeavingThrowsTheEmptyOneAway() {
        let chat = DroiTestCase.session(
            "Pasta recipe", in: "\(ScratchTests.scratchFolder)/2026-09-20-aaaaaa", [("user", "b")],
            extra: ["tags": [ScratchTests.scratchTag]])
        let project = DroiTestCase.session("Fix the build", in: "/Users/dev/acme-web", [("user", "a")])
        startDaemon(sessions: [chat, project])
        launchToList()
        tap("New session")
        let workspace = see(button("Workspace"))
        eventually { workspace.value as? String == "acme-web?" }
        workspace.tap()
        see(app.navigationBars["Workspace"])
        see(button(containing: "acme-web"))
        button("None").tap()
        see(el("What\u{2019}s on your mind?"))
        let scratch = daemon.waitForRequest("daemon.initialize_session", nth: 2)
        let folder = scratch?["cwd"] as? String ?? ""
        XCTAssertTrue(folder.hasPrefix(ScratchTests.scratchFolder + "/"), folder)

        back()
        see(sessionList)
        let recents = see(button("Recents"))
        XCTAssertGreaterThan(recents.frame.minY, see(button("acme-web")).frame.minY)
        eventually(15, "the empty Scratch folder goes to the Trash") { self.trashed.contains(folder) }
    }

    func testArchivingAScratchSessionMovesItsFolderToTheTrash() {
        let folder = "\(ScratchTests.scratchFolder)/2026-09-20-aaaaaa"
        let chat = DroiTestCase.session("Pasta recipe", in: folder, [("user", "b")], extra: ["tags": [ScratchTests.scratchTag]])
        startDaemon(sessions: [chat])
        launchToList()
        press(button("Pasta recipe"), choose: "Archive")
        XCTAssertNotNil(daemon.waitForRequest("daemon.archive_session"))
        eventually(15) { self.trashed.contains(folder) }
    }
}
