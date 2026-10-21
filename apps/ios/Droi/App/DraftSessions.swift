import Droid
import DroiKit
import DroidProtocol
import Foundation

/// What a new Session starts with; nil leaves it to the Session defaults.
struct NewSessionSettings: Equatable {
    var modelId: String?
    var reasoningEffort: String?
    var autonomyLevel: String?
    /// Fixed for the Session's lifetime.
    var toolMode: ToolMode?
}

/// The first message typed on the New session page, sent once its Session opens.
struct PendingPrompt {
    var text: String
    var images: [ImageAttachment]
}

/// The New session page's Draft Session: opened as soon as the Workspace is
/// known, because the Daemon lists commands and skills per Session only and
/// "/" wants them before the first send. It carries the draft tag, which keeps
/// it out of every list; the first send takes it over. A connection holds at
/// most one; a draft in a Scratch Workspace takes its folder along when closed.
@MainActor
final class DraftSessions {
    struct Opened: Equatable {
        var sessionId: String
        /// What the Daemon gave the draft; a Session keeps it for life.
        var toolMode: ToolMode?
    }

    private struct Draft {
        var path: String
        var tags: [SessionTag]
        var requested: ToolMode?
        var task: Task<Opened?, Never>
        var settled: Opened??
    }

    private unowned let connection: ComputerConnection
    private var current: Draft?

    init(connection: ComputerConnection) { self.connection = connection }

    /// The Scratch folder of the current draft, to open again.
    var scratchFolder: String? {
        guard let current, isScratch(current.tags) else { return nil }
        return current.path
    }

    /// The draft for this Workspace, opened (or opened again) when there is none usable.
    func draft(path: String, tags: [SessionTag], requested: ToolMode?) async -> Opened? {
        if let current {
            // A dropped connection marks every Session not loaded; the Daemon may have restarted without it.
            let lost = current.settled.flatMap { $0 }.map { connection.store.loadState($0.sessionId) != .loaded } ?? false
            if current.path == path, current.requested == requested, !lost { return await current.task.value }
            // A lost draft opens again in the same folder, so that folder stays.
            discard(keepFolder: current.path == path)
        }
        let connection = self.connection
        var draftTags = [SessionTag(name: draftTag)]
        draftTags += tags
        let task = Task<Opened?, Never> {
            guard let result = try? await connection.openSession(
                path, settings: NewSessionSettings(toolMode: requested), tags: draftTags)
            else { return nil }
            return Opened(sessionId: result.sessionId, toolMode: result.toolMode)
        }
        current = Draft(path: path, tags: tags, requested: requested, task: task)
        let opened = await task.value
        if self.current?.path == path, self.current?.requested == requested { self.current?.settled = .some(opened) }
        return opened
    }

    /// Closes the draft: the page was left without sending.
    func close() { discard(keepFolder: false) }

    private func discard(keepFolder: Bool) {
        guard let draft = current else { return }
        current = nil
        let connection = self.connection
        Task {
            if let opened = await draft.task.value {
                try? await connection.controller.closeSession(CloseSessionParams(sessionId: opened.sessionId))
                connection.controller.forget(opened.sessionId)
            }
            if !keepFolder, isScratch(draft.tags) { try? await connection.gateway?.trashScratchWorkspace(draft.path) }
        }
    }

    /// Turns the draft into an ordinary Session with these settings. Nil when
    /// there is no usable one (another Workspace, or another tool mode, which
    /// the Daemon cannot change) and the caller should create the Session.
    func take(path: String, settings: NewSessionSettings) async -> String? {
        guard let draft = current else { return nil }
        guard draft.path == path, let settled = draft.settled, let opened = settled,
            settings.toolMode == nil || settings.toolMode == opened.toolMode
        else {
            discard(keepFolder: draft.path == path)
            return nil
        }
        current = nil
        var params = UpdateSessionSettingsParams(sessionId: opened.sessionId)
        params.tags = draft.tags
        params.modelId = settings.modelId
        params.reasoningEffort = settings.reasoningEffort.map(ReasoningEffort.init(rawValue:))
        params.autonomyLevel = settings.autonomyLevel.map(AutonomyLevel.init(rawValue:))
        do {
            _ = try await connection.controller.client().updateSessionSettings(params)
        } catch {
            current = draft
            discard(keepFolder: true)
            return nil
        }
        return opened.sessionId
    }
}

struct OpenedSession {
    var sessionId: String
    var toolMode: ToolMode?
    var worktree: SessionWorktree?
}

struct NewSessionError: Error, LocalizedError {
    var message: String
    var errorDescription: String? { message }
}

extension ComputerConnection {
    /// Checks the folder with the Daemon and creates a Session there.
    func openSession(
        _ path: String, settings: NewSessionSettings = NewSessionSettings(), tags: [SessionTag] = [],
        worktree: WorktreeRequest? = nil
    ) async throws -> OpenedSession {
        let client = try controller.client()
        let check = try await client.validateWorkingDirectory(ValidateWorkingDirectoryParams(workingDirectory: path))
        guard check.isValid else { throw NewSessionError(message: check.error ?? "\(path) is not a usable directory.") }
        let cwd = check.resolvedPath ?? path
        var params = InitializeSessionParams(machineId: "", cwd: cwd, token: "")
        params.modelId = settings.modelId
        params.reasoningEffort = settings.reasoningEffort.map(ReasoningEffort.init(rawValue:))
        params.autonomyLevel = settings.autonomyLevel.map(AutonomyLevel.init(rawValue:))
        params.toolExecutionMode = settings.toolMode?.executionMode
        if !tags.isEmpty { params.tags = tags }
        if let worktree { applyWorktree(worktree, to: &params) }
        let result = try await controller.initializeSession(params)
        var made: SessionWorktree?
        // The Daemon ignores the request outside a Git repository.
        if let info = result.worktree {
            made = SessionWorktree(
                path: info.path, branch: info.branch, lifecycle: info.lifecycle ?? worktree?.lifecycle,
                repoRoot: info.repoRoot ?? cwd)
        }
        return OpenedSession(sessionId: result.sessionId, toolMode: ToolMode(result.settings.toolExecutionMode), worktree: made)
    }
}
