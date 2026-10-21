import XCTest

/// Permission requests and Droid's questions take the composer's place until answered.
final class PromptTests: DroiTestCase {
    private let deploy = DroiTestCase.session("Deploy", in: "/Users/dev/acme-web", [("user", "hi")])

    private func openAndSend(_ text: String) {
        launchToList()
        openSession("Deploy")
        send(text)
    }

    private let permission: [String: Any] = [
        "kind": "permission", "command": "rm -rf build", "reply": "Build directory removed.",
        "toolOutput": "[Process exited with code 0]",
    ]

    func testAllowLetsTheTurnContinueAndTheCardTakesTheComposersPlace() {
        startDaemon(sessions: [deploy], turn: permission)
        openAndSend("Clean the build dir")
        let card = see(el("Permission request: Execute"), timeout: 15)
        see(card.staticTexts.matching(NSPredicate(format: "label CONTAINS 'rm -rf build'")).firstMatch)
        XCTAssertFalse(messageField.exists)
        card.buttons["Yes, allow"].tap()
        gone(el("Permission request: Execute"))
        see(el(containing: "Build directory removed."), timeout: 15)
        see(messageField)
    }

    func testDenyEndsTheTurnWithoutRunningTheTool() {
        startDaemon(sessions: [deploy], turn: permission)
        openAndSend("Clean the build dir")
        let card = see(el("Permission request: Execute"), timeout: 15)
        card.buttons["No, cancel"].tap()
        gone(el("Permission request: Execute"))
        see(el(containing: "I will not run that"), timeout: 15)
        XCTAssertFalse(el(containing: "Build directory removed.").exists)
    }

    func testTheChosenAnswerGoesBackAndTheTurnContinues() {
        startDaemon(sessions: [deploy], turn: ["kind": "askUser", "question": "Which environment?", "options": ["staging", "production"]])
        openAndSend("Deploy it")
        let card = see(el("Droid has a question"), timeout: 15)
        see(card.staticTexts["Which environment?"])
        let answer = card.buttons["Answer"]
        XCTAssertFalse(answer.isEnabled)
        let staging = card.buttons["staging"]
        staging.tap()
        eventually { staging.isSelected }
        XCTAssertTrue(answer.isEnabled)
        answer.tap()
        gone(el("Droid has a question"))
        see(el(containing: "You chose staging."), timeout: 15)
    }

    func testCancelDeclinesTheQuestion() {
        startDaemon(sessions: [deploy], turn: ["kind": "askUser", "question": "Which environment?", "options": ["staging", "production"]])
        openAndSend("Deploy it")
        let card = see(el("Droid has a question"), timeout: 15)
        card.buttons["Cancel"].tap()
        gone(el("Droid has a question"))
        see(el(containing: "I will not ask"), timeout: 15)
        XCTAssertFalse(el(containing: "You chose").exists)
        see(messageField)
    }
}
