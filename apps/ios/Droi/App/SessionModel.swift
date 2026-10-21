import Droid
import DroiKit
import DroidProtocol
import Foundation
import Observation

/// One open Session: its transcript and state as the store holds them,
/// redrawn at most every 32 ms (a 120 Hz display would otherwise draw every
/// token), and what the Session screen does to it.
@MainActor
@Observable
final class SessionModel {
    /// Older messages are read this many at a time.
    static let olderPage = 100
    static let renderInterval: Duration = .milliseconds(32)

    let id: String
    @ObservationIgnored let connection: ComputerConnection

    private(set) var entries: [TranscriptEntry] = []
    private(set) var workingState: DroidWorkingState = .idle
    private(set) var loadState: LoadState = .notLoaded
    /// The Daemon gave the Session to this Client once on this connection.
    private(set) var hasLoaded = false
    private(set) var hasOlder = false
    private(set) var queued: [QueuedMessage] = []
    private(set) var todos: [TodoItem] = []
    private(set) var modelId: String?
    private(set) var reasoningEffort: String?
    private(set) var autonomyLevel: String?
    private(set) var models: [ModelChoice] = []
    private(set) var title: String?
    private(set) var cwd: String?
    private(set) var tags: [SessionTag] = []
    private(set) var context: ContextUsage?
    private(set) var slashItems: [SlashItem] = builtinSlashItems

    var loadError: String?
    var sendError: String?
    private(set) var loadingOlder = false
    private(set) var olderError: String?

    @ObservationIgnored private var unsubscribe: (@MainActor () -> Void)?
    @ObservationIgnored private var pending: Task<Void, Never>?
    @ObservationIgnored private var lastRender = ContinuousClock.now - .seconds(1)
    @ObservationIgnored private var modelsSource: [ModelMetadata] = []
    @ObservationIgnored private var budget: Double?
    @ObservationIgnored private var estimated: Double = 0
    @ObservationIgnored private var budgetModel: String?

    init(id: String, connection: ComputerConnection) {
        self.id = id
        self.connection = connection
        unsubscribe = connection.store.subscribe { [weak self] event in
            guard let self, event.sessionID == self.id else { return }
            self.scheduleRender()
        }
        render()
    }

    func close() {
        unsubscribe?()
        unsubscribe = nil
        pending?.cancel()
    }

    private var controller: DroidController { connection.controller }
    private var session: SessionState? { connection.store.session(id) }

    // MARK: Rendering

    private func scheduleRender() {
        guard pending == nil else { return }
        let wait = Self.renderInterval - (ContinuousClock.now - lastRender)
        if wait <= .zero {
            render()
            return
        }
        pending = Task { [weak self] in
            try? await Task.sleep(for: wait)
            guard let self, !Task.isCancelled else { return }
            self.pending = nil
            self.render()
        }
    }

    private func render() {
        lastRender = .now
        guard let session else {
            loadState = connection.store.loadState(id)
            return
        }
        let next = buildTranscript(session.displayMessages)
        let reused = reuseUnchanged(entries, next)
        if reused != entries { entries = reused }
        if workingState != session.workingState { workingState = session.workingState }
        if loadState != session.loadState { loadState = session.loadState }
        if session.loadState == .loaded, !hasLoaded { hasLoaded = true }
        if hasOlder != session.hasOlderMessages { hasOlder = session.hasOlderMessages }
        if queued != session.queuedMessages { queued = session.queuedMessages }
        let nextTodos = session.todos ?? []
        if todos != nextTodos { todos = nextTodos }
        let settings = session.settings
        if modelId != settings.modelID { modelId = settings.modelID }
        if reasoningEffort != settings.reasoningEffort?.rawValue { reasoningEffort = settings.reasoningEffort?.rawValue }
        if autonomyLevel != settings.autonomyLevel?.rawValue { autonomyLevel = settings.autonomyLevel?.rawValue }
        if tags != (settings.tags ?? []) { tags = settings.tags ?? [] }
        if title != session.title { title = session.title }
        if cwd != session.cwd { cwd = session.cwd }
        if modelsSource != session.availableModels {
            modelsSource = session.availableModels
            models = toModelChoices(modelsSource.compactMap { try? JSONValue(encoding: $0) })
        }
        updateContext(session)
    }

    private func updateContext(_ session: SessionState) {
        guard let budget else { return }
        let lastCall = session.usage.lastCall.map { $0.inputTokens + $0.cacheReadTokens }
        let next = ContextUsage(lastCallTokens: lastCall, estimatedTokens: estimated, budgetTokens: budget)
        if context != next { context = next }
    }

    // MARK: Loading

    /// Loads the Session unless it is loaded or being loaded already.
    func load() async {
        loadError = nil
        let store = connection.store
        if store.loadState(id) == .loaded || controller.isSessionLoadInFlight(id) {
            render()
        } else {
            do {
                try await controller.loadSession(id)
            } catch DroidError.sessionNotFound {
                loadError = "Session not found"
            } catch is CancellationError {
                return
            } catch {
                loadError = String(describing: error)
            }
        }
        render()
        await readSlashItems()
        await readContext()
    }

    private func readSlashItems() async {
        guard let client = try? controller.client() else { return }
        async let commands = try? client.listCommands(ListCommandsParams(sessionId: id))
        async let skills = try? client.listSkills(ListSkillsParams(sessionId: id))
        let fromDaemon = DroiKit.slashItems(
            commands: await commands?.commands ?? [], skills: await skills?.skills ?? [])
        slashItems = mergeSlashItems(builtinSlashItems, fromDaemon)
    }

    func readContext() async {
        guard let client = try? controller.client() else { return }
        guard budget == nil || budgetModel != modelId else { return }
        if let breakdown = try? await client.getContextBreakdown(GetContextBreakdownParams(sessionId: id)) {
            budget = breakdown.contextBudget
            estimated = breakdown.usedTokens
            budgetModel = modelId
            if let session { updateContext(session) }
        }
    }

    /// Reads the next page of older messages into the front of the transcript.
    func loadOlder() async {
        guard hasOlder, !loadingOlder, let cursor = session?.olderMessagesCursor else { return }
        loadingOlder = true
        olderError = nil
        defer { loadingOlder = false }
        do {
            let page = try await controller.client().getSessionMessages(
                GetSessionMessagesParams(sessionId: id, limit: Double(Self.olderPage), cursor: cursor))
            connection.store.prependOlderMessages(id, page: page.messages.reversed(), hasOlder: page.hasMore)
            render()
        } catch {
            olderError = String(describing: error)
        }
    }

    // MARK: Sending

    /// Sends a message, or queues it while a turn runs (`placement`). A plain
    /// send shows at once; a queued one shows from the store's queue.
    func send(_ message: ComposerMessage) async {
        sendError = nil
        let requestID = newID()
        let images = message.images.map {
            Base64ImageSource(type: "base64", data: $0.data, mediaType: Base64ImageSourceMediaType(rawValue: $0.mediaType.rawValue))
        }
        var params = AddUserMessageParams(text: message.text, sessionId: id)
        params.messageId = newID()
        // The Daemon reads `images`, not image blocks in `content`.
        if !images.isEmpty { params.images = images }
        params.queuePlacement = message.placement
        if message.placement == nil {
            let now = Date().timeIntervalSince1970 * 1000
            let user = FactoryDroidMessage(
                id: params.messageId ?? requestID, role: .user,
                content: userContent(text: message.text, images: images), createdAt: now, updatedAt: now)
            connection.store.registerOptimisticSubmit(
                SessionStore.OptimisticSubmit(
                    sessionID: id, externalKey: requestID, userMessage: user, assistantBubbleID: newID(),
                    onError: { [weak self] error in self?.sendError = String(describing: error) }))
        }
        do {
            try await controller.addUserMessage(params, requestID: requestID)
        } catch {
            _ = connection.store.cancelOptimisticSubmit(requestID)
            sendError = String(describing: error)
        }
    }

    func cancel() async {
        do {
            try await controller.interruptSession(id)
        } catch {
            sendError = String(describing: error)
        }
    }

    func removeQueued(_ message: QueuedMessage) async {
        if message.kind.isDaemonBacked {
            do {
                _ = try await controller.client().resolveQueuedUserMessage(
                    .object(["sessionId": .string(id), "requestId": .string(message.requestID), "action": .string("delete")]))
            } catch {
                sendError = String(describing: error)
                return
            }
        }
        session?.clearQueuedMessage(message.requestID)
    }

    /// Takes a paused message back out of the queue, for the composer.
    func takeQueued(_ message: QueuedMessage) -> QueuedContent {
        session?.clearQueuedMessage(message.requestID)
        return queuedContent(message.content)
    }

    // MARK: Settings

    func setModel(_ modelId: String) async {
        var params = UpdateSessionSettingsParams(sessionId: id)
        params.modelId = modelId
        if let model = models.first(where: { $0.id == modelId }), let effort = reasoningEffort,
            !model.reasoningEfforts.isEmpty, !model.reasoningEfforts.contains(effort)
        {
            params.reasoningEffort = ReasoningEffort(rawValue: model.reasoningEfforts[0])
        }
        await update(params)
        budget = nil
        await readContext()
    }

    func setEffort(_ effort: String) async {
        var params = UpdateSessionSettingsParams(sessionId: id)
        params.reasoningEffort = ReasoningEffort(rawValue: effort)
        await update(params)
    }

    func setAutonomy(_ level: String) async {
        var params = UpdateSessionSettingsParams(sessionId: id)
        params.autonomyLevel = AutonomyLevel(rawValue: level)
        await update(params)
    }

    private func update(_ params: UpdateSessionSettingsParams) async {
        do {
            _ = try await controller.client().updateSessionSettings(params)
        } catch {
            sendError = String(describing: error)
        }
    }

    // MARK: Compaction

    /// Summarises the conversation. Returns the Session it continues in when
    /// the Daemon handed off to a new one.
    func compact(_ instructions: String?) async -> String? {
        let log = connection.compactions
        log.start(id)
        do {
            var params = CompactSessionParams(sessionId: id)
            params.customInstructions = instructions?.trimmingCharacters(in: .whitespacesAndNewlines).nonEmpty
            let result = try await controller.client().compactSession(params)
            let next = result.newSessionId
            if next != id {
                _ = try? await controller.loadSession(next)
                var settings = UpdateSessionSettingsParams(sessionId: next)
                settings.tags = continuationTags(id, inherited: tags)
                _ = try? await controller.client().updateSessionSettings(settings)
                connection.invalidateSessions()
            }
            log.finish(id, shownIn: next, removedCount: Int(result.removedCount))
            return next != id ? next : nil
        } catch {
            log.fail(id)
            sendError = String(describing: error)
            return nil
        }
    }
}

extension String {
    var nonEmpty: String? { isEmpty ? nil : self }
}
