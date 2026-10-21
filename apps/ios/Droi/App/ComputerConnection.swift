import Droid
import DroiKit
import DroidProtocol
import Foundation
import Observation
import os

/// The connection to one Paired Computer: the pairing check, the Daemon
/// controller behind its Gateway, the Session list, and what every Session on
/// it is doing. One lives while its computer is selected.
@MainActor
@Observable
final class ComputerConnection {
    enum State: Equatable {
        case connecting
        case connected
        case reconnecting
        /// No Pairing Token, or one the computer rejected (it was reset there).
        case unpaired(rejected: Bool)
        /// `initial` until the first connection succeeded.
        case unreachable(detail: String, initial: Bool)

        var label: String {
            switch self {
            case .connecting: "Connecting…"
            case .connected: "Connected"
            case .reconnecting: "Reconnecting…"
            case .unpaired: "Not paired"
            case .unreachable: "Not reachable"
            }
        }

        var isStartingUp: Bool {
            switch self {
            case .connecting: true
            case .unreachable(_, let initial): initial
            default: false
            }
        }
    }

    /// The pairing check runs again this often while the computer is away.
    static let recoveryPoll: Duration = .seconds(2)
    /// The Daemon's default page of 100 cut long Sessions short on every
    /// reopen; 400 is what the Factory app loads.
    static let loadedMessageLimit = 400

    let computer: PairedComputer
    let token: String?
    let gateway: GatewayClient?
    @ObservationIgnored let controller: DroidController
    @ObservationIgnored let preferences: Preferences
    @ObservationIgnored let alerts: AlertPlayer
    let compactions = CompactionLog()

    private(set) var state: State = .connecting
    /// The computer's `/meta`, for the version notice.
    private(set) var meta: GatewayMeta?

    // MARK: Session list

    /// The listed Sessions, newest first; the cached list until the first read.
    private(set) var listed: [SessionSummary]
    /// The list was read on this connection.
    private(set) var listLive = false
    private(set) var listError: String?
    private(set) var hasOlderSessions = false
    private(set) var loadingOlderSessions = false
    private(set) var announced: [AnnouncedSubagent] = []
    /// When this Client last saw each Session working, in Unix seconds.
    private(set) var activeAt: [String: Double] = [:]
    /// The working state the Daemon reported for Sessions not loaded here.
    private(set) var reported: [String: DroidWorkingState] = [:]
    /// Sessions the Daemon does not know.
    private(set) var notFound: Set<String> = []
    /// Sessions that finished or need input while not on screen.
    private(set) var unread: Set<String> = []
    /// Moves when a loaded Session's working or load state changes.
    private(set) var activityRevision = 0
    /// Moves when a permission or question arrives or is answered.
    private(set) var promptRevision = 0

    /// The Session on screen: it raises no alert and stays read.
    var onScreen: String? {
        didSet { if let onScreen { unread.remove(onScreen) } }
    }

    @ObservationIgnored private var nextCursor: Double?
    @ObservationIgnored private var everConnected = false
    @ObservationIgnored private var suspended = false
    @ObservationIgnored private var disposed = false
    @ObservationIgnored private var connectTask: Task<Void, Never>?
    @ObservationIgnored private var recoveryTask: Task<Void, Never>?
    @ObservationIgnored private var refreshTask: Task<Void, Never>?
    @ObservationIgnored private var unsubscribe: [@MainActor () -> Void] = []
    @ObservationIgnored private var tracker = AlertTracker()
    @ObservationIgnored private let log = Logger(subsystem: "com.kkkk2323.droi", category: "connection")

    init(computer: PairedComputer, token: String?, preferences: Preferences, alerts: AlertPlayer) {
        self.computer = computer
        self.token = token
        self.preferences = preferences
        self.alerts = alerts
        listed = preferences.cachedSessions(of: computer.id)
        let address = URL(string: computer.address)
        gateway = token.flatMap { token in address.map { GatewayClient(gateway: $0, pairingToken: token) } }
        let url = address.flatMap { a in token.map { Gateway.daemonURL(a, pairingToken: $0) } }
            ?? URL(string: "ws://invalid.invalid")!
        var config = DroidController.Config(url: url) { .apiKey(Gateway.apiKeyPlaceholder) }
        config.surface = "droi-phone"
        config.connectTimeout = .seconds(5)
        config.requestTimeout = .seconds(30)
        // The pairing check already found the Gateway; the recovery poll does
        // the waiting when it goes away.
        config.maxConnectAttempts = 3
        config.maxReconnectAttempts = 3
        config.reconnectDelay = .seconds(1)
        config.maxReconnectDelay = .seconds(10)
        config.reconnectBackoff = 1.5
        config.defaultMessageLimit = Self.loadedMessageLimit
        controller = DroidController(config: config)
        unsubscribe.append(controller.subscribe { [weak self] event in self?.handle(event) })
        unsubscribe.append(controller.store.subscribe { [weak self] event in self?.handle(event) })
        compactionDone = compactions.onDone { [weak self] done in self?.raise(done.sessionId, .completion) }
    }

    @ObservationIgnored private var compactionDone: (() -> Void)?

    var store: SessionStore { controller.store }

    // MARK: Lifecycle

    func start() {
        guard gateway != nil else {
            state = .unpaired(rejected: false)
            return
        }
        connect()
    }

    /// The app went to the background: the socket goes, what is on screen stays.
    func suspend() {
        guard !disposed, !suspended else { return }
        suspended = true
        recoveryTask?.cancel()
        recoveryTask = nil
        connectTask?.cancel()
        connectTask = nil
        if case .unpaired = state { return }
        if state == .connected { state = .reconnecting }
        controller.suspend()
    }

    /// Back in the foreground: the pairing is checked again first, as the
    /// token may have been reset meanwhile.
    func resume() {
        guard !disposed, suspended else { return }
        suspended = false
        if case .unpaired = state { return }
        connect()
    }

    func dispose() {
        disposed = true
        connectTask?.cancel()
        recoveryTask?.cancel()
        refreshTask?.cancel()
        for stop in unsubscribe { stop() }
        unsubscribe = []
        for model in models.values { model.close() }
        models = [:]
        compactionDone?()
        controller.close()
    }

    /// Pairs again on the same computer after its token was reset.
    var isUnpaired: Bool {
        if case .unpaired = state { return true }
        return false
    }

    private func connect() {
        guard !disposed, !suspended, let gateway else { return }
        connectTask?.cancel()
        connectTask = Task { [weak self] in
            let check = await gateway.checkPairing()
            guard let self, !Task.isCancelled, !self.disposed, !self.suspended else { return }
            self.log.info("pairing check: \(String(describing: check), privacy: .public)")
            switch check {
            case .rejected:
                self.state = .unpaired(rejected: true)
            case .unreachable(let detail):
                self.state = .unreachable(detail: detail, initial: !self.everConnected)
                self.scheduleRecovery()
            case .paired:
                do {
                    if self.controller.isSuspended {
                        try await self.controller.resume()
                    } else {
                        try await self.controller.connect()
                    }
                } catch {
                    guard !Task.isCancelled, !self.disposed, !self.suspended else { return }
                    self.state = .unreachable(detail: String(describing: error), initial: !self.everConnected)
                    self.scheduleRecovery()
                }
            }
        }
    }

    private func scheduleRecovery() {
        guard recoveryTask == nil, !disposed, !suspended else { return }
        recoveryTask = Task { [weak self] in
            try? await Task.sleep(for: Self.recoveryPoll)
            guard let self, !Task.isCancelled else { return }
            self.recoveryTask = nil
            self.connect()
        }
    }

    // MARK: Events

    private func handle(_ event: DroidController.Event) {
        switch event {
        case .statusChanged(let status):
            log.info(
                "status connected=\(status.connected) reconnecting=\(status.reconnecting) failure=\(String(describing: status.failure), privacy: .public)"
            )
            if status.connected {
                everConnected = true
                state = .connected
                recoveryTask?.cancel()
                recoveryTask = nil
                connected()
            } else if suspended || disposed {
                return
            } else if status.reconnecting {
                state = .reconnecting
            } else if state == .connected || state == .reconnecting {
                // The SDK gave up; keep the banner and poll the Gateway.
                state = .reconnecting
                scheduleRecovery()
            }
        case .sessionNotification(let id, let notification):
            switch notification {
            case .droidWorkingStateChanged(let change):
                reported[id] = change.newState
                if change.newState != .idle { activeAt[id] = Date().timeIntervalSince1970 }
            case .sessionTitleUpdated(let update):
                if let index = listed.firstIndex(where: { $0.sessionId == id }) { listed[index].title = update.title }
                invalidateSessions()
            case .childSessionAvailable(let child):
                if !announced.contains(where: { $0.sessionId == child.childSessionId }) {
                    announced.append(
                        AnnouncedSubagent(
                            sessionId: child.childSessionId, callingSessionId: id,
                            callingToolUseId: child.toolUseId ?? "", subagentType: child.subagentType ?? "",
                            description: child.description ?? "", cwd: nil,
                            announcedAt: Date().timeIntervalSince1970))
                }
                invalidateSessions()
            default: break
            }
        case .daemonNotification(let method, _):
            switch method {
            case DaemonNotification.sessionArchiveStateChanged, DaemonNotification.worktreeRemoved,
                DaemonNotification.worktreeBranchChanged:
                invalidateSessions()
            default: break
            }
        case .permissionRequested, .permissionResolved, .askUserRequested, .askUserResolved, .sessionLoaded:
            promptRevision += 1
        case .sessionNotFound(let id):
            notFound.insert(id)
        default:
            break
        }
    }

    private func handle(_ event: StoreEvent) {
        switch event.kind {
        case .loadStateChanged:
            if event.loadState != .loaded { tracker.forget(event.sessionID) }
            activityRevision += 1
        case .workingStateChanged:
            activityRevision += 1
            guard let session = store.session(event.sessionID), session.loadState == .loaded,
                session.callingSessionID == nil
            else { return }
            if session.workingState != .idle { activeAt[event.sessionID] = Date().timeIntervalSince1970 }
            if let alert = tracker.update(event.sessionID, workingState: session.workingState) {
                raise(event.sessionID, alert)
            }
        case .subagentSummaryUpdated:
            activityRevision += 1
            invalidateSessions()
        default:
            break
        }
    }

    private func raise(_ sessionId: String, _ alert: AlertEvent) {
        guard sessionId != onScreen else { return }
        unread.insert(sessionId)
        alerts.play(alert, switches: preferences.alerts)
    }

    private func connected() {
        notFound = []
        refreshSessions()
        Task { [weak self] in
            guard let self, let client = try? self.controller.client() else { return }
            if let opened = try? await client.listOpenedSessions() {
                var next: [String: DroidWorkingState] = [:]
                for session in opened.sessions { next[session.sessionId] = session.workingState }
                self.reported = next
            }
        }
        Task { [weak self] in
            guard let self, let address = URL(string: self.computer.address) else { return }
            var request = URLRequest(url: Gateway.metaURL(address))
            request.cachePolicy = .reloadIgnoringLocalAndRemoteCacheData
            if let (data, response) = try? await HTTP.urlSession(request), response.statusCode == 200 {
                self.meta = try? JSONDecoder().decode(GatewayMeta.self, from: data)
            }
        }
    }

    // MARK: Session list

    /// What the list shows: the listed Sessions with announced subagents and
    /// recent activity folded in.
    var sessions: [SessionSummary] {
        withActivity(withAnnouncedSubagents(listed, announced), activeAt: activeAt)
    }

    func session(_ id: String) -> SessionSummary? { sessions.first { $0.sessionId == id } }

    /// What each Session is doing now.
    var activity: [String: SessionActivity] {
        _ = activityRevision
        var loaded: [String: DroidWorkingState?] = [:]
        for id in store.sessionIDs where store.loadState(id) == .loaded {
            loaded[id] = store.session(id)?.workingState
        }
        return sessionActivity(reported: reported, loaded: loaded, compacting: compactions.pending)
    }

    /// How each subagent's run is going.
    func subagentRun(_ sessionId: String) -> SubagentRun? {
        _ = activityRevision
        let summary = store.subagentInvocationSummary(sessionId)
        return DroiKit.subagentRun(
            status: summary?.status, toolUseCount: summary?.toolUseCount.map { Int($0) },
            durationMs: summary?.durationMs, workingState: store.session(sessionId)?.workingState)
    }

    /// Reads the list again soon; several changes at once make one read.
    func invalidateSessions() {
        refreshTask?.cancel()
        refreshTask = Task { [weak self] in
            try? await Task.sleep(for: .milliseconds(150))
            guard let self, !Task.isCancelled else { return }
            await self.readSessions()
        }
    }

    func refreshSessions() {
        refreshTask?.cancel()
        refreshTask = Task { [weak self] in await self?.readSessions() }
    }

    private func readSessions() async {
        guard let client = try? controller.client() else { return }
        do {
            let page = try await client.listAvailableSessions(
                ListAvailableSessionsParams(limit: Double(sessionPage), includeArchived: preferences.showArchived))
            let first = page.sessions.filter { !isUnlisted($0) }.map(SessionSummary.init(listed:))
            // Keep the older pages already read; the first page replaces its own range.
            let ids = Set(first.map(\.sessionId))
            let oldest = first.last?.updatedAt ?? .infinity
            let older = listLive ? listed.filter { !ids.contains($0.sessionId) && $0.updatedAt < oldest } : []
            listed = first + older
            if !listLive || older.isEmpty {
                hasOlderSessions = page.hasMore
                nextCursor = page.nextCursor
            }
            listLive = true
            listError = nil
            announced.removeAll { a in listed.contains { $0.sessionId == a.sessionId } }
            preferences.setCachedSessions(first, of: computer.id)
        } catch {
            if Task.isCancelled { return }
            listError = String(describing: error)
        }
    }

    func loadOlderSessions() async {
        guard hasOlderSessions, !loadingOlderSessions, let client = try? controller.client() else { return }
        loadingOlderSessions = true
        defer { loadingOlderSessions = false }
        do {
            let page = try await client.listAvailableSessions(
                ListAvailableSessionsParams(
                    limit: Double(sessionPage), endBefore: nextCursor ?? listed.last?.updatedAt,
                    includeArchived: preferences.showArchived))
            let ids = Set(listed.map(\.sessionId))
            listed += page.sessions.filter { !isUnlisted($0) && !ids.contains($0.sessionId) }
                .map(SessionSummary.init(listed:))
            hasOlderSessions = page.hasMore
            nextCursor = page.nextCursor
        } catch {
            listError = String(describing: error)
        }
    }

    // MARK: Session actions

    func rename(_ sessionId: String, to title: String) async throws {
        let title = title.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !title.isEmpty else { return }
        _ = try await controller.client().renameSession(RenameSessionParams(sessionId: sessionId, title: title))
        if let index = listed.firstIndex(where: { $0.sessionId == sessionId }) { listed[index].title = title }
        invalidateSessions()
    }

    func archive(_ session: SessionSummary) async throws {
        try await archiveConversation(Actions(connection: self), listed: listed, session: session)
        refreshSessions()
    }

    func unarchive(_ session: SessionSummary) async throws {
        try await unarchiveConversation(Actions(connection: self), listed: listed, session: session)
        refreshSessions()
    }

    func markRead(_ sessionId: String) { unread.remove(sessionId) }

    // MARK: Open Sessions

    @ObservationIgnored private var models: [String: SessionModel] = [:]
    @ObservationIgnored private var modelOrder: [String] = []
    /// The draft images per Session; base64 does not belong in UserDefaults,
    /// so they live as long as the connection.
    @ObservationIgnored var draftImages: [String: [ImageAttachment]] = [:]
    @ObservationIgnored lazy var drafts = DraftSessions(connection: self)
    /// The first message of a Session the New session page started.
    @ObservationIgnored var pendingPrompts: [String: PendingPrompt] = [:]

    /// A Session just created, listed before the Daemon lists it (it does
    /// once the Session is written to).
    func started(_ session: SessionSummary) {
        if !listed.contains(where: { $0.sessionId == session.sessionId }) { listed.insert(session, at: 0) }
        invalidateSessions()
    }

    /// The model of an open Session; the few last opened are kept, so going
    /// back to one does not rebuild its transcript.
    func model(for sessionId: String) -> SessionModel {
        modelOrder.removeAll { $0 == sessionId }
        modelOrder.append(sessionId)
        if let model = models[sessionId] { return model }
        let model = SessionModel(id: sessionId, connection: self)
        models[sessionId] = model
        while modelOrder.count > 6 {
            let evicted = modelOrder.removeFirst()
            models.removeValue(forKey: evicted)?.close()
        }
        return model
    }

    private struct Actions: ArchiveActions {
        let client: DroidClient?
        let gateway: GatewayClient?

        @MainActor init(connection: ComputerConnection) {
            client = try? connection.controller.client()
            gateway = connection.gateway
        }

        func archiveSession(_ sessionId: String) async throws {
            guard let client else { throw DroidError.notConnected }
            _ = try await client.archiveSession(ArchiveSessionParams(sessionId: sessionId))
        }

        func unarchiveSession(_ sessionId: String) async throws {
            guard let client else { throw DroidError.notConnected }
            _ = try await client.unarchiveSession(UnarchiveSessionParams(sessionId: sessionId))
        }

        func trashScratchWorkspace(_ path: String) async throws { try await gateway?.trashScratchWorkspace(path) }

        func restoreScratchWorkspace(_ path: String) async throws { try await gateway?.restoreScratchWorkspace(path) }
    }
}
