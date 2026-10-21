import DroiKit
import DroidProtocol
import Observation
import SwiftUI

private struct ChangedFile: Hashable {
    var path: String
    /// The Daemon's word: added, modified, deleted, renamed, ...
    var status: String
    var additions: Int
    var deletions: Int
}

private struct GitChanges: Hashable {
    var branch: String
    var files: [ChangedFile]
    var additions: Int
    var deletions: Int
}

/// What the Session has changed in its Workspace, as the Daemon sees it. The
/// Daemon's get_git_diff reports several ranges; the header wants the
/// uncommitted one (working tree against HEAD, untracked files included).
@MainActor
@Observable
private final class GitChangesModel {
    /// Nil for a Workspace that is not a Git repository, and until the first answer.
    private(set) var changes: GitChanges?
    @ObservationIgnored private let connection: ComputerConnection
    @ObservationIgnored private let sessionId: String
    @ObservationIgnored private var unsubscribe: (@MainActor () -> Void)?
    @ObservationIgnored private var reading = false

    init(connection: ComputerConnection, sessionId: String) {
        self.connection = connection
        self.sessionId = sessionId
    }

    func start() {
        unsubscribe?()
        unsubscribe = connection.controller.subscribe { [weak self] event in
            guard let self else { return }
            switch event {
            case .sessionNotification(let id, .toolResult) where id == self.sessionId:
                // The Daemon edits files while a turn runs: read again after each tool result.
                if let state = self.connection.store.session(id)?.workingState, state != .idle {
                    Task { await self.refresh() }
                }
            case .sessionLoaded(let id, _) where id == self.sessionId:
                Task { await self.refresh() }
            default:
                break
            }
        }
    }

    func stop() {
        unsubscribe?()
        unsubscribe = nil
    }

    /// A read already under way is left to finish rather than restarted per result.
    func refresh() async {
        guard !reading, connection.state == .connected else { return }
        reading = true
        defer { reading = false }
        guard let result = try? await connection.controller.client().getGitDiff(
            GetGitDiffParams(sessionId: sessionId, statsOnly: true)),
            let raw = try? JSONValue(encoding: result)
        else { return }
        changes = Self.parse(raw)
    }

    private static func parse(_ raw: JSONValue) -> GitChanges? {
        guard raw["success"]?.boolValue == true, let data = raw["data"] else { return nil }
        let files = (data["unstagedFiles"]?.arrayValue ?? []).compactMap { file -> ChangedFile? in
            guard let path = file["path"]?.stringValue else { return nil }
            return ChangedFile(
                path: path, status: file["status"]?.stringValue ?? "modified",
                additions: Int(file["additions"]?.doubleValue ?? 0), deletions: Int(file["deletions"]?.doubleValue ?? 0))
        }
        let branch = data["isDetachedHead"]?.boolValue == true ? "detached HEAD" : (data["branch"]?.stringValue ?? "")
        return GitChanges(
            branch: branch, files: files, additions: Int(data["unstagedTotalAdditions"]?.doubleValue ?? 0),
            deletions: Int(data["unstagedTotalDeletions"]?.doubleValue ?? 0))
    }
}

/// The Session's branch and uncommitted line counts in the header; tapping
/// opens the changed files. Nothing shows when the Workspace is not a Git repository.
struct GitChangesButton: View {
    let connection: ComputerConnection
    let sessionId: String
    let workingState: DroidWorkingState
    @State private var model: GitChangesModel
    @State private var open = false
    @Environment(\.palette) private var palette

    init(connection: ComputerConnection, sessionId: String, workingState: DroidWorkingState) {
        self.connection = connection
        self.sessionId = sessionId
        self.workingState = workingState
        _model = State(initialValue: GitChangesModel(connection: connection, sessionId: sessionId))
    }

    var body: some View {
        Group {
            if let changes = model.changes {
                button(changes)
            } else {
                // The lifecycle modifiers below do not run on an empty view.
                Color.clear.frame(width: 0, height: 0).accessibilityHidden(true)
            }
        }
        .onAppear { model.start() }
        .onDisappear { model.stop() }
        // Edits made outside Droi (an editor, a terminal, a commit) show up on this beat.
        .task(id: connection.state == .connected) {
            while connection.state == .connected, !Task.isCancelled {
                await model.refresh()
                try? await Task.sleep(for: .seconds(15))
            }
        }
        .onChange(of: workingState) { _, state in
            if state == .idle { Task { await model.refresh() } }
        }
    }

    private func button(_ changes: GitChanges) -> some View {
        let count = changes.files.count
        return Button {
            open = true
        } label: {
            HStack(spacing: 4) {
                Image(systemName: "arrow.triangle.branch").font(.system(size: 12))
                    .foregroundStyle(palette.mutedForeground)
                Text(changes.branch).font(.sans(FontSize.xs)).foregroundStyle(palette.mutedForeground)
                    .lineLimit(1).truncationMode(.middle)
                if count > 0 { Counts(additions: changes.additions, deletions: changes.deletions) }
            }
            .padding(.horizontal, Space.md)
            .frame(height: 36)
            .frame(maxWidth: 200)
            .contentShape(Capsule())
        }
        .buttonStyle(.plain)
        // The toolbar item hides its shared glass (an empty pill while there is
        // nothing to show), so the button carries its own.
        .glassEffect(.regular.interactive(), in: .capsule)
        .accessibilityLabel(
            count > 0
                ? "Branch \(changes.branch), \(count) changed \(count == 1 ? "file" : "files")"
                : "Branch \(changes.branch), no changes"
        )
        .sheet(isPresented: $open) {
            GitChangesSheet(model: model)
                .environment(\.palette, palette)
                .presentationDetents([.medium, .large])
                .presentationDragIndicator(.visible)
        }
    }
}

private struct GitChangesSheet: View {
    let model: GitChangesModel
    @Environment(\.palette) private var palette

    var body: some View {
        let files = model.changes?.files ?? []
        let count = files.count
        VStack(alignment: .leading, spacing: Space.sm) {
            Text(count > 0 ? "\(count) uncommitted \(count == 1 ? "file" : "files")" : "Working tree clean")
                .font(.sans(FontSize.lg, .semibold))
                .foregroundStyle(palette.foreground)
                .padding(.horizontal, Space.lg)
                .padding(.top, Space.lg)
                .accessibilityAddTraits(.isHeader)
            if count > 0 {
                ScrollView {
                    VStack(spacing: 0) {
                        ForEach(files, id: \.path) { FileRow(file: $0) }
                    }
                    .padding(.horizontal, Space.sm)
                }
                .accessibilityLabel("Changed files")
            }
            Spacer(minLength: 0)
        }
    }
}

private struct Counts: View {
    let additions: Int
    let deletions: Int
    @Environment(\.palette) private var palette

    var body: some View {
        HStack(spacing: 4) {
            Text("+\(additions)").foregroundStyle(palette.success)
            Text("−\(deletions)").foregroundStyle(palette.destructiveForeground)
        }
        .font(.mono(FontSize.xs))
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("\(additions) added, \(deletions) removed")
    }
}

private let statusLetters = [
    "added": "A", "modified": "M", "deleted": "D", "renamed": "R", "copied": "C", "untracked": "A",
]

private struct FileRow: View {
    let file: ChangedFile
    @Environment(\.palette) private var palette

    var body: some View {
        let slash = file.path.lastIndex(of: "/")
        let dir = slash.map { String(file.path[...$0]) } ?? ""
        let name = slash.map { String(file.path[file.path.index(after: $0)...]) } ?? file.path
        HStack(spacing: Space.sm) {
            Text(statusLetters[file.status] ?? String(file.status.prefix(1)).uppercased())
                .font(.mono(FontSize.xs))
                .foregroundStyle(file.status == "deleted" ? palette.destructiveForeground : palette.mutedForeground)
                .frame(width: 14)
                .accessibilityLabel(file.status)
            VStack(alignment: .leading, spacing: 0) {
                Text(name).font(.mono(FontSize.sm)).foregroundStyle(palette.foreground)
                    .lineLimit(1).truncationMode(.middle)
                if !dir.isEmpty {
                    Text(dir).font(.mono(FontSize.xs)).foregroundStyle(palette.mutedForeground)
                        .lineLimit(1).truncationMode(.head)
                }
            }
            Spacer(minLength: Space.sm)
            Counts(additions: file.additions, deletions: file.deletions)
        }
        .padding(.horizontal, Space.sm)
        .frame(minHeight: 44)
        .accessibilityElement(children: .combine)
    }
}
