// The `/compact` runs this Client started. The Daemon does not change a
// Session's working state for one (only an automatic compaction mid-turn
// reports `compacting_conversation`), so the list's mark while it runs and
// the alert when it is done come from here, and only on the Client that
// asked. The log is one per Client; Session ids are unique across Daemons.
//
// `/compact` in older Daemons is a handoff: it summarises the conversation
// into a new Session whose file records the old one as its parent, and
// answers with the new id; the Client tags the child (continuationTags) so
// every Client chains the two. Newer Daemons compact in place and answer
// with the same id.
import Foundation
import Observation

public struct CompactionDone: Sendable, Hashable {
    public var removedCount: Int
    /// Milliseconds since the epoch.
    public var finishedAt: Double

    public init(removedCount: Int, finishedAt: Double) {
        self.removedCount = removedCount
        self.finishedAt = finishedAt
    }
}

/// One finished `/compact`: `sessionId` is the Session that was compacted, `shownIn` where the user lands.
public struct CompactionFinished: Sendable, Hashable {
    public var sessionId: String
    public var shownIn: String
    public var done: CompactionDone
}

@MainActor
@Observable
public final class CompactionLog {
    /// Sessions whose `/compact` is still running.
    public private(set) var pending: Set<String> = []
    /// The last `/compact` that finished for a Session, by the Session it left the user in.
    public private(set) var finished: [String: CompactionDone] = [:]

    @ObservationIgnored private var listeners: [UUID: @MainActor (CompactionFinished) -> Void] = [:]

    public init() {}

    public func start(_ sessionId: String) {
        pending.insert(sessionId)
    }

    /// `shownIn` is where the user lands: the Session itself, or the child of a handoff.
    public func finish(
        _ sessionId: String, shownIn: String, removedCount: Int, now: Double = Date().timeIntervalSince1970 * 1000
    ) {
        pending.remove(sessionId)
        let done = CompactionDone(removedCount: removedCount, finishedAt: now)
        finished[shownIn] = done
        let event = CompactionFinished(sessionId: sessionId, shownIn: shownIn, done: done)
        for listener in listeners.values { listener(event) }
    }

    public func fail(_ sessionId: String) {
        pending.remove(sessionId)
    }

    /// Called once each time a `/compact` finishes; the returned closure stops it.
    @discardableResult
    public func onDone(_ listener: @escaping @MainActor (CompactionFinished) -> Void) -> () -> Void {
        let key = UUID()
        listeners[key] = listener
        return { [weak self] in self?.listeners[key] = nil }
    }

    /// The `/compact` that just finished in this Session, for the notice's few seconds.
    public func notice(for sessionId: String, now: Double = Date().timeIntervalSince1970 * 1000) -> CompactionDone? {
        compactedNotice(finished[sessionId], now: now)
    }
}

/// How long the "compacted" notice stays after a `/compact` finishes, in ms.
public let compactedNoticeMs: Double = 8_000

public func compactedNotice(_ done: CompactionDone?, now: Double) -> CompactionDone? {
    guard let done, done.finishedAt + compactedNoticeMs > now else { return nil }
    return done
}

/// A message that is `/compact`, which the composer runs itself instead of
/// sending, when it carries no images.
public struct CompactCommand: Sendable, Hashable {
    /// What follows the command, trimmed; nil for none.
    public var instructions: String?
}

public func compactCommand(_ text: String) -> CompactCommand? {
    guard text.hasPrefix("/compact") else { return nil }
    let rest = Substring(text.unicodeScalars.dropFirst("/compact".unicodeScalars.count))
    if rest.isEmpty { return CompactCommand(instructions: nil) }
    guard let first = rest.unicodeScalars.first, first.properties.isWhitespace else { return nil }
    let instructions = rest.trimmingCharacters(in: .whitespacesAndNewlines)
    return CompactCommand(instructions: instructions.isEmpty ? nil : instructions)
}
