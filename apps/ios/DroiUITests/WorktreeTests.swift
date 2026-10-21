import XCTest

/// Worktree Sessions in the list and on the New session page.
final class WorktreeTests: DroiTestCase {
    private static let repo = "/Users/dev/acme-web"
    private static let worktrees = "/Users/test/.factory/worktrees"

    private static func worktreeSession(_ title: String, _ id: String, branch: String, lifecycle: String) -> [String: Any] {
        let path = "\(worktrees)/\(id)/acme-web"
        return session(title, in: path, [("user", "x")], extra: [
            "worktree": ["path": path, "branch": branch, "lifecycle": lifecycle, "repoRoot": repo],
        ])
    }

    private let finance = DroiTestCase.session("Budget", in: "/Users/dev/finance", [("user", "sum")])
    private let ephemeral = WorktreeTests.worktreeSession("Fix login", "aaaa1111", branch: "droid/fix-login", lifecycle: "ephemeral")
    private let persistent = WorktreeTests.worktreeSession("Try Redis", "bbbb2222", branch: "droid/try-redis", lifecycle: "persistent")
    private let main = DroiTestCase.session("Main work", in: WorktreeTests.repo, [("user", "hi")])

    // The last Session made is the most recent, so New session opens on acme-web.
    private func start() {
        startDaemon(
            sessions: [finance, ephemeral, persistent, main], turn: ["kind": "reply", "deltas": ["On it."]],
            input: ["gitRepos": [WorktreeTests.repo: ["currentBranch": "main", "branches": ["main", "develop"]]]])
        launchToList()
    }

    func testWorktreeSessionsListUnderTheirRepositoryWithTheirBranch() {
        start()
        see(button("acme-web"))
        see(button("finance"))
        XCTAssertTrue(see(button(containing: "Fix login")).label.contains("droid/fix-login"))
        XCTAssertTrue(see(button(containing: "Try Redis")).label.contains("droid/try-redis"))
        XCTAssertFalse(see(button(containing: "Main work")).label.contains("droid/"))

        // The worktree's folder is not a Workspace to start in.
        tap("New session")
        button("Workspace").tap()
        see(app.navigationBars["Workspace"])
        see(button(containing: "finance"))
        XCTAssertFalse(button(containing: "aaaa1111").exists)
        XCTAssertFalse(button(containing: "bbbb2222").exists)
    }

    func testStartsASessionInANewWorktreeFromAChosenBranch() {
        start()
        tap("New session")
        let worktree = see(button("Worktree"))
        eventually { worktree.value as? String == "Work locally" }
        worktree.tap()
        see(app.navigationBars["Worktree"])
        XCTAssertFalse(button(containing: "Persistent").exists)
        button(containing: "New worktree").tap()
        see(button(containing: "Single-session worktrees that are cleaned up automatically."))
        see(button(containing: "Multi-session worktrees that can only be deleted manually."))
        button(containing: "Persistent").tap()
        tap("Base branch")
        see(app.navigationBars["Base branch"])
        button(containing: "develop").tap()
        see(app.navigationBars["Worktree"])
        eventually { self.button("Base branch").value as? String == "develop" }
        dismissSheet(titled: "Worktree")
        eventually { worktree.value as? String == "New worktree · Persistent · from develop" }

        messageField.tap()
        messageField.typeText("Refactor the invoices")
        tap("Start session")
        let created = daemon.waitForRequest("daemon.initialize_session", nth: 2)
        XCTAssertEqual(created?["cwd"] as? String, WorktreeTests.repo)
        XCTAssertEqual(created?["worktree"] as? Bool, true)
        XCTAssertEqual(created?["worktreeLifecycle"] as? String, "persistent")
        XCTAssertEqual(created?["worktreePromptSlug"] as? String, "Refactor the invoices")
        XCTAssertEqual(created?["worktreeBaseBranch"] as? String, "develop")
        XCTAssertEqual(created?["worktreeBranchMode"] as? String, "copy")
        // The draft in the repository itself gives way to the worktree's Session.
        XCTAssertNotNil(daemon.waitForRequest("daemon.close_session"))
        XCTAssertTrue(daemon.requests("daemon.update_session_settings").isEmpty)
        see(el(containing: "Refactor the invoices"), timeout: 15)
    }

    func testWorkLocallyAsksTheDaemonForNoWorktree() {
        start()
        tap("New session")
        let worktree = see(button("Worktree"))
        eventually { worktree.value as? String == "Work locally" }
        let draft = daemon.waitForRequest("daemon.initialize_session")
        XCTAssertNil(draft?["worktree"])
        messageField.tap()
        messageField.typeText("Look around")
        tap("Start session")
        XCTAssertNotNil(daemon.waitForRequest("daemon.update_session_settings"))
        XCTAssertNotNil(daemon.waitForRequest("daemon.add_user_message"))
        XCTAssertEqual(daemon.requests("daemon.initialize_session").count, 1)
    }

    func testAFolderThatIsNotAGitRepositoryHasNoWorktreeOption() {
        start()
        tap("New session")
        see(button("Worktree"))
        button("Workspace").tap()
        see(app.navigationBars["Workspace"])
        button(containing: "finance").tap()
        eventually { self.button("Workspace").value as? String == "finance?" }
        XCTAssertNotNil(daemon.waitForRequest("daemon.list_git_branches", nth: 2))
        gone(button("Worktree"))
    }

    func testStartingWithAWorktreeByDefaultIsRememberedOnThisPhone() {
        start()
        tap("New session")
        see(button("Worktree")).tap()
        let toggle = see(app.switches["Start with a worktree by default"])
        toggle.switches.firstMatch.tap()
        dismissSheet(titled: "Worktree")
        eventually { self.button("Worktree").value as? String == "New worktree · Ephemeral · from main" }

        relaunch()
        see(sessionList, timeout: 20)
        tap("New session")
        eventually { self.button("Worktree").value as? String == "New worktree · Ephemeral · from main" }
        tap("Start session")
        eventually { self.daemon.requests("daemon.initialize_session").contains { $0["worktree"] as? Bool == true } }
        let request = daemon.requests("daemon.initialize_session").last { $0["worktree"] as? Bool == true }
        XCTAssertEqual(request?["worktreeLifecycle"] as? String, "ephemeral")
        XCTAssertNotNil(request)
        XCTAssertTrue(request?["worktreePromptSlug"] == nil || (request?["worktreePromptSlug"] as? String) == "")
    }

    func testArchivingASessionInAnEphemeralWorktreeAsksFirst() {
        start()
        let row = see(button(containing: "Fix login"))
        row.press(forDuration: 1.0)
        tap("Archive")
        see(app.staticTexts["Archiving this session will delete its worktree."])
        XCTAssertTrue(daemon.requests("daemon.archive_session").isEmpty)
        // The question is a popover here: a tap outside it is the cancel.
        app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.95)).tap()
        gone(app.staticTexts["Archiving this session will delete its worktree."])
        XCTAssertTrue(daemon.requests("daemon.archive_session").isEmpty)

        press(button(containing: "Fix login"), choose: "Archive")
        see(app.staticTexts["Archiving this session will delete its worktree."])
        app.buttons["Archive"].tap()
        let archived = daemon.waitForRequest("daemon.archive_session")
        XCTAssertEqual(archived?["sessionId"] as? String, daemon.session("Fix login").id)
    }

    func testArchivingASessionInAPersistentWorktreeNeedsNoConfirmation() {
        start()
        press(button(containing: "Try Redis"), choose: "Archive")
        let archived = daemon.waitForRequest("daemon.archive_session")
        XCTAssertEqual(archived?["sessionId"] as? String, daemon.session("Try Redis").id)
        XCTAssertFalse(app.staticTexts["Archiving this session will delete its worktree."].exists)
    }
}
