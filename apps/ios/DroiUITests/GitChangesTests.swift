import XCTest

/// The header's branch button and the sheet of changed files.
final class GitChangesTests: DroiTestCase {
    private let dirty = DroiTestCase.session("Fix the login bug", in: "/Users/dev/acme-web", [("user", "Why?")], extra: [
        "git": [
            "branch": "fix/login",
            "files": [
                ["path": "src/auth/login.ts", "status": "modified", "additions": 12, "deletions": 3],
                ["path": "src/auth/login.test.ts", "status": "added", "additions": 40, "deletions": 0],
            ],
        ],
    ])
    private let clean = DroiTestCase.session("Add dark mode", in: "/Users/dev/acme-web", [("user", "Add dark mode")], extra: [
        "git": ["branch": "main", "files": [Any]()],
    ])
    private let notRepo = DroiTestCase.session("Refactor billing", in: "/Users/dev/scratch", [("user", "Refactor the invoice generator")])

    func testTheHeaderShowsTheBranchAndOpensTheChangedFiles() {
        startDaemon(sessions: [dirty, clean, notRepo])
        launchToList()
        openSession("Fix the login bug")
        let branch = see(button("Branch fix/login, 2 changed files"))
        XCTAssertTrue(branch.staticTexts["fix/login"].exists || branch.label.contains("fix/login"))
        branch.tap()
        see(el("2 uncommitted files"))
        let files = see(el("Changed files"))
        let first = see(files.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS 'login.ts' AND NOT label CONTAINS 'test'")).firstMatch)
        XCTAssertTrue(first.label.contains("src/auth/"), first.label)
        XCTAssertTrue(first.label.contains("12 added, 3 removed"), first.label)
        let second = see(files.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS 'login.test.ts'")).firstMatch)
        XCTAssertTrue(second.label.contains("40 added, 0 removed"), second.label)
    }

    func testTheDiffIsReadAgainWhenATurnEnds() {
        startDaemon(sessions: [dirty, clean, notRepo])
        launchToList()
        openSession("Add dark mode")
        see(button("Branch main, no changes"))
        let reads = { self.daemon.requests.filter { $0.method.contains("git") }.count }
        let before = reads()
        let session = daemon.session("Add dark mode")
        daemon.notify(session, ["type": "droid_working_state_changed", "newState": "executing_tool"])
        daemon.notify(session, ["type": "droid_working_state_changed", "newState": "idle"])
        eventually(10, "the app reads the Git state again") { reads() > before }
    }

    func testAPlainDirectoryShowsNothing() {
        startDaemon(sessions: [dirty, clean, notRepo])
        launchToList()
        openSession("Refactor billing")
        see(el(containing: "invoice generator"))
        XCTAssertEqual(app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Branch '")).count, 0)
    }
}
