// What the Sessions are doing right now, for the list's activity marks:
// working, or stopped until the human answers (a permission request or a
// question). A Session this Client has loaded reports through its store; for
// the rest, the Daemon's list of open Sessions gives the state at connect and
// its working-state notifications keep it current. A `/compact` this Client
// started shows as compacting from its own log (CompactionLog), since the
// Daemon reports no state for it.
import DroidProtocol

public enum SessionActivity: String, Sendable, Hashable {
    case working
    case needsInput = "needs-input"
    case compacting
}

public func activityOf(_ workingState: DroidWorkingState?) -> SessionActivity? {
    guard let workingState, workingState != .idle else { return nil }
    if workingState == .waitingForToolConfirmation { return .needsInput }
    if workingState == .compactingConversation { return .compacting }
    return .working
}

/// Every Session's activity: what the Daemon reported, overridden by what a
/// loaded Session's own store says (it has the state after any local step),
/// with this Client's running `/compact`s on top.
public func sessionActivity(
    reported: [String: DroidWorkingState], loaded: [String: DroidWorkingState?], compacting: Set<String>
) -> [String: SessionActivity] {
    var busy: [String: SessionActivity] = [:]
    for (id, state) in reported {
        if let activity = activityOf(state) { busy[id] = activity }
    }
    for (id, state) in loaded {
        busy[id] = activityOf(state)
    }
    for id in compacting { busy[id] = .compacting }
    return busy
}

/// How many of some Sessions are busy, for a folded group's header.
public struct Busy: Sendable, Hashable {
    public var working: Int
    public var needsInput: Int
}

/// Counts the Sessions working (subagents running count as working) and the
/// ones waiting for an answer; nil when none is doing anything.
public func countBusy(
    _ sessionIds: [String], activity: [String: SessionActivity], subagentsRunning: [String: Int] = [:]
) -> Busy? {
    var working = 0
    var needsInput = 0
    for id in sessionIds {
        let doing = activity[id]
        if doing == .needsInput {
            needsInput += 1
        } else if doing != nil || (subagentsRunning[id] ?? 0) > 0 {
            working += 1
        }
    }
    return working + needsInput > 0 ? Busy(working: working, needsInput: needsInput) : nil
}
