// Archiving from the session list. A Session in a Workspace is archived on its
// own. A Scratch Session is a conversation of its own (ADR 0008): its whole
// compaction chain goes, and its folder with it to the Trash; unarchiving
// brings the folder back first, so the Sessions open again.
public protocol ArchiveActions: Sendable {
    func archiveSession(_ sessionId: String) async throws
    func unarchiveSession(_ sessionId: String) async throws
    func trashScratchWorkspace(_ path: String) async throws
    func restoreScratchWorkspace(_ path: String) async throws
}

public func archiveConversation(
    _ actions: some ArchiveActions, listed: [SessionSummary], session: SessionSummary
) async throws {
    guard isScratchSession(session) else {
        try await actions.archiveSession(session.sessionId)
        return
    }
    for each in [session] + continuationChain(listed, of: session) where each.archivedAt == nil {
        try await actions.archiveSession(each.sessionId)
    }
    if let cwd = session.cwd { try await actions.trashScratchWorkspace(cwd) }
}

public func unarchiveConversation(
    _ actions: some ArchiveActions, listed: [SessionSummary], session: SessionSummary
) async throws {
    guard isScratchSession(session) else {
        try await actions.unarchiveSession(session.sessionId)
        return
    }
    if let cwd = session.cwd { try await actions.restoreScratchWorkspace(cwd) }
    for each in [session] + continuationChain(listed, of: session) {
        try await actions.unarchiveSession(each.sessionId)
    }
}
