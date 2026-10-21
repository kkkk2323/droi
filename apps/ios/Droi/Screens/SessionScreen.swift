import Droid
import DroiKit
import DroidProtocol
import SwiftUI

/// One Session: its transcript (with the Sessions it continues), the
/// composer or the prompt waiting for an answer, and the header's tools.
struct SessionScreen: View {
    let connection: ComputerConnection
    let sessionId: String
    @Environment(AppModel.self) private var app
    @Environment(\.palette) private var palette

    var body: some View {
        let model = connection.model(for: sessionId)
        ConnectionGate(connection: connection) {
            SessionContent(connection: connection, model: model)
        }
        .background(palette.background)
        .navigationTitle(connection.session(sessionId)?.title ?? model.title ?? "")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            if !callerTrail.isEmpty {
                ToolbarItem(placement: .principal) { SubagentTitle(connection: connection, sessionId: sessionId, trail: callerTrail) }
            }
            ToolbarItem(placement: .topBarTrailing) {
                GitChangesButton(connection: connection, sessionId: sessionId, workingState: model.workingState)
            }
            .sharedBackgroundVisibility(.hidden)
            ToolbarItemGroup(placement: .topBarTrailing) {
                SubagentsButton(connection: connection, sessionId: sessionId)
                ToolsButton(connection: connection, sessionId: sessionId)
            }
        }
        .onAppear {
            connection.onScreen = sessionId
            app.preferences.setLastSession(sessionId, of: connection.computer.id)
        }
        .onDisappear {
            if connection.onScreen == sessionId { connection.onScreen = nil }
        }
        .task(id: connection.state == .connected) {
            guard connection.state == .connected else { return }
            await model.load()
            if model.hasLoaded, let prompt = connection.pendingPrompts.removeValue(forKey: sessionId) {
                await model.send(ComposerMessage(text: prompt.text, images: prompt.images))
            }
        }
    }

    private var callerTrail: [SessionRef] {
        guard let session = connection.session(sessionId) else { return [] }
        return DroiKit.callerTrail(connection.sessions, of: session)
    }
}

private struct SessionContent: View {
    let connection: ComputerConnection
    let model: SessionModel
    @Environment(AppModel.self) private var app
    @Environment(\.palette) private var palette
    @State private var text = ""
    @State private var images: [ImageAttachment] = []
    @State private var sentCount = 0
    @State private var revealed = 0
    @State private var compactError: String?

    var body: some View {
        let id = model.id
        let summary = connection.session(id)
        let chain = summary.map { continuationChain(connection.sessions, of: $0) } ?? []
        let earlier = Array(chain.prefix(revealed).reversed()).map { connection.model(for: $0.sessionId) }
        let entries = earlier.flatMap(\.entries) + model.entries
        let boundaries = Set((earlier.dropFirst().map(\.entries) + (earlier.isEmpty ? [] : [model.entries])).compactMap { $0.first?.id })
        let compacting = connection.compactions.pending.contains(id)
        let _ = connection.promptRevision
        let permissions = connection.controller.pendingPermissions(sessionID: id)
        let asks = connection.controller.pendingAskUsers(sessionID: id)
        VStack(spacing: 0) {
            Group {
                if connection.store.isNotFound(id) || connection.notFound.contains(id) {
                    centered { ThemedText("This session is not on \(connection.computer.name)'s list.", tone: .destructive, scaled: false) }
                } else if let error = model.loadError {
                    centered { ThemedText(error, tone: .destructive, scaled: false) }
                } else if !model.hasLoaded && model.entries.isEmpty {
                    centered {
                        HStack(spacing: Space.sm) {
                            Spinner(size: 13)
                            ThemedText("Loading session…", tone: .muted, scaled: false)
                        }
                    }
                } else {
                    TranscriptView(
                        entries: entries, boundaryIds: boundaries,
                        workingState: compacting ? .compactingConversation : model.workingState,
                        scrollToEndKey: sentCount
                    ) {
                        lead(chain: chain, earlier: earlier, empty: entries.isEmpty)
                    }
                    .environment(\.transcriptLinks, links)
                }
            }
            .frame(maxHeight: .infinity)
            .environment(\.textScale, CGFloat(app.preferences.textSize.scale))
            VStack(spacing: 0) {
                ComposerShelf(
                    todos: model.todos, queued: model.queued,
                    onRemove: { message in Task { await model.removeQueued(message) } },
                    onTake: take)
                if let permission = permissions.first {
                    PermissionCard(toolUses: permission.toolUses, options: permission.options) { outcome in
                        try await connection.controller.respondToPermission(
                            requestID: permission.requestID, answer: .init(selectedOption: outcome))
                    }
                    .id(permission.requestID)
                    .promptFrame(palette)
                } else if let ask = asks.first {
                    AskUserCard(questions: ask.questions) { answers in
                        try await connection.controller.respondToAskUser(
                            requestID: ask.requestID,
                            result: AskUserResult(
                                answers: answers.map {
                                    AskUserCollectedAnswer(index: Double($0.index), question: $0.question, answer: $0.answer)
                                }))
                    } onCancel: {
                        try await connection.controller.respondToAskUser(
                            requestID: ask.requestID, result: AskUserResult(cancelled: true, answers: []))
                    }
                    .id(ask.requestID)
                    .promptFrame(palette)
                } else {
                    ComposerView(
                        text: $text, images: $images, isRunning: model.workingState != .idle,
                        disabled: !model.hasLoaded, slashItems: model.slashItems,
                        error: model.sendError ?? compactError, onSend: submit,
                        onCancel: { Task { await model.cancel() } }
                    ) {
                        SessionSettingsBar(
                            models: model.models, modelId: model.modelId, reasoningEffort: model.reasoningEffort,
                            autonomyLevel: model.autonomyLevel,
                            onModel: { id in Task { await model.setModel(id) } },
                            onEffort: { e in Task { await model.setEffort(e) } },
                            onAutonomy: { a in Task { await model.setAutonomy(a) } })
                    }
                    .padding(.horizontal, Space.md)
                }
                ComposerFooter(model: model, notice: connection.compactions.notice(for: model.id))
            }
            .environment(\.textScale, CGFloat(app.preferences.textSize.scale))
        }
        .onAppear {
            text = app.preferences.draft(model.id)
            images = connection.draftImages[model.id] ?? []
        }
        .onChange(of: text) { _, value in app.preferences.setDraft(value, for: model.id) }
        .onChange(of: images) { _, value in connection.draftImages[model.id] = value.isEmpty ? nil : value }
        .task(id: revealed) {
            for earlier in chain.prefix(revealed) {
                let m = connection.model(for: earlier.sessionId)
                if !m.hasLoaded { await m.load() }
            }
        }
    }

    private var links: TranscriptLinks {
        let byToolUse = subagentsByToolUse(connection.sessions)
        return TranscriptLinks(
            open: { app.router.open(session: $0) },
            subagentSession: { subagentSessionId($0, byToolUse: byToolUse) },
            subagentRun: { connection.subagentRun($0) })
    }

    @ViewBuilder
    private func lead(chain: [SessionSummary], earlier: [SessionModel], empty: Bool) -> some View {
        let oldest = earlier.first ?? model
        if let error = model.olderError {
            ThemedText("Previous messages did not load: \(error)", size: FontSize.sm, tone: .destructive, scaled: false)
        } else if oldest.hasOlder {
            leadButton(oldest.loadingOlder ? nil : "chevron.up", "Load previous messages") {
                Task { await oldest.loadOlder() }
            }
        } else if let failed = earlier.first(where: { $0.loadError != nil }), let error = failed.loadError {
            ThemedText("Earlier messages did not load: \(error)", size: FontSize.sm, tone: .destructive, scaled: false)
        } else if earlier.contains(where: { !$0.hasLoaded }) {
            HStack(spacing: Space.sm) {
                Spinner(size: 12)
                ThemedText("Loading earlier messages…", size: FontSize.sm, tone: .muted, scaled: false)
            }
        } else if revealed < chain.count {
            leadButton("chevron.up", "Continued from \u{201C}\(chain[revealed].title)\u{201D} · Show earlier messages") {
                revealed += 1
            }
        } else if empty {
            ThemedText("What should Droid work on?", tone: .muted, scaled: false)
                .frame(maxWidth: .infinity, minHeight: 200)
        }
    }

    private func leadButton(_ icon: String?, _ title: String, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            HStack(spacing: Space.sm) {
                if let icon {
                    Image(systemName: icon).font(.system(size: 11, weight: .semibold))
                } else {
                    Spinner(size: 12)
                }
                Text(title).font(.sans(FontSize.sm)).multilineTextAlignment(.leading)
            }
            .foregroundStyle(palette.mutedForeground)
            .frame(maxWidth: .infinity, minHeight: 36, alignment: .leading)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }

    private func centered<Inner: View>(@ViewBuilder _ inner: () -> Inner) -> some View {
        inner().padding(Space.xl).frame(maxWidth: .infinity, maxHeight: .infinity)
    }

    private func submit(_ message: ComposerMessage) {
        sentCount += 1
        compactError = nil
        if message.images.isEmpty, let command = compactCommand(message.text) {
            Task {
                if let next = await model.compact(command.instructions) {
                    app.router.replaceTop(with: .session(next))
                }
            }
            return
        }
        Task { await model.send(message) }
    }

    /// A paused message taken back goes into the composer, before what is
    /// there; with a prompt showing, into the stored draft the same way.
    private func take(_ message: QueuedMessage) {
        let taken = model.takeQueued(message)
        let merged = insertTaken(taken, into: QueuedContent(text: text, images: images))
        text = merged.text
        images = merged.images
    }
}

private extension View {
    func promptFrame(_ palette: Palette) -> some View {
        padding(Space.lg)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(palette.card, in: RoundedRectangle(cornerRadius: Radius.xl + 4))
            .padding(.horizontal, Space.md)
            .accessibilityAddTraits(.updatesFrequently)
    }
}

/// Under the composer: how full the context is, and what the last `/compact` folded.
private struct ComposerFooter: View {
    let model: SessionModel
    let notice: CompactionDone?
    @Environment(\.palette) private var palette

    var body: some View {
        HStack(spacing: Space.sm) {
            if let notice {
                Image(systemName: "checkmark").font(.system(size: 11, weight: .semibold))
                Text("Compacted, \(notice.removedCount) message\(notice.removedCount == 1 ? "" : "s") summarised")
                    .font(.sans(FontSize.xs))
            }
            Spacer(minLength: 0)
            if let context = model.context {
                Text("\(Int((context.ratio * 100).rounded()))% of \(formatTokens(context.budgetTokens))")
                    .font(.mono(FontSize.xs))
                    .foregroundStyle(context.ratio > 0.85 ? palette.attention : palette.mutedForeground)
                    .accessibilityLabel("Context used")
                    .accessibilityValue("\(Int((context.ratio * 100).rounded())) percent")
            }
        }
        .foregroundStyle(palette.success)
        .padding(.horizontal, Space.lg)
        .frame(minHeight: 24)
    }
}

/// The caller trail of a subagent's Session, each step a way back.
private struct SubagentTitle: View {
    let connection: ComputerConnection
    let sessionId: String
    let trail: [SessionRef]
    @Environment(AppModel.self) private var app
    @Environment(\.palette) private var palette

    var body: some View {
        VStack(spacing: 0) {
            Text(connection.session(sessionId)?.title ?? "")
                .font(.sans(FontSize.base, .semibold)).foregroundStyle(palette.foreground).lineLimit(1)
            if let caller = trail.last {
                Button {
                    app.router.open(session: caller.sessionId)
                } label: {
                    HStack(spacing: 2) {
                        Image(systemName: "chevron.left").font(.system(size: 9, weight: .bold))
                        Text(caller.title).font(.sans(FontSize.xs)).lineLimit(1)
                    }
                    .foregroundStyle(palette.mutedForeground)
                }
                .buttonStyle(.plain)
                .accessibilityLabel("Back to \(caller.title)")
            }
        }
    }
}

/// The subagents this Session started, for going to each.
private struct SubagentsButton: View {
    let connection: ComputerConnection
    let sessionId: String
    @Environment(AppModel.self) private var app
    @Environment(\.palette) private var palette

    var body: some View {
        let subagents = subagentsOf(connection.sessions, callers: [sessionId])
        if !subagents.isEmpty {
            Menu {
                ForEach(subagents) { s in
                    Button(s.title) { app.router.open(session: s.sessionId) }
                }
            } label: {
                HStack(spacing: 2) {
                    Image(systemName: "person.2").font(.system(size: 15))
                    Text("\(subagents.count)").font(.mono(FontSize.xs))
                }
                .foregroundStyle(palette.foreground)
                .frame(minWidth: 36, minHeight: 36)
            }
            .accessibilityLabel("Subagents")
            .accessibilityValue("\(subagents.count)")
        }
    }
}
