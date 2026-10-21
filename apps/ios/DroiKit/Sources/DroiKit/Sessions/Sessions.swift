// Session list and Workspace grouping: the pure rules the Session list,
// the New session page and the compaction chain are drawn by.
import DroidProtocol
import Foundation

public struct SessionSummary: Codable, Sendable, Hashable, Identifiable {
    public var sessionId: String
    public var title: String
    public var cwd: String?
    /// Grouping key the Daemon provides for checkouts; falls back to cwd.
    public var repoRoot: String?
    /// The Daemon-managed worktree the Session runs in; nil for a checkout, or once it is removed.
    public var worktree: SessionWorktree?
    /// Unix epoch seconds.
    public var updatedAt: Double
    public var messagesCount: Int?
    public var archivedAt: String?
    public var tags: [SessionTag]
    /// The Session this one continues after a compaction.
    public var parentId: String?
    /// For a subagent: the Session whose Task tool call started it (see Subagents.swift).
    public var callingSessionId: String?
    /// For a subagent: that Task tool call's id in the calling Session.
    public var callingToolUseId: String?

    public var id: String { sessionId }

    public init(
        sessionId: String, title: String = "Untitled session", cwd: String? = nil,
        repoRoot: String? = nil, worktree: SessionWorktree? = nil, updatedAt: Double = 0,
        messagesCount: Int? = nil, archivedAt: String? = nil, tags: [SessionTag] = [],
        parentId: String? = nil, callingSessionId: String? = nil, callingToolUseId: String? = nil
    ) {
        self.sessionId = sessionId
        self.title = title
        self.cwd = cwd
        self.repoRoot = repoRoot
        self.worktree = worktree
        self.updatedAt = updatedAt
        self.messagesCount = messagesCount
        self.archivedAt = archivedAt
        self.tags = tags
        self.parentId = parentId
        self.callingSessionId = callingSessionId
        self.callingToolUseId = callingToolUseId
    }

    /// A Session as `daemon.list_available_sessions` lists it.
    public init(listed s: DaemonAvailableSessionInfo) {
        var worktree: SessionWorktree?
        if let path = s.worktree?.path, !path.isEmpty, let branch = s.worktree?.branch, !branch.isEmpty {
            worktree = SessionWorktree(
                path: path, branch: branch, lifecycle: s.worktree?.lifecycle, repoRoot: s.worktree!.repoRoot)
        }
        let title = s.title?.trimmed ?? ""
        self.init(
            sessionId: s.sessionId,
            title: title.isEmpty ? "Untitled session" : title,
            cwd: s.cwd,
            repoRoot: s.repoRoot,
            worktree: worktree,
            updatedAt: s.updatedAt,
            messagesCount: s.messagesCount.map { Int($0) },
            archivedAt: s.archivedAt,
            tags: s.tags ?? [],
            parentId: continuationParent(s.tags),
            callingSessionId: s.callingSessionId,
            callingToolUseId: s.callingToolUseId)
    }
}

/// Whether the list leaves a listed Session out: a Draft Session or a Memory Session.
public func isUnlisted(_ s: DaemonAvailableSessionInfo) -> Bool {
    isDraft(s.tags) || isMemorySession(s.tags)
}

/// Tag a compaction's child Session carries so every Client can chain it to
/// its parent; the Daemon writes the link into the session file but does not
/// list it.
public let continuesTag = "droi.continues"

public func continuationParent(_ tags: [SessionTag]?) -> String? {
    tags?.first { $0.name == continuesTag }?.metadata?["parent"]
}

/// Tags for a child Session: the parent's, minus any older link, plus the new one.
public func continuationTags(_ parentId: String, inherited: [SessionTag]) -> [SessionTag] {
    inherited.filter { $0.name != continuesTag } + [SessionTag(name: continuesTag, metadata: ["parent": parentId])]
}

/// Tag of a Draft Session. It lives in the session file, so every Client and
/// every later launch leaves the draft unlisted.
public let draftTag = "droi.draft"

public func isDraft(_ tags: [SessionTag]?) -> Bool {
    tags?.contains { $0.name == draftTag } ?? false
}

/// Tag of a Session in a Scratch Workspace (ADR 0008). A compaction's child
/// inherits it with the rest of the parent's tags.
public let scratchTag = "droi.scratch"

public func isScratch(_ tags: [SessionTag]?) -> Bool {
    tags?.contains { $0.name == scratchTag } ?? false
}

/// Memory Sessions are the Desktop Shell's own model work on the Daemon (ADR
/// 0011); every Client leaves them unlisted, as it does a Draft Session.
public let memorySessionTag = "droi.memory"

public func isMemorySession(_ tags: [SessionTag]?) -> Bool {
    tags?.contains { $0.name == memorySessionTag } ?? false
}

/// The name the Gateway gives a Scratch Workspace's folder: `/2026-09-25-a1b2c3`.
func isScratchFolder(_ cwd: String) -> Bool {
    var path = Substring(cwd)
    if path.hasSuffix("/") { path = path.dropLast() }
    guard let slash = path.lastIndex(of: "/") else { return false }
    let name = Array(path[path.index(after: slash)...].unicodeScalars)
    guard name.count == 17 else { return false }
    let digits = CharacterSet(charactersIn: "0123456789")
    let hex = CharacterSet(charactersIn: "0123456789abcdef")
    for (index, scalar) in name.enumerated() {
        switch index {
        case 4, 7, 10: if scalar != "-" { return false }
        case 0..<10: if !digits.contains(scalar) { return false }
        default: if !hex.contains(scalar) { return false }
        }
    }
    return true
}

/// A Session in a Scratch Workspace. The Daemon lists a Session's tags from a
/// file beside its transcript, and some Sessions there come without it, so the
/// folder's name counts too.
public func isScratchSession(_ session: SessionSummary) -> Bool {
    isScratch(session.tags) || session.cwd.map(isScratchFolder) == true
}

/// Group key of Recents, where every Scratch Session is listed.
public let recentsGroupKey = "droi:recents"
/// Group key of the pinned Sessions, taken out of their own groups.
public let pinnedSessionsGroupKey = "droi:pinned-sessions"

/// Drops Sessions that another listed Session continues; the chain shows as its latest link.
public func foldContinued(_ sessions: [SessionSummary]) -> [SessionSummary] {
    // A Session compacted in place was once tagged with itself; that is no fold.
    let parents = Set(sessions.compactMap { s in s.parentId.flatMap { $0 != s.sessionId ? $0 : nil } })
    return sessions.filter { !parents.contains($0.sessionId) }
}

/// The listed Sessions this one continues after each compaction, nearest first.
public func continuationChain(_ sessions: [SessionSummary], of session: SessionSummary) -> [SessionSummary] {
    let byId = Dictionary(sessions.map { ($0.sessionId, $0) }, uniquingKeysWith: { _, last in last })
    var chain: [SessionSummary] = []
    var seen: Set<String> = [session.sessionId]
    var parent = session.parentId.flatMap { byId[$0] }
    while let current = parent, !seen.contains(current.sessionId) {
        chain.append(current)
        seen.insert(current.sessionId)
        parent = current.parentId.flatMap { byId[$0] }
    }
    return chain
}

public struct WorkspaceGroup: Sendable, Hashable, Identifiable {
    public var key: String
    public var label: String
    /// The Workspace; empty for Recents, whose Sessions each have their own.
    public var path: String
    /// Recents: the Sessions in Scratch Workspaces.
    public var scratch: Bool
    public var sessions: [SessionSummary]

    public var id: String { key }
}

/// Sessions per page, the Daemon's maximum; the Factory App pages by 50.
public let sessionPage = 100

/// A subagent the Daemon has announced (`child_session_available`) to a Session
/// this Client follows. The Daemon lists a Session only once its file is on
/// disk, which comes after the announcement, and the list is next read when
/// the run ends; until then the Client knows the subagent only from this.
public struct AnnouncedSubagent: Sendable, Hashable {
    public var sessionId: String
    public var callingSessionId: String
    public var callingToolUseId: String
    public var subagentType: String
    public var description: String
    public var cwd: String?
    /// Unix epoch seconds, when this Client heard of it.
    public var announcedAt: Double

    public init(
        sessionId: String, callingSessionId: String, callingToolUseId: String, subagentType: String,
        description: String, cwd: String?, announcedAt: Double
    ) {
        self.sessionId = sessionId
        self.callingSessionId = callingSessionId
        self.callingToolUseId = callingToolUseId
        self.subagentType = subagentType
        self.description = description
        self.cwd = cwd
        self.announcedAt = announcedAt
    }
}

/// `explorer` → `Explorer`, the way the Daemon titles the subagent's Session.
public func subagentName(_ subagentType: String) -> String {
    guard let first = subagentType.first else { return "Subagent" }
    return first.uppercased() + subagentType.dropFirst()
}

/// The listed Sessions, with a row first for each announced subagent the list lacks.
public func withAnnouncedSubagents(_ listed: [SessionSummary], _ announced: [AnnouncedSubagent])
    -> [SessionSummary]
{
    let byId = Dictionary(listed.map { ($0.sessionId, $0) }, uniquingKeysWith: { first, _ in first })
    let missing = announced.filter { byId[$0.sessionId] == nil }
    if missing.isEmpty { return listed }
    let rows = missing.map { a in
        let caller = byId[a.callingSessionId]
        let name = subagentName(a.subagentType)
        return SessionSummary(
            sessionId: a.sessionId,
            title: a.description.isEmpty ? name : "\(name): \(a.description)",
            cwd: a.cwd ?? caller?.cwd,
            repoRoot: caller?.repoRoot,
            worktree: caller?.worktree,
            updatedAt: a.announcedAt,
            callingSessionId: a.callingSessionId,
            callingToolUseId: a.callingToolUseId)
    }
    return rows + listed
}

/// A listed Session's time moved up to when this Client last saw it working:
/// the list is read now and then, so a Session that starts working after a
/// read would keep its old place far down until the next one.
public func withActivity(_ sessions: [SessionSummary], activeAt: [String: Double]) -> [SessionSummary] {
    if activeAt.isEmpty { return sessions }
    return sessions.map { session in
        guard let at = activeAt[session.sessionId], at > session.updatedAt else { return session }
        var moved = session
        moved.updatedAt = at
        return moved
    }
}

public struct Pins: Sendable, Hashable {
    public var workspaces: Set<String>
    public var sessions: Set<String>

    public init(workspaces: Set<String> = [], sessions: Set<String> = []) {
        self.workspaces = workspaces
        self.sessions = sessions
    }

    public static let none = Pins()
}

/// How the Workspace groups are ordered.
public enum WorkspaceSort: String, Sendable, Hashable, CaseIterable {
    case sessions, recent, name, manual

    public var label: String {
        switch self {
        case .sessions: "Most sessions"
        case .recent: "Recently active"
        case .name: "Name"
        case .manual: "Manual"
        }
    }
}

/// How the Sessions inside a group (Recents too) are ordered.
public enum SessionSort: String, Sendable, Hashable, CaseIterable {
    case recent, created

    public var label: String {
        switch self {
        case .recent: "Recently active"
        case .created: "Created"
        }
    }
}

public struct ListOrder: Sendable, Hashable {
    public var workspaces: WorkspaceSort
    public var sessions: SessionSort
    /// Workspace keys in the order the user dragged them into; for `manual`.
    public var manual: [String]
    /// When each Session was first seen, in ms; for `created`. The Daemon lists no
    /// creation time, so the Client keeps its own (see noteFirstSeen).
    public var firstSeen: [String: Double]

    public init(
        workspaces: WorkspaceSort = .sessions, sessions: SessionSort = .recent, manual: [String] = [],
        firstSeen: [String: Double] = [:]
    ) {
        self.workspaces = workspaces
        self.sessions = sessions
        self.manual = manual
        self.firstSeen = firstSeen
    }

    public static let `default` = ListOrder()
}

/// The first-seen times with any Session not seen before added. A Session is
/// never made after its last change, so its time is the earlier of now and
/// that change: a new one lands at about its creation, and an old one the
/// Client meets for the first time keeps its place among the others.
public func noteFirstSeen(
    _ firstSeen: [String: Double], _ sessions: [SessionSummary], now: Double = Date().timeIntervalSince1970 * 1000
) -> [String: Double] {
    var next = firstSeen
    for session in sessions where next[session.sessionId] == nil {
        next[session.sessionId] = min(now, session.updatedAt * 1000)
    }
    return next
}

/// The Workspace groups in the chosen order (by default the one with the most
/// conversations first, ties broken by the newest), and inside each the
/// Sessions newest first or by when they were first seen. Pinned Workspaces and
/// Sessions come before the rest, in the same order among themselves. With
/// `pinnedApart` (the computer's sidebar) pinned Sessions instead leave their
/// groups for one of their own, first.
///
/// The default does not rank Workspaces by recency alone because the Daemon's
/// `updatedAt` is the file's modified time, which moves when a Session is
/// merely loaded. Scratch Sessions share one Recents group, always last.
public func groupByWorkspace(
    _ sessions: [SessionSummary], pins: Pins = .none, order: ListOrder = .default,
    pinnedApart: Bool = false
) -> [WorkspaceGroup] {
    var groups: [WorkspaceGroup] = []
    var groupIndex: [String: Int] = [:]
    var recents: WorkspaceGroup?
    var pinned: WorkspaceGroup?
    for session in sessions {
        if pinnedApart && pins.sessions.contains(session.sessionId) {
            if pinned == nil {
                pinned = WorkspaceGroup(
                    key: pinnedSessionsGroupKey, label: "Pinned sessions", path: "", scratch: false, sessions: [])
            }
            pinned!.sessions.append(session)
            continue
        }
        if isScratchSession(session) {
            if recents == nil {
                recents = WorkspaceGroup(key: recentsGroupKey, label: "Recents", path: "", scratch: true, sessions: [])
            }
            recents!.sessions.append(session)
            continue
        }
        let path = workspaceOf(session) ?? ""
        let key = path.isEmpty ? "(unknown)" : path
        if let index = groupIndex[key] {
            groups[index].sessions.append(session)
        } else {
            groupIndex[key] = groups.count
            groups.append(
                WorkspaceGroup(key: key, label: workspaceLabel(path), path: path, scratch: false, sessions: [session]))
        }
    }

    func created(_ session: SessionSummary) -> Double {
        order.firstSeen[session.sessionId] ?? session.updatedAt * 1000
    }
    func sortSessions(_ group: inout WorkspaceGroup) {
        group.sessions.sort { a, b in
            let pinA = pins.sessions.contains(a.sessionId)
            let pinB = pins.sessions.contains(b.sessionId)
            if pinA != pinB { return pinA }
            if order.sessions == .created, created(a) != created(b) { return created(a) > created(b) }
            return a.updatedAt > b.updatedAt
        }
    }
    for index in groups.indices { sortSessions(&groups[index]) }
    if recents != nil { sortSessions(&recents!) }
    if pinned != nil { sortSessions(&pinned!) }

    func newest(_ group: WorkspaceGroup) -> Double {
        max(0, group.sessions.map(\.updatedAt).max() ?? 0)
    }
    func conversations(_ group: WorkspaceGroup) -> Int {
        group.sessions.filter { ($0.messagesCount ?? 1) > 0 }.count
    }
    // Negative when a comes first, as JavaScript's comparators read.
    func byDefault(_ a: WorkspaceGroup, _ b: WorkspaceGroup) -> Double {
        let more = Double(conversations(b) - conversations(a))
        return more != 0 ? more : newest(b) - newest(a)
    }
    // A Workspace not placed by hand yet goes after the placed ones, in the default order.
    func place(_ group: WorkspaceGroup) -> Int {
        order.manual.firstIndex(of: group.key) ?? order.manual.count
    }
    func chosen(_ a: WorkspaceGroup, _ b: WorkspaceGroup) -> Double {
        switch order.workspaces {
        case .sessions:
            return byDefault(a, b)
        case .recent:
            let recent = newest(b) - newest(a)
            return recent != 0 ? recent : byDefault(a, b)
        case .name:
            switch a.label.localizedCompare(b.label) {
            case .orderedAscending: return -1
            case .orderedDescending: return 1
            case .orderedSame: return byDefault(a, b)
            }
        case .manual:
            let placed = place(a) - place(b)
            return placed != 0 ? Double(placed) : byDefault(a, b)
        }
    }
    groups.sort { a, b in
        let pinA = pins.workspaces.contains(a.key)
        let pinB = pins.workspaces.contains(b.key)
        if pinA != pinB { return pinA }
        return chosen(a, b) < 0
    }
    return (pinned.map { [$0] } ?? []) + groups + (recents.map { [$0] } ?? [])
}

public enum Placement: Sendable {
    case before, after
}

/// The manual order after moving one Workspace next to another, as shown now
/// (`shown` is the current order of every group's key). Starts from what is
/// shown, so the first drag keeps everything else where it was.
public func moveWorkspace(_ shown: [String], key: String, target: String, place: Placement) -> [String] {
    if key == target { return shown }
    var rest = shown.filter { $0 != key }
    guard let at = rest.firstIndex(of: target) else { return shown }
    rest.insert(key, at: place == .before ? at : at + 1)
    return rest
}

/// Sessions touched within this window always show; older ones sit behind "Show more".
public let recentWindowMs: Double = 3 * 24 * 60 * 60 * 1000
public let olderBatch = 30

/// The rows a Workspace section shows: every pinned or recent Session plus the
/// first `revealed` older ones (the list is newest first), and how many stay hidden.
public func visibleSessions(
    _ sessions: [SessionSummary], revealed: Int, now: Double = Date().timeIntervalSince1970 * 1000,
    pinned: Set<String> = []
) -> (visible: [SessionSummary], hidden: Int) {
    let cutoff = now - recentWindowMs
    var visible: [SessionSummary] = []
    var older = 0
    for session in sessions {
        if pinned.contains(session.sessionId) {
            visible.append(session)
            continue
        }
        let recent = session.updatedAt * 1000 >= cutoff
        if recent || older < revealed { visible.append(session) }
        if !recent { older += 1 }
    }
    return (visible, max(0, older - revealed))
}

/// The Workspace a Session belongs to: its main checkout, never a worktree's own folder.
public func workspaceOf(_ session: SessionSummary) -> String? {
    session.worktree?.repoRoot ?? session.repoRoot ?? session.cwd
}

public func workspaceLabel(_ path: String) -> String {
    if path.isEmpty { return "Unknown workspace" }
    var trimmed = Substring(path)
    while let last = trimmed.last, last == "/" || last == "\\" { trimmed = trimmed.dropLast() }
    let last = trimmed.split(omittingEmptySubsequences: false, whereSeparator: { $0 == "/" || $0 == "\\" }).last
    if let last, !last.isEmpty { return String(last) }
    return String(trimmed)
}

/// A Workspace the New session page offers: one per path, from the Sessions the Daemon knows.
public struct RecentWorkspace: Sendable, Hashable, Identifiable {
    public var path: String
    public var label: String
    public var lastUsedAt: Double

    public var id: String { path }

    public init(path: String, label: String, lastUsedAt: Double) {
        self.path = path
        self.label = label
        self.lastUsedAt = lastUsedAt
    }
}

/// Most recent first, one entry per Workspace path; Scratch Workspaces are left out.
public func recentWorkspaces(_ sessions: [SessionSummary]) -> [RecentWorkspace] {
    var byPath: [String: RecentWorkspace] = [:]
    var order: [String] = []
    for session in sessions {
        if isScratchSession(session) { continue }
        guard let path = workspaceOf(session), !path.isEmpty else { continue }
        if let existing = byPath[path] {
            if existing.lastUsedAt < session.updatedAt {
                byPath[path]!.lastUsedAt = session.updatedAt
            }
        } else {
            order.append(path)
            byPath[path] = RecentWorkspace(path: path, label: workspaceLabel(path), lastUsedAt: session.updatedAt)
        }
    }
    return order.map { byPath[$0]! }.sorted { $0.lastUsedAt > $1.lastUsedAt }
}

/// Where a new Session will work, as the New session page offers it: a recent
/// Workspace, a typed path, or None, which is a fresh Scratch Workspace the
/// Gateway makes on the computer (ADR 0008).
public enum WorkspacePick: Sendable, Hashable {
    /// A folder by path: a recent Workspace, or one typed in.
    case recent(path: String)
    case scratch
    case other
}

/// Until the user picks: the most recent Workspace, or None when there is none
/// yet. Nil while the session list is still on its way, so no folder is made
/// for a user who has recents.
public func defaultPick(_ recent: [RecentWorkspace]?) -> WorkspacePick? {
    guard let recent else { return nil }
    return recent.first.map { .recent(path: $0.path) } ?? .scratch
}

/// Tags a Session in a Scratch Workspace carries.
public let scratchTags = [SessionTag(name: scratchTag)]
