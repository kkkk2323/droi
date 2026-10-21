import DroidProtocol
import Testing

@testable import DroiKit

@Suite struct ToolCallsTests {
    @Test func splitsAnMcpToolIntoItsServerAndTool() {
        #expect(toolDisplayName("droi-memory___memory_list") == ToolName(server: "droi-memory", tool: "memory_list"))
        #expect(toolDisplayName("exa___web_search_exa") == ToolName(server: "exa", tool: "web_search_exa"))
    }

    @Test func leavesTheDaemonsOwnToolsWhole() {
        #expect(toolDisplayName("Execute") == ToolName(server: nil, tool: "Execute"))
        #expect(toolDisplayName("___odd") == ToolName(server: nil, tool: "___odd"))
        #expect(toolDisplayName("odd___") == ToolName(server: nil, tool: "odd___"))
    }

    @Test func aHeadlineInputStandsAlone() {
        #expect(
            toolSummaryParts(call("Execute", ["command": "ls -la\npwd", "summary": "List"]))
                == [SummaryPart(key: nil, value: "List")])
        #expect(
            toolSummaryParts(call("Read", ["file_path": "/w/a.ts", "limit": 40]))
                == [SummaryPart(key: nil, value: "/w/a.ts")])
        #expect(toolSummaryParts(call("Skill", ["skill": "grilling"])) == [SummaryPart(key: nil, value: "grilling")])
    }

    // The protocol keeps no key order, so "first" is by key name; the
    // TypeScript Client follows the order the model wrote them in.
    @Test func anyOtherToolShowsItsFirstShortInputsAsKeyValue() {
        #expect(
            toolSummaryParts(call("droi-memory___memory_list", ["scope": "project"]))
                == [SummaryPart(key: "scope", value: "project")])
        #expect(
            toolSummaryParts(
                call("droi-memory___memory_add", [
                    "scope": "project", "category": "insight",
                    "text": .string(String(repeating: "x", count: 70) + "\nsecond line"), "zzz": "not shown",
                ]))
                == [
                    SummaryPart(key: "category", value: "insight"),
                    SummaryPart(key: "scope", value: "project"),
                    SummaryPart(key: "text", value: String(repeating: "x", count: 57) + "…"),
                ])
    }

    @Test func skipsBlanksAndNestedValuesKeepsNumbersAndBooleans() {
        #expect(
            toolSummaryParts(call("t", ["a": "  ", "b": ["c": 1], "d": [1], "limit": 10, "block": false]))
                == [SummaryPart(key: "block", value: "false"), SummaryPart(key: "limit", value: "10")])
        #expect(toolSummaryParts(call("t")).isEmpty)
    }

    @Test func joinsThePartsIntoOneLine() {
        #expect(toolSummary(call("Execute", ["command": "ls"])) == "ls")
        #expect(
            toolSummary(call("droi-memory___memory_list", ["scope": "project", "category": "insight"]))
                == "category: insight · scope: project")
        #expect(toolSummary(call("t")) == "")
    }

    @Test func readsCreatesBareSuccessObject() {
        #expect(parseStatusResult("{\"success\":true,\"file_path\":\"/w/a.ts\"}") == StatusResult(success: true, message: nil))
        #expect(
            parseStatusResult("{\"success\":false,\"error\":\"EACCES: permission denied\"}")
                == StatusResult(success: false, message: "EACCES: permission denied"))
    }

    @Test func aResultWithMoreToSayIsLeftAsItCame() {
        #expect(parseStatusResult("{\"success\":true,\"stdout\":\"hi\"}") == nil)
        #expect(parseStatusResult("[Process exited with code 0]") == nil)
        #expect(parseStatusResult("{\"success\":\"yes\"}") == nil)
        #expect(parseStatusResult("{\"success\": not json") == nil)
    }

    @Test func readsTheDaemonsEditResultIntoLinesWithBothNumbers() {
        let result = parseDiffResult(
            JSONText.stringify([
                "success": true, "file_path": "/w/a.ts",
                "diffLines": [
                    ["type": "unchanged", "content": "a", "lineNumber": ["old": 1, "new": 1]],
                    ["type": "removed", "content": "b", "lineNumber": ["old": 2]],
                    ["type": "added", "content": "c", "lineNumber": ["new": 2]],
                    ["type": "added", "content": "d", "lineNumber": ["new": 3]],
                ],
            ]))
        #expect(result?.added == 2)
        #expect(result?.removed == 1)
        #expect(
            result?.lines == [
                DiffLine(type: .unchanged, content: "a", old: 1, new: 1),
                DiffLine(type: .removed, content: "b", old: 2, new: nil),
                DiffLine(type: .added, content: "c", old: nil, new: 2),
                DiffLine(type: .added, content: "d", old: nil, new: 3),
            ])
    }

    @Test func otherResultsAreNoDiff() {
        #expect(parseDiffResult("[Process exited with code 0]") == nil)
        #expect(parseDiffResult("{\"success\":true}") == nil)
        #expect(parseDiffResult("{\"diffLines\": not json") == nil)
    }

    @Test func showsACreatesContentAsNumberedAddedLines() {
        let diff = createdFileDiff(call("Create", ["file_path": "a.ts", "content": "one\ntwo\n"]))
        #expect(
            diff?.lines == [
                DiffLine(type: .added, content: "one", old: nil, new: 1),
                DiffLine(type: .added, content: "two", old: nil, new: 2),
            ])
        #expect(diff?.added == 2)
        #expect(diff?.removed == 0)
        #expect(createdFileDiff(call("Edit", ["content": "x"])) == nil)
        #expect(createdFileDiff(call("Create", ["file_path": "a.ts"])) == nil)
    }

    @Test func permissionDetailShowsTheFullCommandThenTheFileThenTheInput() {
        #expect(permissionDetail(["fullCommand": "npm test -- --watch"], input: ["command": "npm test"]) == "$ npm test -- --watch")
        #expect(permissionDetail(["filePath": "/w/a.ts"], input: ["file_path": "/w/a.ts"]) == "/w/a.ts")
        #expect(permissionDetail(nil as JSONValue?, input: ["command": "ls"]) == "$ ls")
        #expect(permissionDetail(JSONValue.null, input: ["url": "https://x"]) == "{\"url\":\"https://x\"}")
    }

    @Test func aScriptPermissionListsEachCallByLine() {
        let details: JSONValue = [
            "type": "script",
            "impactLevel": "medium",
            "calls": [
                [
                    "toolName": "Execute", "line": 3, "column": 26,
                    "toolInput": ["command": "sleep 70 && echo slept", "riskLevel": "low"],
                    "confirmation": [
                        "type": "exec", "fullCommand": "sleep 70 && echo slept", "command": "sleep, echo",
                        "impactLevel": "low",
                    ],
                ],
                [
                    "toolName": "droi-memory___memory_add", "line": 5, "column": 1,
                    "toolInput": ["text": "x"],
                    "confirmation": ["type": "mcp_tool", "toolName": "memory_add"],
                ],
            ],
        ]
        #expect(
            scriptPermission(details)
                == ScriptPermission(
                    impact: "medium",
                    calls: [
                        ScriptPermissionCall(line: 3, tool: "Execute", detail: "$ sleep 70 && echo slept", impact: "low"),
                        ScriptPermissionCall(line: 5, tool: "memory_add", detail: "{\"text\":\"x\"}", impact: nil),
                    ]))
        #expect(scriptPermission(["type": "exec", "fullCommand": "ls"] as JSONValue) == nil)
        #expect(scriptPermission(nil as JSONValue?) == nil)
    }

    private func done(_ name: String, _ nested: [ToolCall]? = nil) -> ToolCall {
        call(name, result: "ok", id: name, nested: nested)
    }

    @Test func aScriptCountsAsTheCallsItMade() {
        let label = clusterLabel([done("Script", [done("Read"), done("Grep")]), done("LS")])
        #expect(label.label == "Used 3 tools")
        #expect(label.pending == false)
        #expect(clusterLabel([done("Script", [done("Read")])]).label == "Used Read")
        #expect(clusterLabel([done("Script", [])]).label == "Used Script")
    }

    @Test func pendingWhileAScriptsCallIsStillRunning() {
        let label = clusterLabel([done("Script", [call("Execute")])])
        #expect(label.label == "Running Execute")
        #expect(label.pending)
    }

    @Test func truncatesLongResults() {
        #expect(truncateLines("a\nb\nc", max: 2) == "a\nb\n… 1 more lines")
        #expect(truncateLines("a\nb", max: 2) == "a\nb")
    }

    @Test func theInputAsTheDetailShowsIt() {
        #expect(toolInputText(call("Execute", ["command": "ls"])) == "$ ls")
        #expect(toolInputText(call("Read", ["file_path": "/a"])) == "/a")
        #expect(toolInputText(call("t", ["b": 1, "a": ["x"]])) == "{\n  \"a\": [\n    \"x\"\n  ],\n  \"b\": 1\n}")
    }

    @Test func readsAResultAsADiffOrAStatus() {
        let created = readToolResult(
            call("Create", ["content": "x\n"], result: "{\"success\":true,\"file_path\":\"a\"}"))
        #expect(created.diff?.added == 1)
        #expect(created.status == nil)
        let failed = readToolResult(call("Create", ["content": "x\n"], result: "{\"success\":false}", isError: true))
        #expect(failed.diff == nil)
        #expect(failed.status == StatusResult(success: false, message: nil))
        #expect(readToolResult(call("Read")).pending)
    }
}
