// What a Script run shows in the transcript. The Daemon's Script tool runs a
// program that calls other tools; each of those calls arrives as its own
// tool call carrying `scriptExecution`, and buildTranscript hangs it under the
// Script (or the WaitForScript that was watching the run when it was made).
// The Script's own result is what the model read: the program's text()
// output, a closing line with the run's figures, and a status as JSON.
import DroidProtocol
import Foundation

public enum ScriptStatus: String, Sendable, Hashable, CaseIterable {
    case completed, failed, running, stalled, cancelled
}

public struct ScriptRun: Sendable, Hashable {
    /// What the program said to the model with text(), as it was cut for the model.
    public var output: String = ""
    public var images: [TranscriptImage] = []
    /// Nil while the call has no result, or when the result is not a run's (a refused call).
    public var status: ScriptStatus?
    /// Why the run failed, or what the Daemon said when it refused the call.
    public var error: String?
    /// The closing line's figures: `3 calls · 86 B in sandbox · 33 B emitted (38%)`.
    public var stats: String?
    /// The log file holding every nested result whole.
    public var logPath: String?
    /// What the program returned, when it returned anything.
    public var value: String?

    public init(
        output: String = "", images: [TranscriptImage] = [], status: ScriptStatus? = nil,
        error: String? = nil, stats: String? = nil, logPath: String? = nil, value: String? = nil
    ) {
        self.output = output
        self.images = images
        self.status = status
        self.error = error
        self.stats = stats
        self.logPath = logPath
        self.value = value
    }
}

public func isScriptLifecycle(_ call: ToolCall) -> Bool {
    call.nested != nil
}

public func readScriptRun(_ call: ToolCall) -> ScriptRun {
    var run = ScriptRun(images: toolResultImages(call.result))
    var parts = textParts(call.result)
    guard let lastPart = parts.last else { return run }

    guard let envelope = parseEnvelope(lastPart) else {
        // Not a run's answer: the Daemon refused the call before it started.
        if call.result?.isError == true {
            run.status = .failed
            run.error = parts.joined(separator: "\n")
        } else {
            run.output = parts.joined(separator: "\n")
        }
        return run
    }
    parts.removeLast()
    run.status = envelope.status
    run.error = envelope.object["error"]?.stringValue
    if let result = envelope.object["result"], !result.isNull {
        run.value = result.stringValue ?? JSONText.stringify(result, indent: 2)
    } else if let path = envelope.object["resultPath"]?.stringValue {
        run.value = "Saved to \(path)"
    }

    if let closing = parts.firstIndex(where: isClosingLine) {
        let figures = readClosingLine(parts[closing].trimmed)
        run.stats = figures.stats
        run.logPath = figures.logPath
        parts.remove(at: closing)
    }
    run.output = parts.joined(separator: "\n")
    return run
}

private struct Envelope {
    var status: ScriptStatus
    var object: [String: JSONValue]
}

private func parseEnvelope(_ text: String) -> Envelope? {
    let trimmed = text.trimmed
    guard trimmed.hasPrefix("{"), trimmed.contains("\"toolCallId\""),
        case .object(let object)? = JSONText.parse(trimmed),
        let status = object["status"]?.stringValue.flatMap(ScriptStatus.init)
    else { return nil }
    return Envelope(status: status, object: object)
}

/// `[Script completed …]` or `[Script failed …]` on one line.
private func isClosingLine(_ part: String) -> Bool {
    let line = part.trimmed
    guard line.hasPrefix("[Script completed") || line.hasPrefix("[Script failed"), line.hasSuffix("]")
    else { return false }
    return !line.unicodeScalars.contains("\n")
}

// `[Script completed · 3 calls · 86 B in sandbox · 33 B emitted (38%) · retained: r1 …, r2 … · log: /path]`
private func readClosingLine(_ line: String) -> (stats: String?, logPath: String?) {
    let inner = String(line.dropFirst().dropLast())
    var fields = inner.components(separatedBy: " · ")
    fields.removeFirst()
    var logPath: String?
    var kept: [String] = []
    for field in fields {
        if field.hasPrefix("log: ") {
            logPath = String(field.dropFirst(5))
        } else if !field.hasPrefix("retained: ") {
            // The handles name results for the model to look up again; they mean nothing on screen.
            kept.append(field)
        }
    }
    return (kept.isEmpty ? nil : kept.joined(separator: " · "), logPath)
}

private func textParts(_ result: ToolResult?) -> [String] {
    switch result?.content {
    case .string(let text)?:
        return text.isEmpty ? [] : [text]
    case .array(let parts)?:
        return parts.compactMap { part in
            part["type"]?.stringValue == "text" ? part["text"]?.stringValue : nil
        }
    default:
        return []
    }
}

public struct ScriptInput: Sendable, Hashable {
    public var name: String
    public var text: String

    public init(name: String, text: String) {
        self.name = name
        self.text = text
    }
}

public struct ScriptSource: Sendable, Hashable {
    public var script: String
    /// Long literals the program reads as `inputs.<name>`, by name.
    public var inputs: [ScriptInput]

    public init(script: String, inputs: [ScriptInput]) {
        self.script = script
        self.inputs = inputs
    }
}

public func scriptSource(_ call: ToolCall) -> ScriptSource? {
    guard call.use.name == scriptTool, let script = call.use.input["script"]?.stringValue else {
        return nil
    }
    let raw = call.use.input["inputs"]?.objectValue ?? [:]
    let inputs = raw.keys.sorted().compactMap { name in
        raw[name]?.stringValue.map { ScriptInput(name: name, text: $0) }
    }
    return ScriptSource(script: script, inputs: inputs)
}

/// The tools a program calls, in the order they first appear: `Read`,
/// `Execute`, `memory_search` (an MCP tool, `tools.droi_memory.memory_search`).
public func scriptToolNames(_ script: String) -> [String] {
    let scalars = Array(script.unicodeScalars)
    var names: [String] = []
    var index = 0
    func isWord(_ s: Unicode.Scalar) -> Bool {
        s == "_" || s == "$" || (s.isASCII && (CharacterSet.alphanumerics.contains(s)))
    }
    func isStart(_ s: Unicode.Scalar) -> Bool {
        s == "_" || s == "$" || (s.isASCII && CharacterSet.letters.contains(s))
    }
    func identifier(at start: Int) -> (String, Int)? {
        guard start < scalars.count, isStart(scalars[start]) else { return nil }
        var end = start + 1
        while end < scalars.count, isWord(scalars[end]) { end += 1 }
        var text = String.UnicodeScalarView()
        text.append(contentsOf: scalars[start..<end])
        return (String(text), end)
    }
    let prefix = Array("tools.".unicodeScalars)
    while index + prefix.count <= scalars.count {
        let boundary = index == 0 || !(isWord(scalars[index - 1]) && scalars[index - 1] != "$")
        guard boundary, Array(scalars[index..<index + prefix.count]) == prefix,
            let (first, afterFirst) = identifier(at: index + prefix.count)
        else {
            index += 1
            continue
        }
        var name = first
        var end = afterFirst
        if end < scalars.count, scalars[end] == ".", let (second, afterSecond) = identifier(at: end + 1) {
            name = second
            end = afterSecond
        }
        var call = end
        while call < scalars.count, scalars[call].properties.isWhitespace { call += 1 }
        if call < scalars.count, scalars[call] == "(" {
            if !names.contains(name) { names.append(name) }
            index = call + 1
        } else {
            index += 1
        }
    }
    return names
}

/// What a Script or WaitForScript row says after its name.
public func scriptSummary(_ call: ToolCall) -> String {
    if call.use.name == waitForScriptTool {
        return call.use.input["kill"] == .bool(true) ? "stopped" : "continued"
    }
    return scriptSource(call).map { scriptToolNames($0.script).joined(separator: " · ") } ?? ""
}

/// The calls of a run of tool calls, with a Script counted as the calls it made.
public func leafCalls(_ calls: [ToolCall]) -> [ToolCall] {
    calls.flatMap { $0.nested ?? [$0] }
}
