// The Paired Computers: which computers this phone can connect to, and which
// one it is connected to now. Names and addresses sit in app storage; each
// Pairing Token sits in the keychain, never in app storage.
import Foundation

public struct PairedComputer: Codable, Sendable, Hashable, Identifiable {
    /// The computer id from the Gateway's /meta; stable across tokens and addresses.
    public var id: String
    public var name: String
    /// The Gateway's address, e.g. http://192.168.1.10:41417 or a Tailscale name.
    public var address: String

    public init(id: String, name: String, address: String) {
        self.id = id
        self.name = name
        self.address = address
    }
}

/// The keychain item holding a computer's Pairing Token.
public func tokenKey(_ computerId: String) -> String {
    "pairingToken.\(computerId)"
}

/// Adds a computer, or updates the one with the same id: its address moves to
/// the new one and the name the user may have given it stays.
public func mergeComputer(_ list: [PairedComputer], _ next: PairedComputer) -> [PairedComputer] {
    guard list.contains(where: { $0.id == next.id }) else { return list + [next] }
    return list.map { computer in
        guard computer.id == next.id else { return computer }
        var moved = computer
        moved.address = next.address
        return moved
    }
}

/// The selected Paired Computer, falling back to the first.
public func selectedComputer(_ list: [PairedComputer], selectedId: String?) -> PairedComputer? {
    list.first { $0.id == selectedId } ?? list.first
}

/// The computers and selection after forgetting one; its token, cached list
/// and last Session are the caller's to clear.
public func removingComputer(_ id: String, from list: [PairedComputer], selectedId: String?)
    -> (computers: [PairedComputer], selectedId: String?)
{
    let rest = list.filter { $0.id != id }
    return (rest, selectedId == id ? rest.first?.id : selectedId)
}

/// What was last seen of a Session in a computer's list.
public struct SessionSummaryCache: Codable, Sendable, Hashable {
    public var sessionId: String
    public var title: String
    public var cwd: String?
    public var repoRoot: String?
    /// Absent in caches written before worktrees.
    public var worktree: SessionWorktree?
    public var updatedAt: Double
    public var archivedAt: String?
    public var parentId: String?
    /// Absent in caches written before subagents were told apart.
    public var callingSessionId: String?
    public var callingToolUseId: String?

    public init(_ s: SessionSummary) {
        sessionId = s.sessionId
        title = s.title
        cwd = s.cwd
        repoRoot = s.repoRoot
        worktree = s.worktree
        updatedAt = s.updatedAt
        archivedAt = s.archivedAt
        parentId = s.parentId
        callingSessionId = s.callingSessionId
        callingToolUseId = s.callingToolUseId
    }

    public var summary: SessionSummary {
        SessionSummary(
            sessionId: sessionId, title: title, cwd: cwd, repoRoot: repoRoot, worktree: worktree, updatedAt: updatedAt,
            archivedAt: archivedAt, parentId: parentId, callingSessionId: callingSessionId,
            callingToolUseId: callingToolUseId)
    }
}
