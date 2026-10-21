// Subagents: Sessions the Daemon starts for a Task tool call. The list carries
// each one's calling Session and tool call; Clients keep them out of the
// Session list and reach them from the Session that called them.
import DroidProtocol
import Foundation

public struct SessionRef: Sendable, Hashable {
    public var sessionId: String
    public var title: String

    public init(sessionId: String, title: String) {
        self.sessionId = sessionId
        self.title = title
    }
}

/// The Sessions a list shows: subagents hang off the Session that called them.
public func mainSessions(_ sessions: [SessionSummary]) -> [SessionSummary] {
    sessions.filter { $0.callingSessionId == nil }
}

/// Subagents called from any of these Sessions, newest first.
public func subagentsOf(_ sessions: [SessionSummary], callers callerIds: [String]) -> [SessionSummary] {
    let callers = Set(callerIds)
    return sessions
        .filter { $0.callingSessionId.map(callers.contains) == true }
        .sorted { $0.updatedAt > $1.updatedAt }
}

/// Every link of the compaction chain a Session belongs to, latest first.
private func compactionChain(_ sessions: [SessionSummary], _ sessionId: String) -> [String] {
    var continuedBy: [String: String] = [:]
    var parentOf: [String: String] = [:]
    for s in sessions {
        guard let parent = s.parentId else { continue }
        continuedBy[parent] = s.sessionId
        parentOf[s.sessionId] = parent
    }
    var latest = sessionId
    var seen: Set<String> = [latest]
    while let next = continuedBy[latest], !seen.contains(next) {
        latest = next
        seen.insert(latest)
    }
    var chain = [latest]
    var id = parentOf[latest]
    while let current = id, !chain.contains(current) {
        chain.append(current)
        id = parentOf[current]
    }
    return chain
}

/// The Sessions above a subagent, its main Session first; empty for a main
/// Session. A caller that has since been compacted leads to the Session that
/// continues it. A caller missing from the list still gets a crumb.
public func callerTrail(_ sessions: [SessionSummary], sessionId: String, callingSessionId: String?)
    -> [SessionRef]
{
    let byId = Dictionary(sessions.map { ($0.sessionId, $0) }, uniquingKeysWith: { _, last in last })
    var trail: [SessionRef] = []
    var seen: Set<String> = [sessionId]
    var callerId = callingSessionId
    while let id = callerId, !seen.contains(id) {
        seen.insert(id)
        let caller = byId[id]
        let latestId = compactionChain(sessions, id).first ?? id
        let latest = byId[latestId] ?? caller
        trail.insert(SessionRef(sessionId: latestId, title: latest?.title ?? "Main session"), at: 0)
        callerId = caller?.callingSessionId
    }
    return trail
}

public func callerTrail(_ sessions: [SessionSummary], of session: SessionSummary) -> [SessionRef] {
    callerTrail(sessions, sessionId: session.sessionId, callingSessionId: session.callingSessionId)
}

/// The subagents a subagent can switch to: everything its caller called, across
/// the caller's compactions, as the caller's header lists them.
public func subagentSiblings(_ sessions: [SessionSummary], of session: SessionSummary) -> [SessionSummary] {
    guard let caller = session.callingSessionId else { return [] }
    return subagentsOf(sessions, callers: compactionChain(sessions, caller))
}

/// The listed row a Session belongs to: its main Session, at the latest link
/// of that Session's compaction chain (see foldContinued).
public func listedSessionOf(_ sessions: [SessionSummary], _ sessionId: String) -> String {
    let byId = Dictionary(sessions.map { ($0.sessionId, $0) }, uniquingKeysWith: { _, last in last })
    var seen: Set<String> = []
    var id = sessionId
    while let s = byId[id], let caller = s.callingSessionId, !seen.contains(id) {
        seen.insert(id)
        id = caller
    }
    var continuedBy: [String: String] = [:]
    for s in sessions { if let parent = s.parentId { continuedBy[parent] = s.sessionId } }
    while let next = continuedBy[id], !seen.contains(id) {
        seen.insert(id)
        id = next
    }
    return id
}

/// A subagent's run as the Daemon reports it.
public typealias SubagentStatus = TaskInvocationStatus

extension TaskInvocationStatus {
    /// What a Task call is doing when it is a background run nobody here has heard back from.
    public static let launched = TaskInvocationStatus(rawValue: "launched")
}

public struct SubagentRun: Sendable, Hashable {
    public var status: SubagentStatus
    public var toolUseCount: Int?
    public var durationMs: Double?

    public init(status: SubagentStatus, toolUseCount: Int? = nil, durationMs: Double? = nil) {
        self.status = status
        self.toolUseCount = toolUseCount
        self.durationMs = durationMs
    }
}

/// The run of a subagent this Client has heard of: the Daemon's summary, or
/// running while its own Session reports work.
public func subagentRun(
    status: SubagentStatus?, toolUseCount: Int?, durationMs: Double?, workingState: DroidWorkingState?
) -> SubagentRun? {
    let fallback: SubagentStatus? = workingState.flatMap { $0 != .idle ? .running : nil }
    guard let status = status ?? fallback else { return nil }
    return SubagentRun(status: status, toolUseCount: toolUseCount, durationMs: durationMs)
}

public func isRunning(_ status: SubagentStatus?) -> Bool {
    status == .running || status == .pending
}

/// How many subagents are running under each listed row.
public func runningSubagents(_ sessions: [SessionSummary], runs: [String: SubagentRun]) -> [String: Int] {
    var counts: [String: Int] = [:]
    for s in sessions where s.callingSessionId != nil && isRunning(runs[s.sessionId]?.status) {
        counts[listedSessionOf(sessions, s.sessionId), default: 0] += 1
    }
    return counts
}

public struct TaskRequest: Sendable, Hashable {
    /// The custom droid's name as the Daemon knows it, such as `explorer`.
    public var subagentType: String
    public var description: String
    public var prompt: String

    public init(subagentType: String, description: String, prompt: String) {
        self.subagentType = subagentType
        self.description = description
        self.prompt = prompt
    }
}

public func taskRequest(_ call: ToolCall) -> TaskRequest {
    let input = call.use.input
    return TaskRequest(
        subagentType: input["subagent_type"]?.stringValue ?? "",
        description: input["description"]?.stringValue ?? "",
        prompt: input["prompt"]?.stringValue ?? "")
}

private let backgroundLaunch = "Task launched in background"

/// A background Task answers at once with the subagent's Session id.
public func launchedSessionId(_ call: ToolCall) -> String? {
    for line in toolResultText(call.result).lines() {
        guard line.hasPrefix("session_id:") else { continue }
        let value = line.dropFirst("session_id:".count).trimmingCharacters(in: .whitespaces)
        if !value.isEmpty, !value.contains(where: \.isWhitespace) { return value }
    }
    return nil
}

/// What the Task call is doing: the Daemon's own account when this Client has
/// one, otherwise what the tool result says. `launched` is a background run
/// nobody here has heard back from.
public func taskState(_ call: ToolCall, run: SubagentRun?) -> SubagentStatus {
    if call.result?.isError == true { return .failed }
    if let run { return run.status }
    guard call.result != nil else { return .running }
    return toolResultText(call.result).hasPrefix(backgroundLaunch) ? .launched : .completed
}

/// The subagent's report, once a waited-for Task has one; a launch notice is not a report.
public func taskReport(_ call: ToolCall) -> String {
    let text = toolResultText(call.result)
    return text.hasPrefix(backgroundLaunch) ? "" : text
}

/// Listed subagents by the Task call that started them.
public func subagentsByToolUse(_ sessions: [SessionSummary]) -> [String: SessionSummary] {
    var map: [String: SessionSummary] = [:]
    for s in sessions { if let id = s.callingToolUseId { map[id] = s } }
    return map
}

/// The subagent Session a Task call leads to, once it is known.
public func subagentSessionId(_ call: ToolCall, byToolUse: [String: SessionSummary]) -> String? {
    byToolUse[call.use.id]?.sessionId ?? launchedSessionId(call)
}

/// The Task calls whose subagent the list does not show, each with whether it
/// has answered yet; empty when every one is listed. When it changes, the
/// Session list is read again: a Session another process drives (the CLI,
/// say) starts subagents without any event reaching this Client.
public func unlistedTaskCalls(_ transcript: [TranscriptEntry], byToolUse: [String: SessionSummary]) -> String {
    var missing: [String] = []
    for entry in transcript {
        for case .subagent(_, let call) in entry.blocks where byToolUse[call.use.id] == nil {
            missing.append(call.use.id + (call.result != nil ? ":answered" : ""))
        }
    }
    return missing.joined(separator: ",")
}

public func formatRunDuration(_ ms: Double) -> String {
    let seconds = Int((ms / 1_000).rounded())
    if seconds < 60 { return "\(seconds)s" }
    let minutes = seconds / 60
    if minutes < 60 { return "\(minutes)m \(seconds % 60)s" }
    return "\(minutes / 60)h \(minutes % 60)m"
}
