import Droid
import DroiKit
import DroidProtocol
import Observation
import SwiftUI
import UIKit

private enum ToolsTab: String, CaseIterable {
    case skills, mcp

    var label: String {
        switch self {
        case .skills: "Skills"
        case .mcp: "MCP servers"
        }
    }
}

/// The Session's skills and MCP Servers, behind a wrench in the header.
struct ToolsButton: View {
    let connection: ComputerConnection
    let sessionId: String
    @State private var open = false
    @State private var tab: ToolsTab = .skills
    @Environment(\.palette) private var palette

    var body: some View {
        Button {
            open = true
        } label: {
            Image(systemName: "wrench")
                .font(.system(size: 15))
                .foregroundStyle(palette.mutedForeground)
                .frame(width: 30, height: 30)
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel("Skills and MCP servers")
        .sheet(isPresented: $open) {
            ToolsSheet(connection: connection, sessionId: sessionId, tab: $tab)
                .environment(\.palette, palette)
                .presentationDetents([.medium, .large])
                .presentationDragIndicator(.visible)
        }
    }
}

private struct ToolsSheet: View {
    let connection: ComputerConnection
    let sessionId: String
    @Binding var tab: ToolsTab
    @Environment(\.palette) private var palette

    var body: some View {
        VStack(alignment: .leading, spacing: Space.sm) {
            Text("Skills and MCP servers")
                .font(.sans(FontSize.lg, .semibold))
                .foregroundStyle(palette.foreground)
                .padding(.horizontal, Space.lg)
                .padding(.top, Space.lg)
                .accessibilityAddTraits(.isHeader)
            HStack(spacing: Space.xs) {
                ForEach(ToolsTab.allCases, id: \.self) { item in
                    Button {
                        tab = item
                    } label: {
                        Text(item.label)
                            .font(.sans(FontSize.sm, tab == item ? .medium : .regular))
                            .foregroundStyle(tab == item ? palette.foreground : palette.mutedForeground)
                            .padding(.horizontal, Space.md)
                            .frame(height: 30)
                            .background(tab == item ? palette.accent : .clear, in: RoundedRectangle(cornerRadius: Radius.md))
                            .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .accessibilityAddTraits(tab == item ? .isSelected : [])
                }
            }
            .padding(.horizontal, Space.lg)
            .accessibilityElement(children: .contain)
            .accessibilityLabel("Skills and MCP servers")
            ScrollView {
                VStack(alignment: .leading, spacing: Space.md) {
                    switch tab {
                    case .skills: SkillsList(connection: connection, sessionId: sessionId)
                    case .mcp: McpList(connection: connection, sessionId: sessionId)
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(.horizontal, Space.lg)
                .padding(.bottom, Space.lg)
            }
            .scrollDismissesKeyboard(.interactively)
        }
    }
}

private func toolsErrorText(_ error: any Error) -> String {
    (error as? LocalizedError)?.errorDescription ?? String(describing: error)
}

// MARK: Shared bits

private struct ToolsBadge: View {
    let text: String
    @Environment(\.palette) private var palette

    var body: some View {
        Text(text)
            .font(.sans(FontSize.xs))
            .foregroundStyle(palette.mutedForeground)
            .padding(.horizontal, 6)
            .padding(.vertical, 1)
            .background(palette.muted, in: RoundedRectangle(cornerRadius: Radius.sm))
    }
}

private struct ToolsLoading: View {
    let what: String
    @Environment(\.palette) private var palette

    var body: some View {
        HStack(spacing: Space.sm) {
            Spinner(size: 14)
            Text("Loading \(what)…").font(.sans(FontSize.sm)).foregroundStyle(palette.mutedForeground)
        }
        .accessibilityElement(children: .combine)
    }
}

private struct ToolsFailure: View {
    let message: String
    @Environment(\.palette) private var palette

    var body: some View {
        Text(message).font(.sans(FontSize.sm)).foregroundStyle(palette.destructiveForeground)
            .fixedSize(horizontal: false, vertical: true)
    }
}

private struct SmallButton: View {
    let label: String
    var accessibilityName: String?
    var busy = false
    var disabled = false
    var destructive = false
    var primary = false
    let action: () -> Void
    @Environment(\.palette) private var palette

    var body: some View {
        Button(action: action) {
            HStack(spacing: Space.xs) {
                if busy { Spinner(size: 12) }
                Text(label).font(.sans(FontSize.xs, .medium))
            }
            .foregroundStyle(
                primary ? palette.primaryForeground : destructive ? palette.destructiveForeground : palette.foreground
            )
            .padding(.horizontal, Space.sm)
            .frame(height: 30)
            .background(primary ? palette.primary : palette.background, in: RoundedRectangle(cornerRadius: Radius.md))
            .hairlineBorder(primary ? palette.primary : palette.border, radius: Radius.md)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .disabled(disabled || busy)
        .opacity(disabled && !busy ? 0.5 : 1)
        .accessibilityLabel(accessibilityName ?? label)
    }
}

private func plural(_ count: Int, _ word: String) -> String {
    "\(count) \(count == 1 ? word : "\(word)s")"
}

// MARK: Skills

@MainActor
@Observable
private final class SkillsModel {
    private(set) var skills: [SkillInfo] = []
    private(set) var projectAvailable = false
    private(set) var loaded = false
    private(set) var loadError: String?
    private(set) var actionError: String?
    private(set) var saving: String?
    @ObservationIgnored private let connection: ComputerConnection
    @ObservationIgnored private let sessionId: String

    init(connection: ComputerConnection, sessionId: String) {
        self.connection = connection
        self.sessionId = sessionId
    }

    func load() async {
        do {
            let result = try await connection.controller.client().listSkills(ListSkillsParams(sessionId: sessionId))
            skills = result.skills
            projectAvailable = result.projectAvailable == true
            loaded = true
            loadError = nil
        } catch {
            if !loaded { loadError = toolsErrorText(error) }
        }
    }

    func setDisabled(_ name: String, disabled: Bool, level: SkillLevel) async {
        saving = name
        actionError = nil
        do {
            _ = try await connection.controller.client().setSkillDisabled(
                SetSkillDisabledParams(
                    sessionId: sessionId, skillName: name, disabled: disabled,
                    settingsLevel: EditableSkillSettingsLevel(rawValue: level.rawValue)))
        } catch {
            actionError = toolsErrorText(error)
        }
        await load()
        saving = nil
    }
}

private struct SkillsList: View {
    let connection: ComputerConnection
    @State private var model: SkillsModel
    @Environment(\.palette) private var palette

    init(connection: ComputerConnection, sessionId: String) {
        self.connection = connection
        _model = State(initialValue: SkillsModel(connection: connection, sessionId: sessionId))
    }

    var body: some View {
        Group {
            if let error = model.loadError {
                ToolsFailure(message: error)
            } else if !model.loaded {
                ToolsLoading(what: "skills")
            } else if model.skills.isEmpty {
                Text("No skills are installed.").font(.sans(FontSize.base)).foregroundStyle(palette.mutedForeground)
            } else {
                if let error = model.actionError { ToolsFailure(message: error) }
                ForEach(groupSkills(model.skills), id: \.location) { group in
                    VStack(alignment: .leading, spacing: Space.xs) {
                        Text(group.location.label)
                            .font(.sans(FontSize.xs, .medium))
                            .foregroundStyle(palette.mutedForeground)
                            .accessibilityAddTraits(.isHeader)
                        ForEach(group.skills, id: \.name) { skill in
                            SkillRow(skill: skill, projectAvailable: model.projectAvailable, saving: model.saving == skill.name) {
                                disabled, level in
                                Task { await model.setDisabled(skill.name, disabled: disabled, level: level) }
                            }
                        }
                    }
                    .accessibilityElement(children: .contain)
                    .accessibilityLabel(group.location.label)
                }
            }
        }
        .task(id: connection.state == .connected) {
            if connection.state == .connected { await model.load() }
        }
    }
}

private struct SkillRow: View {
    let skill: SkillInfo
    let projectAvailable: Bool
    let saving: Bool
    let onSwitch: (Bool, SkillLevel) -> Void
    @Environment(\.palette) private var palette

    var body: some View {
        let off = skill.enabled == false
        let toggle = skillSwitch(skill, projectAvailable: projectAvailable)
        HStack(spacing: Space.sm) {
            VStack(alignment: .leading, spacing: 2) {
                HStack(spacing: Space.xs) {
                    Text(skill.name).font(.sans(FontSize.base, .medium)).foregroundStyle(palette.foreground)
                    if off { ToolsBadge(text: disabledLabel(skill)) }
                    if skill.userInvocable == false { ToolsBadge(text: "Model only") }
                    if saving { Spinner(size: 12) }
                }
                if let description = skill.description, !description.isEmpty {
                    Text(description).font(.sans(FontSize.sm)).foregroundStyle(palette.mutedForeground).lineLimit(2)
                }
            }
            Spacer(minLength: Space.sm)
            // A personal skill is managed by editing its files, so it has no switch at all.
            if skill.location != .personal {
                Toggle(
                    "\(skill.name) enabled",
                    isOn: Binding(
                        get: { !off },
                        set: { on in if let toggle { onSwitch(!on, toggle.level) } })
                )
                .labelsHidden()
                .tint(palette.success)
                .disabled(toggle == nil || saving)
            }
        }
        .padding(.vertical, Space.xs)
        .opacity(off ? 0.7 : 1)
        .accessibilityElement(children: .contain)
        .accessibilityLabel(skill.name)
    }
}

// MARK: MCP

@MainActor
@Observable
private final class McpModel {
    private(set) var servers: [MCPServerStatusInfo] = []
    private(set) var summary: MCPStatusSummary?
    private(set) var tools: [MCPToolInfo] = []
    private(set) var loaded = false
    private(set) var loadError: String?
    private(set) var registry: [MCPRegistryServer] = []
    private(set) var registryLoading = true
    private(set) var registryError: String?
    private(set) var busy: String?
    private(set) var error: String?
    @ObservationIgnored private let connection: ComputerConnection
    @ObservationIgnored private let sessionId: String
    @ObservationIgnored private var unsubscribe: (@MainActor () -> Void)?
    @ObservationIgnored private var registryLoadedAt: Date?

    init(connection: ComputerConnection, sessionId: String) {
        self.connection = connection
        self.sessionId = sessionId
    }

    /// The Daemon pushes the server list as connections come and go; a sign-in
    /// it asks for shows in the list as the server's pendingAuthUrl.
    func start() {
        unsubscribe?()
        unsubscribe = connection.controller.subscribe { [weak self] event in
            guard let self, case .sessionNotification(let id, let notification) = event, id == self.sessionId else {
                return
            }
            switch notification {
            case .mcpStatusChanged(let change):
                self.servers = change.servers
                self.summary = change.summary
                self.loaded = true
            case .mcpAuthRequired, .mcpAuthCompleted:
                Task { await self.loadServers() }
            default:
                break
            }
        }
    }

    func stop() {
        unsubscribe?()
        unsubscribe = nil
    }

    func load() async {
        async let servers: Void = loadServers()
        async let tools: Void = loadTools()
        _ = await (servers, tools)
    }

    func loadServers() async {
        do {
            let result = try await connection.controller.client().listMCPServers(ListMCPServersParams(sessionId: sessionId))
            servers = result.servers
            summary = result.summary
            loaded = true
            loadError = nil
        } catch {
            if !loaded { loadError = toolsErrorText(error) }
        }
    }

    func loadTools() async {
        if let result = try? await connection.controller.client().listMCPTools(ListMCPToolsParams(sessionId: sessionId)) {
            tools = result.tools
        }
    }

    func loadRegistry() async {
        if let at = registryLoadedAt, Date().timeIntervalSince(at) < 300 { return }
        registryLoading = registry.isEmpty
        do {
            registry = try await connection.controller.client().listMCPRegistry(ListMCPRegistryParams(sessionId: sessionId))
                .servers
            registryLoadedAt = Date()
            registryError = nil
        } catch {
            registryError = toolsErrorText(error)
        }
        registryLoading = false
    }

    /// Runs one change for a server, then reads the list and its tools again. False when the Daemon refused.
    @discardableResult
    private func act(_ serverName: String, _ run: (DroidClient) async throws -> Void) async -> Bool {
        busy = serverName
        error = nil
        var ok = true
        do {
            try await run(connection.controller.client())
        } catch {
            self.error = toolsErrorText(error)
            ok = false
        }
        await load()
        busy = nil
        return ok
    }

    func setEnabled(_ name: String, _ enabled: Bool) async {
        let id = sessionId
        await act(name) { _ = try await $0.toggleMCPServer(ToggleMCPServerParams(serverName: name, enabled: enabled, sessionId: id)) }
    }

    func remove(_ name: String) async {
        let id = sessionId
        await act(name) { _ = try await $0.removeMCPServer(RemoveMCPServerParams(serverName: name, sessionId: id)) }
    }

    func add(_ input: McpServerInput) async -> Bool {
        let params = input.params(sessionId: sessionId)
        return await act(input.name) { _ = try await $0.addMCPServer(params) }
    }

    func setToolEnabled(_ server: String, _ tool: String, _ enabled: Bool) async {
        let id = sessionId
        await act(server) {
            _ = try await $0.toggleMCPTool(
                ToggleMCPToolParams(serverName: server, toolName: tool, enabled: enabled, sessionId: id))
        }
    }

    func signIn(_ name: String) async {
        let id = sessionId
        await act(name) { _ = try await $0.authenticateMCPServer(AuthenticateMCPServerParams(serverName: name, sessionId: id)) }
    }

    func cancelSignIn(_ name: String) async {
        let id = sessionId
        await act(name) { _ = try await $0.cancelMCPAuth(CancelMCPAuthParams(serverName: name, sessionId: id)) }
    }

    func signOut(_ name: String) async {
        let id = sessionId
        await act(name) { _ = try await $0.clearMCPAuth(ClearMCPAuthParams(serverName: name, sessionId: id)) }
    }
}

private enum McpPage {
    case list
    case catalogue
    /// `from` is the catalogue entry being set up; nil for a server by hand.
    case form(from: MCPRegistryServer?)
}

private struct McpList: View {
    let connection: ComputerConnection
    @State private var model: McpModel
    @State private var page: McpPage = .list
    @Environment(\.palette) private var palette

    init(connection: ComputerConnection, sessionId: String) {
        self.connection = connection
        _model = State(initialValue: McpModel(connection: connection, sessionId: sessionId))
    }

    var body: some View {
        let servers = sortServers(model.servers)
        let taken = servers.map(\.name)
        Group {
            if let error = model.loadError {
                ToolsFailure(message: error)
            } else if !model.loaded {
                ToolsLoading(what: "MCP servers")
            } else {
                switch page {
                case .list:
                    serverList(servers)
                case .catalogue:
                    McpCatalogue(
                        model: model, taken: taken,
                        onAdd: add,
                        onSetUp: { page = .form(from: $0) },
                        onByHand: { page = .form(from: nil) },
                        onBack: { page = .list })
                case .form(let from):
                    McpAddServer(
                        model: model, from: from, taken: taken, onAdd: add, onBack: { page = .catalogue })
                }
            }
        }
        .task(id: connection.state == .connected) {
            if connection.state == .connected { await model.load() }
        }
        .onAppear { model.start() }
        .onDisappear { model.stop() }
    }

    private func add(_ input: McpServerInput) async -> Bool {
        let added = await model.add(input)
        if added { page = .list }
        return added
    }

    @ViewBuilder
    private func serverList(_ servers: [MCPServerStatusInfo]) -> some View {
        if let configError = model.summary?.configError {
            ToolsFailure(message: "\(configError.path): \(configError.message)")
        }
        if let error = model.error { ToolsFailure(message: error) }
        if servers.isEmpty {
            Text("No MCP servers are configured.").font(.sans(FontSize.base)).foregroundStyle(palette.mutedForeground)
        } else {
            VStack(alignment: .leading, spacing: Space.sm) {
                ForEach(servers, id: \.name) { server in
                    McpServerRow(model: model, server: server, tools: model.tools.filter { $0.serverName == server.name })
                }
            }
            .accessibilityElement(children: .contain)
            .accessibilityLabel("MCP servers")
        }
        SmallButton(label: "Add server") { page = .catalogue }
    }
}

private struct McpSubHeader: View {
    let title: String
    let onBack: () -> Void
    @Environment(\.palette) private var palette

    var body: some View {
        HStack(spacing: Space.xs) {
            Button(action: onBack) {
                Image(systemName: "chevron.left").font(.system(size: 14, weight: .semibold))
                    .foregroundStyle(palette.mutedForeground)
                    .frame(width: 36, height: 36)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityLabel("Back")
            Text(title).font(.sans(FontSize.base, .medium)).foregroundStyle(palette.foreground)
                .accessibilityAddTraits(.isHeader)
        }
    }
}

private struct McpCatalogue: View {
    let model: McpModel
    let taken: [String]
    let onAdd: (McpServerInput) async -> Bool
    let onSetUp: (MCPRegistryServer) -> Void
    let onByHand: () -> Void
    let onBack: () -> Void
    @State private var query = ""
    @Environment(\.palette) private var palette

    var body: some View {
        let entries = filterRegistry(model.registry, query: query, taken: taken)
        let trimmed = query.trimmingCharacters(in: .whitespacesAndNewlines)
        VStack(alignment: .leading, spacing: Space.md) {
            McpSubHeader(title: "Add MCP server", onBack: onBack)
            HStack(spacing: Space.sm) {
                Image(systemName: "magnifyingglass").font(.system(size: 13)).foregroundStyle(palette.mutedForeground)
                TextField("Search Factory's catalogue", text: $query)
                    .font(.sans(FontSize.sm))
                    .foregroundStyle(palette.foreground)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .submitLabel(.search)
                    .accessibilityLabel("Search the catalogue")
            }
            .padding(.horizontal, Space.md)
            .frame(height: 40)
            .background(palette.card, in: RoundedRectangle(cornerRadius: Radius.lg))
            if let error = model.error { ToolsFailure(message: error) }
            if model.registryLoading {
                ToolsLoading(what: "the catalogue")
            } else if model.registryError != nil {
                Text("The catalogue is not available.").font(.sans(FontSize.sm)).foregroundStyle(palette.mutedForeground)
            } else if entries.isEmpty {
                Text(trimmed.isEmpty ? "Everything in the catalogue is added." : "Nothing in the catalogue matches “\(trimmed)”.")
                    .font(.sans(FontSize.sm)).foregroundStyle(palette.mutedForeground)
            } else {
                VStack(alignment: .leading, spacing: Space.sm) {
                    ForEach(entries, id: \.name) { entry in
                        row(entry)
                    }
                }
                .accessibilityElement(children: .contain)
                .accessibilityLabel("Catalogue")
            }
            Rectangle().fill(palette.border).frame(height: 0.5)
            HStack(spacing: Space.sm) {
                Text("Not in the catalogue?").font(.sans(FontSize.sm)).foregroundStyle(palette.mutedForeground)
                SmallButton(label: "Add a server by hand", action: onByHand)
            }
        }
        .task { await model.loadRegistry() }
    }

    private func row(_ entry: MCPRegistryServer) -> some View {
        let setup = needsSetup(entry)
        return HStack(spacing: Space.sm) {
            VStack(alignment: .leading, spacing: 1) {
                HStack(spacing: Space.xs) {
                    Text(entry.name).font(.sans(FontSize.base, .medium)).foregroundStyle(palette.foreground)
                    ToolsBadge(text: entry.type.rawValue)
                }
                Text(entry.description).font(.sans(FontSize.sm)).foregroundStyle(palette.mutedForeground).lineLimit(2)
                if let note = entry.note {
                    Text(note).font(.sans(FontSize.xs)).foregroundStyle(palette.mutedForeground)
                }
            }
            Spacer(minLength: Space.sm)
            SmallButton(
                label: setup ? "Set up" : "Add", accessibilityName: "\(setup ? "Set up" : "Add") \(entry.name)",
                busy: model.busy == entry.name, disabled: model.busy != nil
            ) {
                if !setup, case .success(let input) = parseServerForm(ServerForm(registry: entry)) {
                    Task { _ = await onAdd(input) }
                } else {
                    onSetUp(entry)
                }
            }
        }
        .accessibilityElement(children: .contain)
        .accessibilityLabel(entry.name)
    }
}

private struct McpServerRow: View {
    let model: McpModel
    let server: MCPServerStatusInfo
    let tools: [MCPToolInfo]
    @State private var toolsOpen = false
    @State private var confirmRemove = false
    @Environment(\.palette) private var palette

    var body: some View {
        let readOnly = isReadOnly(server)
        let busy = model.busy == server.name
        let on = server.status.rawValue != "disabled"
        // The Daemon does not always count the tools in the server list.
        let toolCount = server.toolCount.map(Int.init) ?? tools.count
        VStack(alignment: .leading, spacing: Space.xs) {
            HStack(spacing: Space.sm) {
                Circle().fill(dotColor).frame(width: 8, height: 8)
                    .accessibilityElement()
                    .accessibilityLabel(server.status.label)
                VStack(alignment: .leading, spacing: 1) {
                    HStack(spacing: Space.xs) {
                        Text(server.name).font(.sans(FontSize.base, .medium)).foregroundStyle(palette.foreground)
                        ToolsBadge(text: server.serverType.rawValue)
                        if server.source == .org { ToolsBadge(text: "Organization") }
                        if server.source == .project { ToolsBadge(text: "Project") }
                        if isDroiMemory(server) { ToolsBadge(text: "Droi") }
                    }
                    Text(statusLine(on: on, toolCount: toolCount))
                        .font(.sans(FontSize.sm)).foregroundStyle(palette.mutedForeground)
                    if isDroiMemory(server) {
                        Text("Droi’s Memory. Turn it on or off on the computer in Settings → Memory.")
                            .font(.sans(FontSize.sm)).foregroundStyle(palette.mutedForeground)
                    }
                }
                Spacer(minLength: Space.sm)
                if !isDroiMemory(server) {
                    Toggle(
                        "\(server.name) enabled",
                        isOn: Binding(get: { on }, set: { checked in Task { await model.setEnabled(server.name, checked) } })
                    )
                    .labelsHidden()
                    .tint(palette.success)
                    .disabled(readOnly || busy)
                }
            }
            if let url = server.pendingAuthUrl {
                SignInNotice(
                    serverName: server.name, url: url,
                    message: server.pendingAuthMessage ?? "Sign in to continue"
                ) {
                    Task { await model.cancelSignIn(server.name) }
                }
            }
            HStack(spacing: Space.xs) {
                if on, toolCount > 0 {
                    Button {
                        toolsOpen.toggle()
                    } label: {
                        HStack(spacing: 2) {
                            Image(systemName: "chevron.right").font(.system(size: 10, weight: .semibold))
                                .rotationEffect(.degrees(toolsOpen ? 90 : 0))
                            Text(plural(toolCount, "tool")).font(.sans(FontSize.xs, .medium))
                        }
                        .foregroundStyle(palette.mutedForeground)
                        .frame(height: 30)
                        .padding(.trailing, Space.xs)
                        .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .accessibilityValue(toolsOpen ? "expanded" : "collapsed")
                }
                if !readOnly, on, needsSignIn(server), server.pendingAuthUrl == nil {
                    SmallButton(label: "Sign in", disabled: busy) { Task { await model.signIn(server.name) } }
                }
                if !readOnly, server.hasAuthTokens == true {
                    SmallButton(label: "Sign out", disabled: busy) { Task { await model.signOut(server.name) } }
                }
                if canRemove(server) {
                    if confirmRemove {
                        SmallButton(label: "Remove \(server.name)", disabled: busy, destructive: true) {
                            Task { await model.remove(server.name) }
                        }
                        SmallButton(label: "Keep") { confirmRemove = false }
                    } else {
                        SmallButton(label: "Remove", disabled: busy) { confirmRemove = true }
                    }
                }
            }
            if toolsOpen, on {
                McpToolList(model: model, server: server, tools: tools, readOnly: readOnly)
            }
        }
        .padding(.vertical, Space.xs)
        .accessibilityElement(children: .contain)
        .accessibilityLabel(server.name)
    }

    private var dotColor: Color {
        switch server.status.rawValue {
        case "connected": palette.success
        case "connecting": palette.attention
        case "failed": palette.destructiveForeground
        default: palette.border
        }
    }

    private func statusLine(on: Bool, toolCount: Int) -> String {
        var line = server.status.label
        if on, toolCount > 0 { line += " · \(plural(toolCount, "tool"))" }
        if let error = server.error, !error.isEmpty { line += " · \(error)" }
        return line
    }
}

private struct McpToolList: View {
    let model: McpModel
    let server: MCPServerStatusInfo
    let tools: [MCPToolInfo]
    let readOnly: Bool
    @Environment(\.palette) private var palette

    var body: some View {
        if tools.isEmpty {
            ToolsLoading(what: "tools")
        } else {
            VStack(alignment: .leading, spacing: Space.xs) {
                ForEach(tools, id: \.name) { tool in
                    HStack(spacing: Space.sm) {
                        VStack(alignment: .leading, spacing: 1) {
                            Text(tool.name).font(.mono(FontSize.xs)).foregroundStyle(palette.foreground)
                            if let description = tool.description, !description.isEmpty {
                                Text(description).font(.sans(FontSize.xs)).foregroundStyle(palette.mutedForeground)
                                    .lineLimit(1)
                            }
                        }
                        Spacer(minLength: Space.sm)
                        Toggle(
                            "\(tool.name) enabled",
                            isOn: Binding(
                                get: { tool.isEnabled },
                                set: { checked in Task { await model.setToolEnabled(server.name, tool.name, checked) } })
                        )
                        .labelsHidden()
                        .tint(palette.success)
                        .scaleEffect(0.8)
                        .frame(width: 44)
                        .disabled(readOnly)
                    }
                }
            }
            .padding(.leading, Space.lg)
            .accessibilityElement(children: .contain)
            .accessibilityLabel("\(server.name) tools")
        }
    }
}

/// A sign-in finishes at the Daemon's own callback on the computer, which this
/// phone cannot reach; the page's link is offered to copy and open there.
private struct SignInNotice: View {
    let serverName: String
    let url: String
    let message: String
    let onCancel: () -> Void
    @State private var copied = false
    @Environment(\.palette) private var palette

    var body: some View {
        VStack(alignment: .leading, spacing: Space.xs) {
            Text(message).font(.sans(FontSize.sm)).foregroundStyle(palette.foreground)
            Text("The sign-in finishes on the computer running Droi: open the page there.")
                .font(.sans(FontSize.xs)).foregroundStyle(palette.mutedForeground)
            HStack(spacing: Space.xs) {
                SmallButton(label: copied ? "Link copied" : "Copy sign-in link") {
                    UIPasteboard.general.string = url
                    copied = true
                }
                SmallButton(label: "Cancel", action: onCancel)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(Space.sm)
        .background(palette.card, in: RoundedRectangle(cornerRadius: Radius.lg))
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Sign in to \(serverName)")
    }
}

private struct McpAddServer: View {
    let model: McpModel
    let from: MCPRegistryServer?
    let taken: [String]
    let onAdd: (McpServerInput) async -> Bool
    let onBack: () -> Void
    @State private var form: ServerForm
    @State private var problem: String?
    @State private var saving = false
    @Environment(\.palette) private var palette

    init(
        model: McpModel, from: MCPRegistryServer?, taken: [String], onAdd: @escaping (McpServerInput) async -> Bool,
        onBack: @escaping () -> Void
    ) {
        self.model = model
        self.from = from
        self.taken = taken
        self.onAdd = onAdd
        self.onBack = onBack
        _form = State(initialValue: from.map { ServerForm(registry: $0) } ?? ServerForm())
    }

    private static let types: [(value: MCPServerType, label: String)] = [
        (.stdio, "Command"), (.http, "HTTP"), (.sse, "SSE"),
    ]

    var body: some View {
        let remote = form.type != .stdio
        VStack(alignment: .leading, spacing: Space.sm) {
            McpSubHeader(title: from.map { "Set up \($0.name)" } ?? "Add a server by hand", onBack: onBack)
            if let note = from?.note {
                Text(note).font(.sans(FontSize.sm)).foregroundStyle(palette.foreground)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(Space.sm)
                    .background(palette.card, in: RoundedRectangle(cornerRadius: Radius.lg))
            }
            HStack(spacing: Space.xs) {
                ForEach(Self.types, id: \.label) { type in
                    Button {
                        form.type = type.value
                    } label: {
                        Text(type.label)
                            .font(.sans(FontSize.xs, form.type == type.value ? .medium : .regular))
                            .foregroundStyle(palette.foreground)
                            .padding(.horizontal, Space.md)
                            .frame(height: 30)
                            .background(
                                form.type == type.value ? palette.accent : palette.background,
                                in: RoundedRectangle(cornerRadius: Radius.md)
                            )
                            .hairlineBorder(palette.border, radius: Radius.md)
                            .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .accessibilityAddTraits(form.type == type.value ? .isSelected : [])
                }
            }
            .accessibilityElement(children: .contain)
            .accessibilityLabel("Server type")
            DroiTextField(label: "Server name", text: $form.name, placeholder: "Name", mono: true)
            if remote {
                DroiTextField(label: "Server URL", text: $form.url, placeholder: "https://…", mono: true, keyboard: .URL)
                TextField("Headers, one per line: Authorization: Bearer …", text: $form.headers, axis: .vertical)
                    .lineLimit(3...6)
                    .font(.mono(FontSize.sm))
                    .foregroundStyle(palette.foreground)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .padding(Space.md)
                    .background(palette.card, in: RoundedRectangle(cornerRadius: Radius.lg))
                    .accessibilityLabel("Headers")
            } else {
                DroiTextField(label: "Command", text: $form.command, placeholder: "Command, e.g. npx", mono: true)
                DroiTextField(
                    label: "Arguments", text: $form.args, placeholder: "Arguments, separated by spaces", mono: true)
            }
            if let message = problem ?? model.error { ToolsFailure(message: message) }
            HStack(spacing: Space.xs) {
                Spacer()
                SmallButton(label: "Cancel", action: onBack)
                SmallButton(label: "Add server", disabled: saving, primary: true) { submit() }
            }
        }
    }

    private func submit() {
        switch parseServerForm(form) {
        case .failure(let failure):
            problem = failure.message
        case .success(let input):
            if taken.contains(input.name) {
                problem = "There is already a server named \"\(input.name)\"."
                return
            }
            problem = nil
            saving = true
            Task {
                _ = await onAdd(input)
                saving = false
            }
        }
    }
}
