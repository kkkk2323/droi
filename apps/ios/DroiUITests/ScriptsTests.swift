import XCTest

/// Script runs: the calls a run made hang under its Script, a WaitForScript picks the run up where it
/// sits in the turn, and a reply's <json-render> output is drawn in place.
/// Not covered: the Script permission card (its turn needs a custom handler the Fake Daemon's serve.ts
/// cannot be given) and TeX typesetting (the app shows TeX as text).
final class ScriptsTests: DroiTestCase {
    private static let run = "toolu_script"

    private static let source = [
        "const pkg = await tools.Read({ file_path: 'package.json' })",
        "text(pkg)",
        "const tests = await tools.Execute({ summary: 'Run the tests', command: 'pnpm test', riskLevel: 'low' })",
        "text(tests)",
        "const build = await tools.Execute({ summary: 'Build the app', command: 'pnpm build', riskLevel: 'low' })",
        "text(build)",
    ].joined(separator: "\n")

    /// A call the Script made, tagged with the run it belongs to.
    private static func nested(_ id: String, _ name: String, _ input: [String: Any]) -> [String: Any] {
        message("assistant", content: [[
            "type": "tool_use", "id": id, "name": name, "input": input,
            "scriptExecution": ["runId": run, "outerToolUseId": run],
        ]])
    }

    private static func resultBlocks(_ id: String, _ texts: [String]) -> [String: Any] {
        message("tool", content: [["type": "tool_result", "toolUseId": id, "content": texts.map { ["type": "text", "text": $0] }]])
    }

    private static let audit = session("Audit the build", in: "/Users/dev/acme-web", messages: [
        message("user", "Check the package, run the tests, then build"),
        toolCall(run, "Script", ["script": source, "inputs": ["notes": "Build with the release flags."]]),
        nested("\(run)-1", "Read", ["file_path": "package.json"]),
        toolResult("\(run)-1", "{ \"name\": \"acme-web\" }"),
        nested("\(run)-3", "Execute", ["summary": "Run the tests", "command": "pnpm test", "riskLevel": "low"]),
        toolResult(run, "{\"toolCallId\":\"\(run)\",\"status\":\"running\"}"),
        toolResult("\(run)-3", "42 passed\n\n[Process exited with code 0]"),
        message("assistant", "The tests are still running; waiting for them."),
        toolCall("toolu_wait", "WaitForScript", ["toolCallId": run]),
        nested("\(run)-5", "Execute", ["summary": "Build the app", "command": "pnpm build", "riskLevel": "low"]),
        toolResult("\(run)-5", "build ok\n\n[Process exited with code 0]"),
        resultBlocks("toolu_wait", [
            "{ \"name\": \"acme-web\" }\n42 passed\nbuild ok",
            "[Script completed \u{00B7} 3 calls \u{00B7} 120 B in sandbox \u{00B7} 48 B emitted (40%) \u{00B7} retained: r1 Read package.json, r2 Execute Run the tests, r3 Execute Build the app \u{00B7} log: /tmp/s1.log]",
            "{\"toolCallId\":\"\(run)\",\"status\":\"completed\",\"result\":null}",
        ]),
        toolCall("toolu_broken", "Script", ["script": "const notes = await tools.Read({ file_path: missing })"]),
        resultBlocks("toolu_broken", [
            "[Script failed at 1:43 \u{00B7} 0 calls completed \u{00B7} 0 B emitted \u{00B7} log: /tmp/broken.log]",
            "{\"toolCallId\":\"toolu_broken\",\"status\":\"failed\",\"error\":\"ReferenceError: missing is not defined\"}",
        ]),
        message("assistant", "Tests pass and the build is green."),
    ])

    private static let report: [String: Any] = [
        "root": "card",
        "elements": [
            "card": ["type": "Card", "props": ["title": "Release 1.26"], "children": ["status", "table", "progress", "bars"]],
            "status": ["type": "StatusLine", "props": ["text": "All checks passed", "status": "success"], "children": []],
            "table": [
                "type": "Table",
                "props": [
                    "columns": [["header": "Package", "key": "name"], ["header": "Version", "key": "version"]],
                    "rows": [["name": "droi", "version": "1.26.0"], ["name": "@droi/mobile", "version": "0.4.0"]],
                ],
                "children": [],
            ],
            "progress": ["type": "ProgressBar", "props": ["progress": 0.75, "label": "Rollout"], "children": []],
            "bars": [
                "type": "BarChart",
                "props": ["data": [["label": "macOS", "value": 60], ["label": "iPhone", "value": 40]], "showPercentage": true],
                "children": [],
            ],
        ],
    ]

    private static var releaseReport: [String: Any] {
        let json = String(decoding: try! JSONSerialization.data(withJSONObject: report, options: [.sortedKeys]), as: UTF8.self)
        let reply = [
            "Here is where the release stands, drawn from a `<json-render>` tag.", "",
            "<json-render>\(json)</json-render>", "",
            "Ship once the last check is green.", "",
            "```html", "<json-render>{\"root\":\"quoted\"}</json-render>", "```",
        ].joined(separator: "\n")
        return session("Release report", in: "/Users/dev/acme-web", messages: [
            message("user", "Summarize the release"), message("assistant", reply),
        ])
    }

    func testAScriptListsItsCallsUnderItAndOpensToItsSourceAndOutput() {
        startDaemon(sessions: [Self.audit])
        launchToList()
        openSession("Audit the build")

        let script = scrollTo(button(containing: "Script, Read"))
        see(script)
        XCTAssertTrue(script.label.contains("Still running"), script.label)
        XCTAssertEqual(script.value as? String, "Collapsed")
        let calls = app.otherElements.matching(identifier: "Calls made by Script")
        eventually { calls.count == 2 }
        let tests = calls.containing(NSPredicate(format: "label CONTAINS 'Run the tests'")).firstMatch
        see(tests.buttons.matching(NSPredicate(format: "label CONTAINS 'Read'")).firstMatch)
        see(tests.buttons.matching(NSPredicate(format: "label CONTAINS 'Execute'")).firstMatch)

        script.tap()
        XCTAssertEqual(script.value as? String, "Expanded")
        see(el(containing: "summary: 'Build the app'"))
        let wait = scrollTo(button(containing: "WaitForScript"))
        wait.tap()
        see(el(containing: "build ok"))
        XCTAssertFalse(el(containing: "toolCallId").exists)
        see(el(containing: "3 calls"))
        XCTAssertTrue(el(containing: "120 B in sandbox").exists)

        let broken = scrollTo(button(containing: "Script, Read"), swipes: 4)
        XCTAssertTrue(broken.exists)
    }

    func testAReplyDrawsItsJsonRenderOutputInPlace() {
        startDaemon(sessions: [Self.releaseReport])
        launchToList()
        openSession("Release report")
        see(el(containing: "Release 1.26"))
        see(el(containing: "All checks passed"))
        for text in ["Package", "Version", "droi", "1.26.0", "@droi/mobile", "0.4.0", "macOS", "iPhone"] {
            see(app.staticTexts[text], timeout: 5, text)
        }
        XCTAssertEqual(see(el("Rollout")).value as? String, "75 percent")
        see(el(containing: "Ship once the last check is green."))
        XCTAssertFalse(el(containing: "\"elements\"").exists)
        // A tag inside a code fence is code, not a card.
        see(el(containing: "{\"root\":\"quoted\"}"))
    }
}
