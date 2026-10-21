import Droid
import DroiKit
import DroidProtocol
import SwiftUI

/// Starting a Session: where (a recent Workspace, None for a fresh Scratch
/// Workspace, or a typed folder), in a worktree or not, with which model, and
/// the first message, or nothing.
struct NewSessionScreen: View {
    let connection: ComputerConnection
    let workspace: String?
    let scratch: Bool

    var body: some View {
        ConnectionGate(connection: connection) {
            NewSessionForm(
                connection: connection,
                initialPick: scratch ? .scratch : workspace.map { .recent(path: $0) })
        }
        .navigationTitle("New session")
        .navigationBarTitleDisplayMode(.inline)
    }
}

private struct NewSessionForm: View {
    let connection: ComputerConnection
    let initialPick: WorkspacePick?
    @Environment(AppModel.self) private var app
    @Environment(\.palette) private var palette

    @State private var chosen: WorkspacePick?
    @State private var scratchFolder: String?
    @State private var scratchRound = 0
    @State private var scratchError: String?
    @State private var typedPath = ""
    @State private var git: ListGitBranchesResult?
    @State private var placement: Placement?
    @State private var baseBranch: (path: String, branch: String)?
    @State private var defaults: SessionDefaultsView?
    @State private var overrides = NewSessionSettings()
    @State private var draft: DraftSessions.Opened?
    @State private var slashItems: [SlashItem] = []
    @State private var creating = false
    @State private var taking = false
    @State private var error: String?
    @State private var text = ""
    @State private var images: [ImageAttachment] = []
    @State private var sheet: Sheet?

    enum Placement { case local, worktree }
    enum Sheet: String, Identifiable {
        case workspace, worktree, branches
        var id: String { rawValue }
    }

    private var known: [RecentWorkspace]? {
        connection.listLive || !connection.listed.isEmpty ? recentWorkspaces(connection.sessions) : nil
    }
    private var pick: WorkspacePick? { chosen ?? defaultPick(known) }
    private var isScratchPick: Bool { pick == .scratch }
    private var pickedPath: String? {
        if case .recent(let path) = pick { return path }
        return nil
    }
    private var workspace: String? { pickedPath ?? (isScratchPick ? scratchFolder : nil) }
    private var tags: [SessionTag] { isScratchPick ? scratchTags : [] }
    private var canWorktree: Bool { git?.isGitRepository == true }
    private var inWorktree: Bool {
        canWorktree && (placement ?? (app.preferences.worktreeByDefault ? .worktree : .local)) == .worktree
    }
    private var lifecycle: WorktreeLifecycle {
        WorktreeLifecycle(rawValue: app.preferences.worktreeLifecycle)
    }
    private var chosenBase: String? { baseBranch?.path == pickedPath ? baseBranch?.branch : nil }
    private var shownBase: String? { chosenBase ?? git?.currentBranch }
    private var toolMode: (shown: ToolMode?, requested: ToolMode?) {
        newSessionToolMode(choice: overrides.toolMode, fallback: app.preferences.toolMode, draft: draft?.toolMode)
    }
    private var settings: NewSessionSettings {
        NewSessionSettings(
            modelId: overrides.modelId ?? defaults?.modelId,
            reasoningEffort: overrides.reasoningEffort ?? defaults?.reasoningEffort,
            autonomyLevel: overrides.autonomyLevel ?? defaults?.autonomyLevel,
            toolMode: toolMode.requested)
    }

    var body: some View {
        let label = pickedPath.map { path in known?.first { $0.path == path }?.label ?? workspaceLabel(path) }
        let pickText = label.map { "\($0)?" } ?? (isScratchPick ? "Workspace: None" : "Choose a workspace")
        let placementText =
            inWorktree ? "New worktree · \(lifecycle.label)" + (shownBase.map { " · from \($0)" } ?? "") : "Work locally"
        VStack(spacing: 0) {
            ScrollView {
                VStack(spacing: Space.lg) {
                    VStack(spacing: Space.xs) {
                        ThemedText(
                            label != nil ? "What do you want to build in" : isScratchPick ? "What\u{2019}s on your mind?" : "Where should Droid work?",
                            size: FontSize.xl, weight: .medium, scaled: false
                        )
                        .multilineTextAlignment(.center)
                        .accessibilityAddTraits(.isHeader)
                        if label != nil || isScratchPick || known != nil {
                            Button { sheet = .workspace } label: {
                                HStack(spacing: Space.xs) {
                                    Text(pickText)
                                        .font(.sans(isScratchPick ? FontSize.base : FontSize.xl, .medium))
                                        .foregroundStyle(isScratchPick ? palette.mutedForeground : palette.foreground)
                                    Image(systemName: "chevron.down").font(.system(size: 13, weight: .semibold))
                                        .foregroundStyle(palette.mutedForeground)
                                }
                                .overlay(alignment: .bottom) {
                                    Rectangle().stroke(style: StrokeStyle(lineWidth: 1, dash: [3, 3]))
                                        .foregroundStyle(palette.mutedForeground).frame(height: 1).offset(y: 3)
                                }
                            }
                            .buttonStyle(.plain)
                            .accessibilityLabel("Workspace")
                            .accessibilityValue(pickText)
                        }
                    }
                    if canWorktree {
                        Button { sheet = .worktree } label: {
                            HStack(spacing: Space.xs) {
                                Image(systemName: "arrow.triangle.branch").font(.system(size: 12))
                                Text(placementText).font(.sans(FontSize.sm)).lineLimit(1)
                                Image(systemName: "chevron.down").font(.system(size: 10, weight: .semibold))
                            }
                            .foregroundStyle(palette.mutedForeground)
                            .frame(minHeight: 44)
                        }
                        .buttonStyle(.plain)
                        .accessibilityLabel("Worktree")
                        .accessibilityValue(placementText)
                    }
                    if pick == .other {
                        HStack(spacing: Space.sm) {
                            DroiTextField(
                                label: "Workspace path", text: $typedPath, placeholder: "/Users/you/projects/app", mono: true
                            ) { Task { await start(typedPath, prompt: nil) } }
                            DroiButton(
                                title: "Start", busy: creating,
                                disabled: typedPath.trimmingCharacters(in: .whitespaces).isEmpty
                            ) { Task { await start(typedPath, prompt: nil) } }
                            .frame(width: 90)
                        }
                    }
                    if creating && inWorktree {
                        ThemedText("Creating worktree…", size: FontSize.sm, tone: .muted, scaled: false)
                    }
                    if let failure = error ?? scratchError {
                        ThemedText(failure, size: FontSize.sm, tone: .destructive, scaled: false)
                            .multilineTextAlignment(.center)
                    }
                }
                .padding(Space.xl)
                .frame(maxWidth: .infinity)
                .containerRelativeFrame(.vertical, alignment: .center)
            }
            .scrollDismissesKeyboard(.interactively)
            VStack(spacing: Space.xs) {
                ComposerView(
                    text: $text, images: $images, disabled: workspace == nil || creating || taking,
                    placeholder: "Do anything…", sendLabel: "Start session", allowEmpty: true,
                    slashItems: slashItems,
                    onSend: { message in
                        if let workspace { Task { await start(workspace, prompt: PendingPrompt(text: message.text, images: message.images)) } }
                    }
                ) {
                    SessionSettingsBar(
                        models: defaults?.models ?? [], modelId: settings.modelId,
                        reasoningEffort: settings.reasoningEffort, autonomyLevel: settings.autonomyLevel,
                        toolMode: toolMode.shown ?? .directOnly,
                        onModel: pickModel,
                        onEffort: { overrides.reasoningEffort = $0 },
                        onAutonomy: { overrides.autonomyLevel = $0 },
                        onToolMode: { overrides.toolMode = $0 })
                }
                .padding(.horizontal, Space.md)
                .environment(\.textScale, CGFloat(app.preferences.textSize.scale))
                if let workspace {
                    Text(workspace).font(.sans(FontSize.xs)).foregroundStyle(palette.mutedForeground).lineLimit(1)
                        .padding(.horizontal, Space.xl)
                }
            }
            .padding(.bottom, Space.sm)
        }
        .background(palette.background)
        .accessibilityElement(children: .contain)
        .accessibilityLabel("New session")
        .sheet(item: $sheet) { which in
            Group {
                switch which {
                case .workspace: workspaceSheet
                case .worktree: worktreeSheet
                case .branches: branchesSheet
                }
            }
            .environment(\.palette, palette)
            .presentationDetents([.medium, .large])
            .presentationDragIndicator(.visible)
        }
        .task(id: connection.state == .connected) { await readDefaults() }
        .task(id: ScratchKey(scratch: isScratchPick, round: scratchRound, connected: connection.state == .connected)) {
            await makeScratchFolder()
        }
        .task(id: pickedPath) { await readGit() }
        .task(id: DraftKey(path: workspace, mode: toolMode.requested, connected: connection.state == .connected)) {
            await openDraft()
        }
        .onDisappear {
            if !taking { connection.drafts.close() }
        }
    }

    private struct ScratchKey: Equatable { var scratch: Bool; var round: Int; var connected: Bool }
    private struct DraftKey: Equatable { var path: String?; var mode: ToolMode?; var connected: Bool }

    // MARK: Sheets

    private var workspaceSheet: some View {
        RadioSheet(
            title: "Workspace",
            choices: [("none", "None", nil)] + (known ?? []).map { ($0.path, $0.label, Optional($0.path)) }
                + [("other", "Other folder…", nil)],
            selected: isScratchPick ? "none" : pickedPath ?? (pick == .other ? "other" : nil)
        ) { value in
            sheet = nil
            switch value {
            case "none": choose(.scratch)
            case "other": choose(.other)
            default: choose(.recent(path: value))
            }
        }
    }

    private var worktreeSheet: some View {
        @Bindable var prefs = app.preferences
        return NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: Space.lg) {
                    radio("Work locally", "In the Workspace's own folder.", on: !inWorktree) { placement = .local }
                    radio(
                        "New worktree", "A separate checkout on a new branch, so the Workspace stays as it is.",
                        on: inWorktree
                    ) { placement = .worktree }
                    if inWorktree {
                        Divider()
                        ForEach(worktreeLifecycles, id: \.self) { each in
                            radio(each.label, each.summary, on: each == lifecycle) {
                                app.preferences.worktreeLifecycle = each.rawValue
                            }
                        }
                        ListRow(label: "Base branch", value: shownBase ?? "Default") { sheet = .branches }
                            .accessibilityLabel("Base branch")
                            .accessibilityValue(shownBase ?? "Default")
                    }
                    ListSwitch(label: "Start with a worktree by default", isOn: Binding(
                        get: { prefs.worktreeByDefault },
                        set: {
                            prefs.worktreeByDefault = $0
                            placement = nil
                        }))
                }
                .padding(.vertical, Space.lg)
            }
            .navigationTitle("Worktree")
            .navigationBarTitleDisplayMode(.inline)
        }
    }

    private var branchesSheet: some View {
        RadioSheet(
            title: "Base branch",
            choices: (git?.branches ?? []).map { ($0, $0, $0 == git?.currentBranch ? "Current branch" : nil) },
            selected: shownBase
        ) { branch in
            if let pickedPath { baseBranch = (pickedPath, branch) }
            sheet = .worktree
        }
    }

    private func radio(_ title: String, _ description: String, on: Bool, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            HStack(alignment: .top, spacing: Space.md) {
                Image(systemName: on ? "largecircle.fill.circle" : "circle").font(.system(size: 17))
                    .foregroundStyle(on ? palette.foreground : palette.mutedForeground)
                VStack(alignment: .leading, spacing: 2) {
                    Text(title).font(.sans(FontSize.base)).foregroundStyle(palette.foreground)
                    Text(description).font(.sans(FontSize.sm)).foregroundStyle(palette.mutedForeground)
                        .fixedSize(horizontal: false, vertical: true)
                }
                Spacer(minLength: 0)
            }
            .padding(.horizontal, Space.lg)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityAddTraits(on ? .isSelected : [])
    }

    // MARK: Work

    private func choose(_ next: WorkspacePick) {
        // None after something else makes a new folder: the last one went with its draft.
        if next == .scratch && pick != .scratch {
            scratchRound += 1
            scratchFolder = nil
        }
        chosen = next
    }

    private func pickModel(_ modelId: String) {
        let model = defaults?.models.first { $0.id == modelId }
        overrides.modelId = modelId
        if let effort = settings.reasoningEffort, model?.reasoningEfforts.contains(effort) == true { return }
        overrides.reasoningEffort = model?.reasoningEfforts.first
    }

    private func readDefaults() async {
        guard connection.state == .connected, defaults == nil, let client = try? connection.controller.client() else { return }
        if let result = try? await client.getDefaultSettings() { defaults = SessionDefaultsView(result) }
    }

    private func makeScratchFolder() async {
        guard isScratchPick, scratchFolder == nil, connection.state == .connected, let gateway = connection.gateway else { return }
        scratchError = nil
        if scratchRound == 0, let reuse = connection.drafts.scratchFolder {
            scratchFolder = reuse
            return
        }
        do {
            scratchFolder = try await gateway.createScratchWorkspace()
        } catch {
            scratchError = error.localizedDescription
        }
    }

    private func readGit() async {
        git = nil
        guard let pickedPath, let client = try? connection.controller.client() else { return }
        git = try? await client.listGitBranches(ListGitBranchesParams(cwd: pickedPath))
    }

    private func openDraft() async {
        guard let workspace, connection.state == .connected, !taking else { return }
        let opened = await connection.drafts.draft(path: workspace, tags: tags, requested: toolMode.requested)
        guard !Task.isCancelled else { return }
        draft = opened
        guard let opened, let client = try? connection.controller.client() else { return }
        async let commands = try? client.listCommands(ListCommandsParams(sessionId: opened.sessionId))
        async let skills = try? client.listSkills(ListSkillsParams(sessionId: opened.sessionId))
        slashItems = DroiKit.slashItems(commands: await commands?.commands ?? [], skills: await skills?.skills ?? [])
    }

    private func start(_ target: String, prompt: PendingPrompt?) async {
        let target = target.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !target.isEmpty else {
            error = "Enter a directory path."
            return
        }
        taking = true
        creating = true
        error = nil
        defer { creating = false }
        let inTree = inWorktree && target == workspace
        var made: SessionWorktree?
        var sessionId: String?
        do {
            if inTree {
                // A draft is in the Workspace itself; the worktree is made by a Session of its own.
                let opened = try await connection.openSession(
                    target, settings: settings, tags: tags,
                    worktree: WorktreeRequest(lifecycle: lifecycle, promptSlug: prompt?.text ?? "", baseBranch: chosenBase))
                sessionId = opened.sessionId
                made = opened.worktree
                connection.drafts.close()
            } else if let taken = await connection.drafts.take(path: target, settings: settings) {
                sessionId = taken
            } else {
                sessionId = try await connection.openSession(target, settings: settings, tags: tags).sessionId
            }
        } catch {
            self.error = error.localizedDescription
        }
        guard let sessionId else {
            taking = false
            return
        }
        if let prompt, !prompt.text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || !prompt.images.isEmpty {
            connection.pendingPrompts[sessionId] = prompt
        }
        connection.started(
            SessionSummary(
                sessionId: sessionId, cwd: made?.path ?? target, repoRoot: made?.repoRoot, worktree: made,
                updatedAt: Date().timeIntervalSince1970, messagesCount: 0, tags: tags))
        app.router.replaceTop(with: .session(sessionId))
    }
}
