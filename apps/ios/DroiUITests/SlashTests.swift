import XCTest

/// "/" in the composer: commands and skills, as a menu and as a tag before the text.
final class SlashTests: DroiTestCase {
    private func start() {
        startDaemon(
            sessions: [DroiTestCase.session("Chat", in: "/Users/dev/acme-web", [("user", "hi")])],
            turn: ["kind": "reply", "deltas": ["Skill \"handoff\" activated"]],
            input: [
                "commands": [["name": "opsx-propose", "description": "Propose a change", "argumentHint": "command arguments"]],
                "skills": [
                    ["name": "handoff", "description": "Compact the conversation"],
                    ["name": "hidden-helper", "description": "Not for users", "userInvocable": false],
                ],
            ])
        launchToList()
        openSession("Chat")
        // The menu needs the commands and skills the Session loaded.
        XCTAssertNotNil(daemon.waitForRequest("daemon.list_skills"))
    }

    func testSlashOffersCommandsAndSkillsFiltersAsYouTypeAndInsertsThePick() {
        start()
        messageField.tap()
        messageField.typeText("/")
        let menu = see(el("Commands and skills"))
        let items = menu.buttons
        eventually { items.count == 3 }
        XCTAssertTrue(items.element(boundBy: 0).label.contains("/compact"))
        XCTAssertTrue(items.element(boundBy: 1).label.contains("/opsx-propose"))
        XCTAssertTrue(items.element(boundBy: 2).label.contains("/handoff"))
        XCTAssertFalse(el(containing: "hidden-helper").exists)

        messageField.typeText("ha")
        eventually { menu.buttons.count == 1 }
        menu.buttons.firstMatch.tap()
        // The pick shows as a tag before the text, so it reads as a skill to run.
        see(el("Skill handoff"))
        XCTAssertTrue((messageField.value as? String ?? "").isEmpty || messageField.value as? String == messageField.placeholderValue)
        gone(el("Commands and skills"))

        messageField.typeText("next session is for tests")
        tap("Send")
        let sent = daemon.waitForRequest("daemon.add_user_message")
        XCTAssertEqual(sent?["text"] as? String, "/handoff next session is for tests")
    }

    func testATypedNameBecomesATagBackspaceOrItsRemoveButtonDropsIt() {
        start()
        messageField.tap()
        messageField.typeText("/opsx-propose ")
        see(el("Command opsx-propose"))
        eventually(5, "placeholder is \(messageField.placeholderValue ?? "nil"), value \(messageField.value ?? "nil")") { self.messageField.placeholderValue == "command arguments" }
        messageField.typeText(XCUIKeyboardKey.delete.rawValue)
        gone(el("Command opsx-propose"))

        messageField.typeText("/handoff tidy up")
        tap("Remove skill handoff")
        gone(el("Skill handoff"))
        eventually { self.messageField.value as? String == "tidy up" }
        tap("Send")
        XCTAssertEqual(daemon.waitForRequest("daemon.add_user_message")?["text"] as? String, "tidy up")
    }
}
