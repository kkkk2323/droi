// Turn the SDK's flat message list into what the transcript renders: user
// turns, assistant turns whose tool calls carry their results, and nothing
// for bare tool-result messages, which only exist to complete a tool call.
import DroidProtocol
import Foundation
import Synchronization

public struct ToolCall: Sendable, Hashable {
    public var use: ToolUse
    public var result: ToolResult?
    /// Set on a Script or WaitForScript call: the calls its run made while this
    /// call was the latest one watching it (see ScriptRuns.swift).
    public var nested: [ToolCall]?

    public init(use: ToolUse, result: ToolResult? = nil, nested: [ToolCall]? = nil) {
        self.use = use
        self.result = result
        self.nested = nested
    }
}

/// A base64 picture from a message or a tool result. The bytes are decoded
/// once, on first use, however often the row draws; equal holders compare by
/// their text, so a rebuilt transcript keeps the previous holder (see
/// `reuseUnchanged`) and with it the decoded bytes.
public final class TranscriptImage: Sendable, Hashable {
    public let mediaType: String
    public let base64: String
    private let decoded = Mutex<Data??>(nil)

    public init(mediaType: String, base64: String) {
        self.mediaType = mediaType
        self.base64 = base64
    }

    /// The image's bytes; nil when the text is not base64.
    public var data: Data? {
        decoded.withLock { cached in
            if let cached { return cached }
            let data = Data(base64Encoded: base64, options: .ignoreUnknownCharacters)
            cached = .some(data)
            return data
        }
    }

    public var dataURL: String { "data:\(mediaType);base64,\(base64)" }

    public static func == (lhs: TranscriptImage, rhs: TranscriptImage) -> Bool {
        lhs === rhs || (lhs.mediaType == rhs.mediaType && lhs.base64 == rhs.base64)
    }

    public func hash(into hasher: inout Hasher) {
        hasher.combine(mediaType)
        hasher.combine(base64.utf8.count)
    }
}

public enum TranscriptBlock: Sendable, Hashable, Identifiable {
    case text(id: String, text: String)
    case image(id: String, image: TranscriptImage)
    case thinking(id: String, text: String, durationMs: Double?)
    /// A run of tool calls with nothing said in between; rendered as one cluster.
    case tools(id: String, calls: [ToolCall])
    /// A Task call: work handed to a subagent, shown on its own (see Subagents.swift).
    case subagent(id: String, call: ToolCall)

    public var id: String {
        switch self {
        case .text(let id, _), .image(let id, _), .thinking(let id, _, _), .tools(let id, _),
            .subagent(let id, _):
            id
        }
    }

    public var isText: Bool {
        if case .text = self { return true }
        return false
    }
}

public enum TranscriptRole: String, Sendable, Hashable {
    case user
    case assistant
}

public struct TranscriptEntry: Sendable, Hashable, Identifiable {
    public var id: String
    public var role: TranscriptRole
    public var blocks: [TranscriptBlock]
    /// Milliseconds since the epoch.
    public var createdAt: Double
    public var isError: Bool

    public init(
        id: String, role: TranscriptRole, blocks: [TranscriptBlock], createdAt: Double, isError: Bool
    ) {
        self.id = id
        self.role = role
        self.blocks = blocks
        self.createdAt = createdAt
        self.isError = isError
    }
}

/// The tool that starts a subagent.
public let taskTool = "Task"
/// The tool that runs a program calling other tools, and the one that waits on a run past its first minute.
public let scriptTool = "Script"
public let waitForScriptTool = "WaitForScript"

/// The run a Script or WaitForScript call is about: the Script's own tool use id.
public func scriptRunOf(_ use: ToolUse) -> String? {
    if use.scriptExecution != nil { return nil }
    if use.name == scriptTool { return use.id }
    if use.name == waitForScriptTool, let watched = use.input["toolCallId"]?.stringValue {
        return watched
    }
    return nil
}

public func buildTranscript(_ messages: [FactoryDroidMessage]) -> [TranscriptEntry] {
    var results: [String: ToolResult] = [:]
    for message in messages where message.role == .tool {
        for case .toolResult(let result) in message.content {
            results[result.toolUseId] = result
        }
    }

    // A call a Script made goes under the latest call watching its run, so the
    // calls a WaitForScript saw show where that wait sits in the turn. Calls
    // are values here, so a watching call is tracked by where it sits.
    var watching: [String: CallPath] = [:]
    var entries: [TranscriptEntry] = []
    for message in messages {
        // A user-only message is the Daemon telling the user something (a usage
        // limit, a model swap): a system message live, a user message once
        // loaded. It reads as the Droid's, as in Factory's own app.
        let role: TranscriptRole
        if message.visibility == .userOnly && (message.role == .system || message.role == .user) {
            role = .assistant
        } else if message.role == .user {
            role = .user
        } else if message.role == .assistant {
            role = .assistant
        } else {
            continue
        }
        if message.isUserVisible == false { continue }

        // One turn arrives as several assistant messages (reasoning, tool calls,
        // text); shown as one entry so nothing splits it. Tool runs join up too.
        let merging = role == .assistant && entries.last?.role == .assistant
        var entry =
            merging
            ? entries.removeLast()
            : TranscriptEntry(
                id: message.id, role: role, blocks: [], createdAt: message.createdAt,
                isError: message.isError == true)
        let entryIndex = entries.count
        let blocksBefore = entry.blocks.count
        // A message's first tool block joins the entry's trailing cluster only
        // when merging; within one message, consecutive calls always do.
        var lastIsOwnTools = false

        for (index, block) in message.content.enumerated() {
            let id = "\(message.id):\(index)"
            switch block {
            case .text(let text):
                if !text.text.isEmpty {
                    entry.blocks.append(.text(id: id, text: text.text))
                    lastIsOwnTools = false
                }
            case .image(let image):
                if image.source.type == "base64" {
                    entry.blocks.append(
                        .image(
                            id: id,
                            image: TranscriptImage(
                                mediaType: image.source.mediaType.rawValue, base64: image.source.data)))
                    lastIsOwnTools = false
                }
            case .thinking(let thinking):
                if !thinking.thinking.isEmpty {
                    entry.blocks.append(
                        .thinking(id: id, text: thinking.thinking, durationMs: thinking.durationMs))
                    lastIsOwnTools = false
                }
            case .toolUse(let use):
                var call = ToolCall(use: use, result: results[use.id])
                if let execution = use.scriptExecution, let owner = watching[execution.outerToolUseId] {
                    appendNested(call, at: owner, in: &entries, current: &entry, currentIndex: entryIndex)
                    break
                }
                let run = scriptRunOf(use)
                if run != nil { call.nested = [] }
                if use.name == taskTool {
                    entry.blocks.append(.subagent(id: id, call: call))
                    lastIsOwnTools = false
                    break
                }
                let joins = lastIsOwnTools || (merging && entry.blocks.count == blocksBefore)
                if joins, case .tools(let clusterId, var calls)? = entry.blocks.last {
                    calls.append(call)
                    entry.blocks[entry.blocks.count - 1] = .tools(id: clusterId, calls: calls)
                } else {
                    entry.blocks.append(.tools(id: id, calls: [call]))
                }
                lastIsOwnTools = true
                if let run {
                    if case .tools(_, let calls) = entry.blocks.last {
                        watching[run] = CallPath(
                            entry: entryIndex, block: entry.blocks.count - 1, call: calls.count - 1)
                    }
                }
            default:
                break
            }
        }

        // A user message with nothing to show is a record, not a turn: the
        // Daemon persists each hook run as one (`hookEventName`, no content), and
        // a message that was only a system reminder loses its text on the way.
        if entry.blocks.count == blocksBefore {
            if merging { entries.append(entry) }
            continue
        }
        if merging {
            entry.createdAt = message.createdAt
            entry.isError = entry.isError || message.isError == true
        }
        entries.append(entry)
    }
    return entries
}

private struct CallPath {
    var entry: Int
    var block: Int
    var call: Int
}

private func appendNested(
    _ call: ToolCall, at path: CallPath, in entries: inout [TranscriptEntry],
    current: inout TranscriptEntry, currentIndex: Int
) {
    func add(to entry: inout TranscriptEntry) {
        guard case .tools(let id, var calls) = entry.blocks[path.block] else { return }
        calls[path.call].nested?.append(call)
        entry.blocks[path.block] = .tools(id: id, calls: calls)
    }
    if path.entry == currentIndex {
        add(to: &current)
    } else {
        add(to: &entries[path.entry])
    }
}

/// A freshly built transcript that keeps the previous build's entries
/// wherever nothing changed (and with them their decoded images). The store
/// rebuilds every message on each read; while a turn streams, only its last
/// entry is new. Returns `previous` itself when nothing changed at all.
public func reuseUnchanged(_ previous: [TranscriptEntry], _ next: [TranscriptEntry]) -> [TranscriptEntry] {
    if previous.isEmpty { return next }
    var byId: [String: TranscriptEntry] = [:]
    for entry in previous { byId[entry.id] = entry }
    var changed = previous.count != next.count
    let entries = next.enumerated().map { index, entry in
        if let old = byId[entry.id], old == entry {
            if !changed, previous[index].id != old.id { changed = true }
            return old
        }
        changed = true
        return entry
    }
    return changed ? entries : previous
}

public func toolResultText(_ result: ToolResult?) -> String {
    guard let content = result?.content else { return "" }
    switch content {
    case .string(let text):
        return text
    case .array(let parts):
        return parts.compactMap { part -> String? in
            guard case .object(let object) = part, let text = object["text"] else { return nil }
            return text.scalarText ?? (text.isNull ? "null" : JSONText.stringify(text))
        }.joined(separator: "\n")
    default:
        return ""
    }
}

/// The pictures a tool handed back (a Read of an image file, a screenshot).
/// Each call makes new holders; keep them with the row that shows them.
public func toolResultImages(_ result: ToolResult?) -> [TranscriptImage] {
    guard case .array(let parts) = result?.content else { return [] }
    return parts.compactMap { part in
        guard part["type"]?.stringValue == "image", let source = part["source"],
            source["type"]?.stringValue == "base64", let data = source["data"]?.stringValue
        else { return nil }
        return TranscriptImage(mediaType: source["mediaType"]?.stringValue ?? "", base64: data)
    }
}

/// A turn's time, with the day in front when it was not today.
public func formatTimestamp(
    _ ms: Double, now: Date = Date(), locale: Locale = .autoupdatingCurrent,
    timeZone: TimeZone = .autoupdatingCurrent
) -> String {
    let date = Date(timeIntervalSince1970: ms / 1000)
    var calendar = Calendar(identifier: .gregorian)
    calendar.timeZone = timeZone
    let time = Date.FormatStyle(locale: locale, calendar: calendar, timeZone: timeZone)
        .hour(.twoDigits(amPM: .abbreviated)).minute(.twoDigits)
        .format(date)
    if calendar.isDate(date, inSameDayAs: now) { return time }
    let day = Date.FormatStyle(locale: locale, calendar: calendar, timeZone: timeZone)
        .month(.abbreviated).day()
        .format(date)
    return "\(day) \(time)"
}

/// When a turn ended, and when the user's message that started it was sent.
public struct TurnEnd: Sendable, Hashable {
    public var endedAt: Double
    public var startedAt: Double?

    public init(endedAt: Double, startedAt: Double?) {
        self.endedAt = endedAt
        self.startedAt = startedAt
    }
}

/// The assistant entries that close a turn, by id. A turn closes with a reply:
/// an assistant entry whose last block is text, followed by a user message.
/// One that stops on tool calls and is followed by a user message was steered
/// mid-turn, not closed; the turn goes on in the next entry. The last entry
/// closes once the Daemon rests, however it ends (cancelled, say).
public func turnEnds(_ entries: [TranscriptEntry], running: Bool) -> [String: TurnEnd] {
    var ends: [String: TurnEnd] = [:]
    var startedAt: Double?
    for (index, entry) in entries.enumerated() {
        if entry.role == .user {
            if startedAt == nil { startedAt = entry.createdAt }
            continue
        }
        let replied = entry.blocks.last?.isText == true
        let closes = index + 1 < entries.count ? entries[index + 1].role == .user && replied : !running
        if !closes { continue }
        ends[entry.id] = TurnEnd(endedAt: entry.createdAt, startedAt: startedAt)
        startedAt = nil
    }
    return ends
}

/// `23:13`, or `23:13 · took 4m 12s` when the turn's start is known.
public func formatTurnEnd(
    _ end: TurnEnd, now: Date = Date(), locale: Locale = .autoupdatingCurrent,
    timeZone: TimeZone = .autoupdatingCurrent
) -> String {
    let time = formatTimestamp(end.endedAt, now: now, locale: locale, timeZone: timeZone)
    guard let startedAt = end.startedAt else { return time }
    let took = end.endedAt - startedAt
    return took >= 1_000 ? "\(time) · took \(formatDuration(took))" : time
}

/// `640 ms`, `12s`, `4m 12s`, `1h 03m`.
public func formatDuration(_ ms: Double) -> String {
    if ms < 1_000 { return "\(Int(ms.rounded())) ms" }
    let seconds = Int((ms / 1_000).rounded())
    if seconds < 60 { return "\(seconds)s" }
    let minutes = seconds / 60
    if minutes < 60 { return "\(minutes)m \(seconds % 60)s" }
    let rest = minutes % 60
    return "\(minutes / 60)h \(rest < 10 ? "0" : "")\(rest)m"
}

private let workingLabels: [String: String] = [
    "idle": "",
    "thinking": "Thinking",
    "streaming_assistant_message": "Responding",
    "waiting_for_tool_confirmation": "Waiting for your approval",
    "executing_tool": "Running a tool",
    "compacting_conversation": "Compacting",
]

/// What the activity row under the transcript says for a working state.
public func workingLabel(_ workingState: String) -> String {
    workingLabels[workingState] ?? workingState
}

public func workingLabel(_ workingState: DroidWorkingState) -> String {
    workingLabel(workingState.rawValue)
}

/// One row of the transcript list: a user's message whole, or one block of
/// an assistant's turn, so an earlier part of a turn stays put while a later
/// part streams.
public struct TranscriptRow: Sendable, Hashable, Identifiable {
    public var id: String
    public var entry: TranscriptEntry
    /// An assistant turn's block; nil for the user's message.
    public var block: TranscriptBlock?
    public var first: Bool
    public var last: Bool
}

/// The rows newest first, as an inverted list takes them.
public func transcriptRows(_ entries: [TranscriptEntry]) -> [TranscriptRow] {
    var rows: [TranscriptRow] = []
    for entry in entries.reversed() {
        if entry.role == .user {
            rows.append(TranscriptRow(id: entry.id, entry: entry, block: nil, first: true, last: true))
            continue
        }
        let count = entry.blocks.count
        for (index, block) in entry.blocks.enumerated().reversed() {
            rows.append(
                TranscriptRow(
                    id: block.id, entry: entry, block: block, first: index == 0, last: index == count - 1))
        }
    }
    return rows
}

/// The entry whose last block is being written: the last one, while the
/// Daemon streams an assistant message into it.
public func streamingEntryId(_ entries: [TranscriptEntry], workingState: DroidWorkingState?) -> String? {
    guard workingState == .streamingAssistantMessage, let last = entries.last, last.role == .assistant
    else { return nil }
    return last.id
}
