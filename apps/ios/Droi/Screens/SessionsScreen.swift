import Droid
import DroiKit
import DroidProtocol
import SwiftUI

/// The selected computer's Sessions, by Workspace.
struct SessionsScreen: View {
    let connection: ComputerConnection
    @Environment(AppModel.self) private var app
    @Environment(\.palette) private var palette
    @State private var menuOpen = false

    var body: some View {
        VStack(spacing: 0) {
            if menuOpen {
                ComputerMenu(current: connection.computer.id) { menuOpen = false }
                    .transition(.move(edge: .top).combined(with: .opacity))
            }
            ConnectionGate(connection: connection, keepWhenUnreachable: true) {
                SessionList(connection: connection)
            }
        }
        .background(palette.background)
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .principal) {
                ComputerTitle(connection: connection, open: menuOpen) {
                    withAnimation(.snappy(duration: 0.2)) { menuOpen.toggle() }
                }
            }
            ToolbarItemGroup(placement: .topBarTrailing) {
                IconButton(systemName: "square.and.pencil", label: "New session") {
                    app.router.push(.newSession(workspace: nil, scratch: false))
                }
                IconButton(systemName: "gearshape", label: "Settings") { app.router.push(.settings) }
            }
        }
        .onAppear(perform: reopenLast)
        .onChange(of: app.preferences.showArchived) { connection.refreshSessions() }
    }

    /// At launch the Session last open on this computer opens again, if the
    /// list still has it.
    private func reopenLast() {
        guard !app.reopenedLastSession else { return }
        app.reopenedLastSession = true
        let id = connection.computer.id
        guard let last = app.preferences.lastSession(of: id) else { return }
        if connection.session(last) != nil {
            app.router.open(session: last)
        } else {
            app.preferences.setLastSession(nil, of: id)
        }
    }
}

/// The computer's name over the state of its connection; tapping it offers the other computers.
private struct ComputerTitle: View {
    let connection: ComputerConnection
    let open: Bool
    let toggle: () -> Void
    @Environment(\.palette) private var palette

    var body: some View {
        Button(action: toggle) {
            VStack(spacing: 0) {
                HStack(spacing: 4) {
                    Text(connection.computer.name).font(.sans(FontSize.lg, .semibold)).foregroundStyle(palette.foreground)
                    Image(systemName: "chevron.down").font(.system(size: 10, weight: .bold))
                        .foregroundStyle(palette.mutedForeground)
                        .rotationEffect(.degrees(open ? 180 : 0))
                }
                Text(connection.state.label)
                    .font(.sans(FontSize.xs))
                    .foregroundStyle(connection.state == .connected ? palette.mutedForeground : palette.attention)
                    .accessibilityLabel("Connection")
                    .accessibilityValue(connection.state.label)
            }
        }
        .buttonStyle(.plain)
        .accessibilityLabel("Switch computer, \(connection.computer.name)")
        .accessibilityValue(open ? "Expanded" : "Collapsed")
    }
}

private struct ComputerMenu: View {
    let current: String
    let close: () -> Void
    @Environment(AppModel.self) private var app
    @Environment(\.palette) private var palette

    var body: some View {
        VStack(spacing: 0) {
            ForEach(app.computers.computers.filter { $0.id != current }) { computer in
                row(computer.name, icon: "desktopcomputer") {
                    close()
                    app.select(computer.id)
                }
            }
            row("Add a computer", icon: "plus") {
                close()
                app.router.push(.pair)
            }
        }
        .padding(.vertical, Space.xs)
        .background(palette.card)
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Computers")
    }

    private func row(_ title: String, icon: String, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            HStack(spacing: Space.sm) {
                Image(systemName: icon).frame(width: 20)
                Text(title).font(.sans(FontSize.base))
                Spacer()
            }
            .foregroundStyle(palette.foreground)
            .padding(.horizontal, Space.lg)
            .frame(minHeight: 44)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }
}

private struct SessionList: View {
    let connection: ComputerConnection
    @Environment(AppModel.self) private var app
    @Environment(\.palette) private var palette
    @State private var query = ""
    @State private var hits: [SessionSearchHit]?
    @State private var searchError: String?
    @State private var revealed: [String: Int] = [:]
    @State private var renaming: SessionSummary?
    @State private var newTitle = ""
    @State private var archiving: SessionSummary?
    @State private var actionError: String?

    var body: some View {
        let prefs = app.preferences
        let all = connection.sessions
        let shown = mainSessions(foldContinued(all)).filter { prefs.showArchived || $0.archivedAt == nil }
        let pins = Pins(workspaces: Set(prefs.pinnedWorkspaces), sessions: Set(prefs.pinnedSessions))
        let groups = groupByWorkspace(shown, pins: pins)
        let activity = connection.activity
        var runs: [String: SubagentRun] = [:]
        for s in all where s.callingSessionId != nil {
            if let run = connection.subagentRun(s.sessionId) { runs[s.sessionId] = run }
        }
        let running = runningSubagents(all, runs: runs)
        return ScrollView {
            LazyVStack(alignment: .leading, spacing: 0) {
                if !query.trimmingCharacters(in: .whitespaces).isEmpty {
                    SearchResults(query: query, hits: hits, error: searchError) { open($0) }
                } else {
                    if let error = connection.listError ?? actionError {
                        ThemedText(error, size: FontSize.sm, tone: .destructive, scaled: false)
                            .padding(Space.lg)
                    }
                    if connection.listLive && groups.isEmpty {
                        ThemedText("No sessions yet.", tone: .muted, scaled: false).padding(Space.xl)
                    }
                    ForEach(groups) { group in
                        section(group, pins: pins, activity: activity, running: running)
                    }
                    if connection.hasOlderSessions {
                        Button {
                            Task { await connection.loadOlderSessions() }
                        } label: {
                            HStack(spacing: Space.sm) {
                                if connection.loadingOlderSessions {
                                    Spinner(size: 12)
                                } else {
                                    Image(systemName: "chevron.down").font(.system(size: 11, weight: .semibold))
                                }
                                Text("Load older sessions").font(.sans(FontSize.sm))
                            }
                            .foregroundStyle(palette.mutedForeground)
                            .padding(.horizontal, Space.lg)
                            .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading)
                            .contentShape(Rectangle())
                        }
                        .buttonStyle(.plain)
                    }
                }
            }
            .padding(.bottom, Space.md)
        }
        .scrollDismissesKeyboard(.interactively)
        .refreshable { connection.refreshSessions() }
        .searchable(text: $query, placement: .navigationBarDrawer(displayMode: .always), prompt: "Search sessions")
        .task(id: query) { await search() }
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Sessions")
        .alert("Rename session", isPresented: Binding(get: { renaming != nil }, set: { if !$0 { renaming = nil } })) {
            TextField("Session title", text: $newTitle)
            Button("Cancel", role: .cancel) { renaming = nil }
            Button("Save") {
                if let session = renaming { run { try await connection.rename(session.sessionId, to: newTitle) } }
                renaming = nil
            }
        }
        .confirmationDialog(
            archiveWorktreeWarning, isPresented: Binding(get: { archiving != nil }, set: { if !$0 { archiving = nil } }),
            titleVisibility: .visible
        ) {
            Button("Archive", role: .destructive) {
                if let session = archiving { run { try await connection.archive(session) } }
                archiving = nil
            }
            Button("Cancel", role: .cancel) { archiving = nil }
        }
    }

    @ViewBuilder
    private func section(
        _ group: WorkspaceGroup, pins: Pins, activity: [String: SessionActivity], running: [String: Int]
    ) -> some View {
        let folded = app.preferences.foldedWorkspaces.contains(group.key)
        let pinned = pins.workspaces.contains(group.key)
        VStack(alignment: .leading, spacing: 0) {
            Button {
                withAnimation(.snappy(duration: 0.2)) { app.preferences.toggle(group.key, in: \.foldedWorkspaces) }
            } label: {
                HStack(spacing: Space.xs) {
                    Image(systemName: "chevron.down").font(.system(size: 10, weight: .bold))
                        .rotationEffect(.degrees(folded ? -90 : 0))
                    Text(group.label).font(.sans(FontSize.sm, .medium)).lineLimit(1)
                    if pinned {
                        Image(systemName: "pin.fill").font(.system(size: 10)).accessibilityLabel("Pinned")
                    }
                    Spacer(minLength: 0)
                    if folded, let busy = countBusy(group.sessions.map(\.sessionId), activity: activity, subagentsRunning: running) {
                        if busy.needsInput > 0 {
                            Image(systemName: "exclamationmark.circle").font(.system(size: 11)).foregroundStyle(palette.attention)
                        }
                        if busy.working > 0 { Spinner(size: 11, color: palette.working) }
                    }
                }
                .foregroundStyle(palette.mutedForeground)
                .padding(.horizontal, Space.lg)
                .padding(.top, Space.md)
                .frame(minHeight: 36)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityLabel(group.label)
            .accessibilityValue(folded ? "Collapsed" : "Expanded")
            .accessibilityHint("Hold for more actions")
            .contextMenu {
                if group.key != recentsGroupKey {
                    Button(pinned ? "Unpin workspace" : "Pin workspace", systemImage: pinned ? "pin.slash" : "pin") {
                        app.preferences.toggle(group.key, in: \.pinnedWorkspaces)
                    }
                }
                Button("New session here", systemImage: "square.and.pencil") {
                    app.router.push(
                        group.scratch ? .newSession(workspace: nil, scratch: true) : .newSession(workspace: group.path, scratch: false))
                }
            }
            if !folded {
                let visible = visibleSessions(group.sessions, revealed: revealed[group.key] ?? 0, pinned: pins.sessions)
                ForEach(visible.visible) { session in
                    row(session, pinned: pins.sessions.contains(session.sessionId), activity: activity[session.sessionId],
                        subagents: running[session.sessionId] ?? 0)
                }
                if visible.hidden > 0 {
                    Button {
                        revealed[group.key, default: 0] += olderBatch
                    } label: {
                        Text("Show \(min(visible.hidden, olderBatch)) more").font(.sans(FontSize.sm))
                            .foregroundStyle(palette.mutedForeground)
                            .padding(.horizontal, Space.lg)
                            .frame(maxWidth: .infinity, minHeight: 36, alignment: .leading)
                            .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                }
            }
        }
        .accessibilityElement(children: .contain)
        .accessibilityLabel(group.label)
    }

    private func row(_ session: SessionSummary, pinned: Bool, activity: SessionActivity?, subagents: Int) -> some View {
        let unread = connection.unread.contains(session.sessionId)
        return Button {
            open(session.sessionId)
        } label: {
            HStack(spacing: Space.sm) {
                Text(session.title)
                    .font(.sans(FontSize.base, unread ? .medium : .regular))
                    .foregroundStyle(palette.foreground)
                    .lineLimit(1)
                    .frame(maxWidth: .infinity, alignment: .leading)
                if let branch = session.worktree?.branch {
                    HStack(spacing: 3) {
                        Image(systemName: "arrow.triangle.branch").font(.system(size: 10))
                        Text(branch).font(.mono(FontSize.xs)).lineLimit(1)
                    }
                    .foregroundStyle(palette.mutedForeground)
                    .frame(maxWidth: 140, alignment: .trailing)
                    .fixedSize(horizontal: true, vertical: false)
                }
                if unread {
                    Circle().fill(palette.unread).frame(width: 6, height: 6).accessibilityLabel("Unread")
                }
                if pinned {
                    Image(systemName: "pin.fill").font(.system(size: 10)).foregroundStyle(palette.mutedForeground)
                        .accessibilityLabel("Pinned")
                }
                if session.archivedAt != nil {
                    Text("Archived").font(.sans(FontSize.xs)).foregroundStyle(palette.mutedForeground)
                }
                ActivityMark(activity: activity, subagents: subagents)
            }
            .padding(.horizontal, Space.lg)
            .frame(minHeight: 44)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityHint("Hold for more actions")
        .contextMenu {
            Button(pinned ? "Unpin" : "Pin", systemImage: pinned ? "pin.slash" : "pin") {
                app.preferences.toggle(session.sessionId, in: \.pinnedSessions)
            }
            Button("Rename", systemImage: "pencil") {
                newTitle = session.title
                renaming = session
            }
            if session.archivedAt == nil {
                Button("Archive", systemImage: "archivebox") {
                    if archiveDeletesWorktree(session.worktree) {
                        archiving = session
                    } else {
                        run { try await connection.archive(session) }
                    }
                }
            } else {
                Button("Unarchive", systemImage: "tray.and.arrow.up") { run { try await connection.unarchive(session) } }
            }
        }
    }

    private func open(_ sessionId: String) {
        app.router.open(session: sessionId)
    }

    private func run(_ body: @escaping () async throws -> Void) {
        actionError = nil
        Task {
            do { try await body() } catch { actionError = String(describing: error) }
        }
    }

    private func search() async {
        searchError = nil
        guard isSearchable(query) else {
            hits = nil
            return
        }
        hits = nil
        do {
            try await Task.sleep(for: searchDebounce)
            let client = try connection.controller.client()
            let result = try await client.searchSessions(searchParams(query))
            hits = result.sessions.map(SessionSearchHit.init)
        } catch is CancellationError {
        } catch {
            searchError = String(describing: error)
        }
    }
}

/// What a Session is doing, at the end of its row.
struct ActivityMark: View {
    let activity: SessionActivity?
    var subagents = 0
    @Environment(\.palette) private var palette

    var body: some View {
        switch activity {
        case .compacting:
            mark(spinner: true, "Compacting", color: palette.working).accessibilityLabel("Compacting")
        case .needsInput:
            HStack(spacing: 4) {
                Image(systemName: "exclamationmark.circle").font(.system(size: 12))
                Text("Needs input").font(.sans(FontSize.xs))
            }
            .foregroundStyle(palette.attention)
            .accessibilityElement(children: .ignore)
            .accessibilityLabel("Needs input")
        case .working where subagents == 0:
            mark(spinner: true, "Working", color: palette.working).accessibilityLabel("Working")
        default:
            if subagents > 0 {
                mark(spinner: true, "\(subagents) subagent\(subagents == 1 ? "" : "s") running", color: palette.working)
            }
        }
    }

    private func mark(spinner: Bool, _ text: String, color: Color) -> some View {
        HStack(spacing: 4) {
            Spinner(size: 11, color: color)
            Text(text).font(.sans(FontSize.xs))
        }
        .foregroundStyle(color)
        .accessibilityElement(children: .ignore)
    }
}

private struct SearchResults: View {
    let query: String
    let hits: [SessionSearchHit]?
    let error: String?
    let open: (String) -> Void
    @Environment(\.palette) private var palette

    var body: some View {
        Group {
            if !isSearchable(query) {
                ThemedText("Type a little more to search.", tone: .muted, scaled: false)
            } else if let error {
                ThemedText(error, size: FontSize.sm, tone: .destructive, scaled: false)
            } else if let hits {
                if hits.isEmpty {
                    ThemedText("No sessions match.", tone: .muted, scaled: false)
                } else {
                    VStack(alignment: .leading, spacing: 0) {
                        ForEach(hits) { hit in
                            Button { open(hit.sessionId) } label: { row(hit) }.buttonStyle(.plain)
                        }
                    }
                    .padding(.horizontal, -Space.lg)
                    .accessibilityElement(children: .contain)
                    .accessibilityLabel("Search results")
                }
            } else {
                HStack(spacing: Space.sm) {
                    Spinner(size: 12)
                    ThemedText("Searching…", tone: .muted, scaled: false)
                }
            }
        }
        .padding(Space.lg)
    }

    private func row(_ hit: SessionSearchHit) -> some View {
        var attributed = AttributedString()
        for run in hit.snippet {
            var part = AttributedString(run.text)
            part.foregroundColor = run.match ? palette.foreground : palette.mutedForeground
            part.font = .sans(FontSize.sm, run.match ? .medium : .regular)
            attributed += part
        }
        let snippet = Text(attributed)
        return VStack(alignment: .leading, spacing: 2) {
            Text(hit.title).font(.sans(FontSize.base)).foregroundStyle(palette.foreground).lineLimit(1)
            snippet.lineLimit(2)
        }
        .padding(.horizontal, Space.lg)
        .padding(.vertical, Space.sm)
        .frame(maxWidth: .infinity, alignment: .leading)
        .contentShape(Rectangle())
    }
}
