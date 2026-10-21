// What the transcript shows for a tool call, whichever Client draws it: a
// one-line summary, the input, and the result read as a diff, a status or
// plain text, with any pictures it handed back.
import DroidProtocol
import Foundation

public struct DiffLine: Sendable, Hashable {
    public enum Kind: String, Sendable, Hashable {
        case unchanged, added, removed
    }

    public var type: Kind
    public var content: String
    /// Line numbers in the old and new file; a removed line has no new one.
    public var old: Int?
    public var new: Int?

    public init(type: Kind, content: String, old: Int?, new: Int?) {
        self.type = type
        self.content = content
        self.old = old
        self.new = new
    }
}

public struct DiffResult: Sendable, Hashable {
    public var lines: [DiffLine]
    public var added: Int
    public var removed: Int

    public init(lines: [DiffLine]) {
        self.lines = lines
        added = lines.filter { $0.type == .added }.count
        removed = lines.filter { $0.type == .removed }.count
    }
}

/// The Daemon's Edit / Create results are JSON with `diffLines`; anything
/// else (plain text, errors, other tools) is shown as it came.
public func parseDiffResult(_ text: String) -> DiffResult? {
    guard text.hasPrefix("{"), text.contains("\"diffLines\""),
        case .object(let object)? = JSONText.parse(text),
        case .array(let raw)? = object["diffLines"]
    else { return nil }
    var lines: [DiffLine] = []
    for entry in raw {
        guard case .object(let e) = entry, let type = e["type"]?.stringValue.flatMap(DiffLine.Kind.init)
        else { continue }
        let numbers = e["lineNumber"]
        lines.append(
            DiffLine(
                type: type,
                content: e["content"]?.stringValue ?? "",
                old: numbers?["old"]?.doubleValue.map { Int($0) },
                new: numbers?["new"]?.doubleValue.map { Int($0) }))
    }
    return DiffResult(lines: lines)
}

public struct StatusResult: Sendable, Hashable {
    public var success: Bool
    /// The Daemon's own words when it gave any (`message` or `error`).
    public var message: String?

    public init(success: Bool, message: String?) {
        self.success = success
        self.message = message
    }
}

private let statusKeys: Set<String> = ["success", "file_path", "path", "message", "error"]

/// Create and friends answer with a small JSON object such as
/// `{"success":true,"file_path":"..."}`. Its path is already on the row, so
/// the result reads as a status line rather than as JSON. Anything with more
/// to say is shown as it came.
public func parseStatusResult(_ text: String) -> StatusResult? {
    guard text.hasPrefix("{"), text.contains("\"success\""),
        case .object(let object)? = JSONText.parse(text),
        case .bool(let success)? = object["success"]
    else { return nil }
    let message = [object["message"], object["error"]]
        .compactMap { $0?.stringValue }
        .first { !$0.isBlank }
    if object.keys.contains(where: { !statusKeys.contains($0) }) { return nil }
    return StatusResult(success: success, message: message)
}

/// Create answers with a bare status, so the file it wrote is read from the
/// call's own input and shown as all-added lines.
public func createdFileDiff(_ call: ToolCall) -> DiffResult? {
    guard call.use.name == "Create", let content = call.use.input["content"]?.stringValue else {
        return nil
    }
    let text = content.hasSuffix("\n") ? String(content.unicodeScalars.dropLast()) : content
    let lines = text.lines().enumerated().map { index, line in
        DiffLine(type: .added, content: line, old: nil, new: index + 1)
    }
    return DiffResult(lines: lines)
}

public struct ToolName: Sendable, Hashable {
    /// The MCP Server the tool belongs to; nil for the Daemon's own tools.
    public var server: String?
    public var tool: String

    public init(server: String?, tool: String) {
        self.server = server
        self.tool = tool
    }
}

/// The Daemon names an MCP tool `<server>___<tool>`; the row shows the two apart.
public func toolDisplayName(_ name: String) -> ToolName {
    guard let at = name.range(of: "___"), at.lowerBound > name.startIndex, at.upperBound < name.endIndex
    else { return ToolName(server: nil, tool: name) }
    return ToolName(server: String(name[..<at.lowerBound]), tool: String(name[at.upperBound...]))
}

/// One piece of a row's summary: a bare value for a well-known input, else `key: value`.
public struct SummaryPart: Sendable, Hashable {
    public var key: String?
    public var value: String

    public init(key: String?, value: String) {
        self.key = key
        self.value = value
    }
}

// One input that says what the call is about, shown on its own without its name.
private let headlineKeys = ["summary", "command", "file_path", "path", "pattern", "url", "query", "skill"]
private let fallbackParts = 3
private let fallbackValueLength = 60

/// What the row says after the tool's name. A headline input (the command,
/// the file, the search) stands alone; any other tool, MCP tools above all,
/// shows its first few short inputs as `key: value` pairs so a call to
/// `memory_list` reads `scope: project` rather than nothing. The protocol
/// keeps no key order, so "first" is by key name.
public func toolSummaryParts(_ call: ToolCall) -> [SummaryPart] {
    let input = call.use.input
    for key in headlineKeys {
        if let value = input[key]?.stringValue, !value.isBlank {
            return [SummaryPart(key: nil, value: firstLine(value))]
        }
    }
    var parts: [SummaryPart] = []
    for key in input.keys.sorted() {
        if parts.count == fallbackParts { break }
        switch input[key]! {
        case .string(let value):
            if !value.isBlank {
                parts.append(SummaryPart(key: key, value: firstLine(value, max: fallbackValueLength)))
            }
        case .number, .bool:
            parts.append(SummaryPart(key: key, value: input[key]!.scalarText!))
        default:
            break
        }
    }
    return parts
}

public func toolSummary(_ call: ToolCall) -> String {
    toolSummaryParts(call)
        .map { part in part.key.map { "\($0): \(part.value)" } ?? part.value }
        .joined(separator: " · ")
}

func firstLine(_ text: String, max: Int = 120) -> String {
    let line = text.lines().first ?? ""
    let units = Array(line.utf16)
    guard units.count > max else { return line }
    return String(decoding: units.prefix(max - 3), as: UTF16.self) + "…"
}

public func truncateLines(_ text: String, max: Int) -> String {
    let lines = text.lines()
    if lines.count <= max { return text }
    return "\(lines.prefix(max).joined(separator: "\n"))\n… \(lines.count - max) more lines"
}

/// The call's input as the row's detail shows it: the command, the path, or the raw input.
public func toolInputText(_ call: ToolCall) -> String {
    let input = call.use.input
    if let command = input["command"]?.stringValue, !command.isEmpty { return "$ \(command)" }
    if let path = input["file_path"]?.stringValue ?? input["path"]?.stringValue { return path }
    return JSONText.stringify(input, indent: 2)
}

/// What a permission Prompt shows of the tool it asks about: the Daemon's
/// details (the full command, the file) first, else the tool's input.
public func permissionDetail(_ details: JSONValue?, input: [String: JSONValue]) -> String {
    if let command = details?["fullCommand"]?.stringValue { return "$ \(command)" }
    if let path = details?["filePath"]?.stringValue { return path }
    if let command = input["command"]?.stringValue { return "$ \(command)" }
    return JSONText.stringify(input)
}

public func permissionDetail(_ details: ToolConfirmationDetails?, input: [String: JSONValue]) -> String {
    permissionDetail(details.flatMap { try? JSONValue(encoding: $0) }, input: input)
}

public struct ScriptPermissionCall: Sendable, Hashable {
    /// Where the call sits in the program, 1-based.
    public var line: Int
    public var tool: String
    public var detail: String
    /// The Daemon's impact level for this one call, when it gave one (`low`, `medium`, `high`).
    public var impact: String?

    public init(line: Int, tool: String, detail: String, impact: String?) {
        self.line = line
        self.tool = tool
        self.detail = detail
        self.impact = impact
    }
}

public struct ScriptPermission: Sendable, Hashable {
    /// The highest impact among the calls.
    public var impact: String?
    public var calls: [ScriptPermissionCall]

    public init(impact: String?, calls: [ScriptPermissionCall]) {
        self.impact = impact
        self.calls = calls
    }
}

/// A Script asks once for the calls it can see in its own source, each with
/// the details its direct call would have asked with. Calls whose input is
/// only known at run time are asked about one by one as they come.
public func scriptPermission(_ details: JSONValue?) -> ScriptPermission? {
    guard details?["type"]?.stringValue == "script", case .array(let raw)? = details?["calls"] else {
        return nil
    }
    var calls: [ScriptPermissionCall] = []
    for entry in raw {
        guard case .object(let object) = entry else { continue }
        let confirmation = object["confirmation"] ?? .object([:])
        let input = object["toolInput"]?.objectValue ?? [:]
        let toolName = object["toolName"].map { $0.scalarText ?? JSONText.stringify($0) } ?? ""
        calls.append(
            ScriptPermissionCall(
                line: object["line"]?.doubleValue.map { Int($0) } ?? 0,
                tool: toolDisplayName(toolName).tool,
                detail: permissionDetail(confirmation, input: input),
                impact: confirmation["impactLevel"]?.stringValue))
    }
    return ScriptPermission(impact: details?["impactLevel"]?.stringValue, calls: calls)
}

public func scriptPermission(_ details: ToolConfirmationDetails?) -> ScriptPermission? {
    scriptPermission(details.flatMap { try? JSONValue(encoding: $0) })
}

/// What a tool cluster's header says: the one tool's name or how many ran,
/// with a Script counted as the calls it made. Pending while any call, or any
/// call a Script made, is still to answer.
public func clusterLabel(_ calls: [ToolCall]) -> (label: String, pending: Bool) {
    let leaves = leafCalls(calls)
    let pending = calls.contains { $0.result == nil } || leaves.contains { $0.result == nil }
    let only: ToolCall? = leaves.count == 1 ? leaves[0] : leaves.isEmpty ? calls.first : nil
    let what =
        only.map { toolDisplayName($0.use.name).tool } ?? (leaves.isEmpty ? "a tool" : "\(leaves.count) tools")
    return (pending ? "Running \(what)" : "Used \(what)", pending)
}

/// The result's text and how it reads, for the row and its detail.
public struct ToolResultReading: Sendable, Hashable {
    public var text: String
    public var images: [TranscriptImage]
    public var pending: Bool
    public var isError: Bool
    public var diff: DiffResult?
    public var status: StatusResult?
}

public func readToolResult(_ call: ToolCall) -> ToolResultReading {
    let text = toolResultText(call.result)
    let isError = call.result?.isError == true
    let diff = parseDiffResult(text) ?? (isError ? nil : createdFileDiff(call))
    return ToolResultReading(
        text: text,
        images: toolResultImages(call.result),
        pending: call.result == nil,
        isError: isError,
        diff: diff,
        status: diff == nil ? parseStatusResult(text) : nil)
}

/// How many lines of a result or a Script's output a row shows before "… N more lines".
public let resultPreviewLines = 40
