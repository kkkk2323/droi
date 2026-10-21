// Git worktrees for new Sessions. The Daemon makes and removes them itself;
// the Client asks on `daemon.initialize_session` and shows what comes back.
import DroidProtocol

/// The worktree a Session runs in, as the Daemon lists it (gone once removed).
public struct SessionWorktree: Codable, Sendable, Hashable {
    public var path: String
    public var branch: String
    /// Nil for a checkout the Daemon adopted rather than made; nothing may treat it as ephemeral.
    public var lifecycle: WorktreeLifecycle?
    /// The main checkout the worktree was made from.
    public var repoRoot: String

    public init(path: String, branch: String, lifecycle: WorktreeLifecycle?, repoRoot: String) {
        self.path = path
        self.branch = branch
        self.lifecycle = lifecycle
        self.repoRoot = repoRoot
    }
}

/// What a new Session asks of the Daemon: a fresh worktree of its Workspace.
public struct WorktreeRequest: Sendable, Hashable {
    public var lifecycle: WorktreeLifecycle
    /// The first message; the Daemon names the new branch after its words.
    public var promptSlug: String?
    /// Branch the worktree starts from; the Workspace's current one when absent.
    public var baseBranch: String?

    public init(lifecycle: WorktreeLifecycle, promptSlug: String? = nil, baseBranch: String? = nil) {
        self.lifecycle = lifecycle
        self.promptSlug = promptSlug
        self.baseBranch = baseBranch
    }
}

public let worktreeLifecycles: [WorktreeLifecycle] = [.ephemeral, .persistent]
public let defaultWorktreeLifecycle: WorktreeLifecycle = .ephemeral

public func isWorktreeLifecycle(_ value: String?) -> Bool {
    value == WorktreeLifecycle.ephemeral.rawValue || value == WorktreeLifecycle.persistent.rawValue
}

extension WorktreeLifecycle {
    // The wording is the Factory App's.
    public var label: String {
        switch self {
        case .ephemeral: "Ephemeral"
        case .persistent: "Persistent"
        default: rawValue
        }
    }

    public var summary: String {
        switch self {
        case .ephemeral: "Single-session worktrees that are cleaned up automatically."
        case .persistent: "Multi-session worktrees that can only be deleted manually."
        default: ""
        }
    }
}

/// Shown before archiving a Session in an ephemeral worktree.
public let archiveWorktreeWarning = "Archiving this session will delete its worktree."

public func archiveDeletesWorktree(_ worktree: SessionWorktree?) -> Bool {
    worktree?.lifecycle == .ephemeral
}

/// A creation can take a while: the Daemon makes the checkout and runs its setup first.
public let worktreeInitTimeout: Duration = .seconds(120)

/// The `daemon.initialize_session` params that ask for a worktree.
public func applyWorktree(_ request: WorktreeRequest, to params: inout InitializeSessionParams) {
    params.worktree = true
    params.worktreeLifecycle = request.lifecycle
    if let slug = request.promptSlug?.trimmed, !slug.isEmpty { params.worktreePromptSlug = slug }
    if let base = request.baseBranch, !base.isEmpty {
        params.worktreeBaseBranch = base
        params.worktreeBranchMode = .copy
    }
}
