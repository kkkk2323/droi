import DroidProtocol
import Testing

@testable import DroiKit

private let log = "/home/dev/.factory/artifacts/scripts/s1/run-1.log"

private func run(_ name: String, _ input: [String: JSONValue], _ content: JSONValue? = nil, isError: Bool = false)
    -> ToolCall
{
    call(name, input, result: content, isError: isError, id: "run-1")
}

@Suite struct ScriptRunsTests {
    @Test func splitsWhatTheModelReadFromTheClosingLineAndTheStatus() {
        let result = readScriptRun(
            run("WaitForScript", ["toolCallId": "run-1"], [
                text("# probe workspace\nline two\n\nslept"),
                text(
                    "[Script completed · 3 calls · 86 B in sandbox · 33 B emitted (38%) · retained: r1 Read README.md, r2 Execute Sleep · log: \(log)]"
                ),
                text("{\"toolCallId\":\"run-1\",\"status\":\"completed\",\"result\":null}"),
            ]))
        #expect(
            result
                == ScriptRun(
                    output: "# probe workspace\nline two\n\nslept", status: .completed,
                    stats: "3 calls · 86 B in sandbox · 33 B emitted (38%)", logPath: log))
    }

    @Test func aRunStillGoingAnswersWithItsStatusAlone() {
        let result = readScriptRun(run("Script", ["script": ""], "{\"toolCallId\":\"run-1\",\"status\":\"running\"}"))
        #expect(result.status == .running)
        #expect(result.output == "")
        #expect(result.stats == nil)
        #expect(result.logPath == nil)
    }

    @Test func aFailedRunCarriesItsErrorAndAReturnedValueIsJSON() {
        let failed = readScriptRun(
            run("Script", ["script": ""], [
                text("[Script failed at 2:5 · 1 call completed · 0 B emitted · log: /l]"),
                text("{\"toolCallId\":\"run-1\",\"status\":\"failed\",\"error\":\"ReferenceError: x is not defined\"}"),
            ]))
        #expect(failed.status == .failed)
        #expect(failed.error == "ReferenceError: x is not defined")
        #expect(failed.stats == "1 call completed · 0 B emitted")
        #expect(failed.logPath == "/l")
        let returned = readScriptRun(
            run("Script", ["script": ""], [text("{\"toolCallId\":\"run-1\",\"status\":\"completed\",\"result\":{\"files\":2}}")]))
        #expect(returned.value == "{\n  \"files\": 2\n}")
    }

    @Test func aCallTheDaemonRefusedReadsAsAFailure() {
        let refused = readScriptRun(run("Script", ["script": ""], "Script is not available", isError: true))
        #expect(refused.status == .failed)
        #expect(refused.error == "Script is not available")
        #expect(refused.output == "")
    }

    @Test func noResultYet() {
        let pending = readScriptRun(run("Script", ["script": ""]))
        #expect(pending.status == nil)
        #expect(pending.output == "")
    }

    @Test func theProgramAndItsNamedInputs() {
        #expect(
            scriptSource(run("Script", ["script": "text(inputs.body)", "inputs": ["body": "hi", "n": 3]]))
                == ScriptSource(script: "text(inputs.body)", inputs: [ScriptInput(name: "body", text: "hi")]))
        #expect(scriptSource(run("WaitForScript", ["toolCallId": "run-1"])) == nil)
    }

    @Test func eachToolOnceInOrderMcpToolsByTheirOwnName() {
        #expect(
            scriptToolNames(
                "const a = await tools.Read({})\nawait tools.Execute({})\nawait tools.droi_memory.memory_search({})\nawait tools.Read({})"
            ) == ["Read", "Execute", "memory_search"])
        #expect(scriptToolNames("mytools.Read({}); tools.Glob ({}); tools.x.y.z()") == ["Glob"])
    }

    @Test func aScriptNamesItsToolsAWaitSaysItContinuesOrStops() {
        #expect(scriptSummary(run("Script", ["script": "await tools.Grep({}); await tools.Edit({})"])) == "Grep · Edit")
        #expect(scriptSummary(run("WaitForScript", ["toolCallId": "run-1"])) == "continued")
        #expect(scriptSummary(run("WaitForScript", ["toolCallId": "run-1", "kill": true])) == "stopped")
    }

    @Test func aScriptCountsAsTheCallsItMade() {
        let read = run("Read", [:])
        let script = call("Script", ["script": ""], nested: [read, read])
        let grep = run("Grep", [:])
        #expect(leafCalls([script, grep]) == [read, read, grep])
    }
}
