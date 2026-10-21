import DroiKit
import DroidProtocol
import Observation
import SwiftUI

/// What every new Session on the selected computer starts with. The Daemon
/// keeps it in that computer's ~/.factory/settings.json, shared with the droid
/// CLI and the Factory App.
@MainActor
@Observable
private final class SessionDefaultsEditor {
    private(set) var defaults: SessionDefaultsView?
    private(set) var error: String?
    @ObservationIgnored private let connection: ComputerConnection

    init(connection: ComputerConnection) {
        self.connection = connection
    }

    func load() async {
        do {
            let result = try await connection.controller.client().getDefaultSettings()
            defaults = SessionDefaultsView(result)
            error = nil
        } catch {
            self.error = describe(error)
        }
    }

    /// Shows the change at once; the Daemon's answer replaces it, a failure puts it back.
    func update(_ patch: SessionDefaultsPatch) async {
        error = nil
        let before = defaults
        if let before { defaults = before.applying(patch) }
        do {
            let result: UpdateSessionDefaultsResult = try await connection.controller.client().call(
                DaemonMethod.updateSessionDefaults, params: patch.params)
            defaults = SessionDefaultsView(result.defaults)
        } catch {
            defaults = before
            self.error = describe(error)
        }
    }

    private func describe(_ error: any Error) -> String {
        (error as? LocalizedError)?.errorDescription ?? String(describing: error)
    }
}

struct SessionDefaultsScreen: View {
    let connection: ComputerConnection
    @State private var editor: SessionDefaultsEditor
    @Environment(\.palette) private var palette

    init(connection: ComputerConnection) {
        self.connection = connection
        _editor = State(initialValue: SessionDefaultsEditor(connection: connection))
    }

    var body: some View {
        let name = connection.computer.name
        ScrollView {
            VStack(alignment: .leading, spacing: Space.xl) {
                Text(
                    "For new Sessions on \(name). Shared with the droid CLI and the Factory App there; existing Sessions keep their settings."
                )
                .font(.sans(FontSize.sm))
                .foregroundStyle(palette.mutedForeground)
                .padding(.horizontal, Space.xs)
                .fixedSize(horizontal: false, vertical: true)
                if let error = editor.error {
                    Text("Session defaults did not load or save: \(error)")
                        .font(.sans(FontSize.sm))
                        .foregroundStyle(palette.destructiveForeground)
                        .accessibilityAddTraits(.isStaticText)
                }
                if let defaults = editor.defaults {
                    SessionDefaultsSections(
                        defaults: defaults, preferences: connection.preferences,
                        save: { patch in Task { await editor.update(patch) } })
                } else {
                    Text(connection.state == .connected ? "Loading session defaults…" : "Connecting to \(name)…")
                        .font(.sans(FontSize.sm))
                        .foregroundStyle(palette.mutedForeground)
                }
            }
            .padding(Space.lg)
        }
        .scrollDismissesKeyboard(.interactively)
        .task(id: connection.state == .connected) {
            if connection.state == .connected { await editor.load() }
        }
    }
}

private typealias Option = (value: String, label: String)

private let sameAsMain = "__same_as_main__"
private let modelDefault = "__model_default__"
private let inherit = "inherit"
private let followDroid = "droid"
private let removeLimit = "__remove__"

private func modelOptions(_ defaults: SessionDefaultsView, routers: Bool = true, keeping selected: String? = nil)
    -> [Option]
{
    var options = pickableModels(defaults.models, routers: routers).map { (value: $0.id, label: $0.label) }
    // A model the Daemon no longer offers still shows as the current choice.
    if let selected, !selected.isEmpty, !options.contains(where: { $0.value == selected }) {
        options.append((selected, defaults.models.first { $0.id == selected }?.label ?? selected))
    }
    return options
}

private func effortOptions(_ values: [String]) -> [Option] {
    values.map { (value: $0, label: effortLabel($0)) }
}

private func limitOptions(current: Double?) -> [Option] {
    compactionLimitChoices(current: current).map { (value: String(Int($0)), label: tokenLimitLabel($0)) }
}

private struct SessionDefaultsSections: View {
    let defaults: SessionDefaultsView
    let preferences: Preferences
    let save: (SessionDefaultsPatch) -> Void

    @State private var customFolder = false

    private func editable(_ key: String, _ change: @escaping (String) -> Void) -> ((String) -> Void)? {
        defaults.locked.contains(key) ? nil : change
    }

    private func modelLabel(_ id: String) -> String { defaults.models.first { $0.id == id }?.label ?? id }

    private func specFooter(_ folder: SpecSaveChoice, _ paths: SpecSavePaths) -> String {
        switch folder {
        case .user: "Specs go to \(paths.user)."
        case .project: "Specs go to the project's \(paths.project), or \(paths.user) outside a project."
        case .custom: "Specs from every Session go to this folder."
        }
    }

    var body: some View {
        let locked = defaults.locked
        let paths = specSavePaths(userFactoryDir: defaults.userFactoryDir)
        let stored = specSaveChoice(defaults.specSaveDir, paths: paths)
        let folder: SpecSaveChoice = customFolder ? .custom : stored
        let specModel = defaults.specModeModelId ?? defaults.modelId
        let overrides = defaults.compactionTokenLimitPerModel
        let subagents = defaults.subagentModelSettings
        let autonomy = defaults.autonomyLevel ?? "off"

        VStack(alignment: .leading, spacing: Space.xl) {
            ListSection(
                title: "Model and autonomy",
                footer: !locked.isEmpty
                    ? "Settings your organization manages cannot be changed here."
                    : "\(autonomyLabel(autonomy)): \((autonomyDescriptions[autonomy] ?? "").lowercased())."
            ) {
                ListPicker(
                    label: "Default model", choices: modelOptions(defaults, keeping: defaults.modelId),
                    selection: defaults.modelId ?? "",
                    onChange: editable("modelId") { modelId in
                        let supported = reasoningChoices(defaults.models, modelId: modelId, current: nil)
                        let effort = defaults.reasoningEffort
                        let keep = effort.map(supported.contains) ?? false
                        save(SessionDefaultsPatch(modelId: modelId, reasoningEffort: keep ? nil : supported.first))
                    })
                ListPicker(
                    label: "Reasoning level",
                    choices: effortOptions(
                        reasoningChoices(defaults.models, modelId: defaults.modelId, current: defaults.reasoningEffort)),
                    selection: defaults.reasoningEffort ?? "",
                    onChange: editable("reasoningEffort") { save(SessionDefaultsPatch(reasoningEffort: $0)) })
                ListPicker(
                    label: "Interaction mode", choices: interactionModes.map { (value: $0.value, label: $0.label) },
                    selection: defaults.interactionMode,
                    onChange: editable("interactionMode") { save(SessionDefaultsPatch(interactionMode: $0)) })
                ListPicker(
                    label: "Autonomy",
                    choices: defaults.availableAutonomyLevels.map { (value: $0, label: autonomyLabel($0)) },
                    selection: autonomy,
                    onChange: editable("autonomyLevel") { save(SessionDefaultsPatch(autonomyLevel: $0)) })
            }

            ListSection(
                title: "Tool calls",
                footer:
                    "Script lets the model call tools from a small program that can batch and filter them. A Session keeps the mode it starts with. Kept on this phone."
            ) {
                ListPicker(
                    label: "Mode",
                    choices: [(value: followDroid, label: "Follow droid settings")]
                        + ToolMode.allCases.map { (value: $0.rawValue, label: $0.label) },
                    selection: preferences.toolMode?.rawValue ?? followDroid,
                    onChange: { preferences.toolMode = ToolMode(rawValue: $0) })
            }

            ListSection(
                title: "Spec mode",
                footer: specFooter(folder, paths)
            ) {
                ListPicker(
                    label: "Spec model",
                    choices: [(value: sameAsMain, label: "Same as main")]
                        + modelOptions(defaults, keeping: defaults.specModeModelId),
                    selection: defaults.specModeModelId ?? sameAsMain,
                    onChange: editable("specModeModelId") { value in
                        save(
                            SessionDefaultsPatch(
                                specModeModelId: .some(value == sameAsMain ? nil : value),
                                specModeReasoningEffort: .some(nil)))
                    })
                ListPicker(
                    label: "Spec reasoning level",
                    choices: [(value: sameAsMain, label: "Same as main")]
                        + effortOptions(
                            reasoningChoices(
                                defaults.models, modelId: specModel,
                                current: defaults.specModeReasoningEffort ?? defaults.reasoningEffort)),
                    selection: defaults.specModeReasoningEffort ?? sameAsMain,
                    onChange: editable("specModeReasoningEffort") { value in
                        save(SessionDefaultsPatch(specModeReasoningEffort: .some(value == sameAsMain ? nil : value)))
                    })
                ListPicker(
                    label: "Spec folder",
                    choices: [
                        (value: SpecSaveChoice.user.rawValue, label: "User home"),
                        (value: SpecSaveChoice.project.rawValue, label: "Project"),
                        (value: SpecSaveChoice.custom.rawValue, label: "Custom folder"),
                    ],
                    selection: folder.rawValue,
                    onChange: editable("specSaveDir") { value in
                        customFolder = value == SpecSaveChoice.custom.rawValue
                        if value == SpecSaveChoice.user.rawValue { save(SessionDefaultsPatch(specSaveDir: .some(nil))) }
                        if value == SpecSaveChoice.project.rawValue {
                            save(SessionDefaultsPatch(specSaveDir: .some(paths.project)))
                        }
                    })
            }
            if folder == .custom {
                CustomSpecFolder(stored: stored == .custom ? (defaults.specSaveDir ?? "") : "") {
                    save(SessionDefaultsPatch(specSaveDir: .some($0)))
                }
                .id(defaults.specSaveDir ?? "")
            }

            ListSection(
                title: "Compaction", footer: "New Sessions compact their history once it passes the token limit."
            ) {
                ListSwitch(
                    label: "Compact automatically",
                    isOn: Binding(
                        get: { defaults.compactionThresholdCheckEnabled },
                        set: { save(SessionDefaultsPatch(compactionThresholdCheckEnabled: $0)) })
                )
                .disabled(locked.contains("compactionThresholdCheckEnabled"))
                ListPicker(
                    label: "Token limit", choices: limitOptions(current: defaults.compactionTokenLimit),
                    selection: String(Int(defaults.compactionTokenLimit ?? defaultCompactionLimit)),
                    onChange: editable("compactionTokenLimit") { value in
                        if let tokens = Double(value) { save(SessionDefaultsPatch(compactionTokenLimit: tokens)) }
                    })
                ListPicker(
                    label: "Compaction model",
                    choices: [(value: currentModel, label: "Current model")]
                        + modelOptions(defaults, routers: false, keeping: defaults.compactionModel == currentModel ? nil : defaults.compactionModel),
                    selection: defaults.compactionModel,
                    onChange: editable("compactionModel") { save(SessionDefaultsPatch(compactionModel: $0)) })
            }

            ListSection(title: "Limits for specific models") {
                ForEach(overrides.keys.sorted(), id: \.self) { modelId in
                    let tokens = overrides[modelId] ?? defaultCompactionLimit
                    ListPicker(
                        label: modelLabel(modelId),
                        choices: [(value: removeLimit, label: "Remove this limit")] + limitOptions(current: tokens),
                        selection: String(Int(tokens)),
                        onChange: editable("compactionTokenLimitPerModel") { value in
                            var next = overrides
                            if value == removeLimit {
                                next[modelId] = nil
                            } else if let limit = Double(value) {
                                next[modelId] = limit
                            }
                            save(SessionDefaultsPatch(compactionTokenLimitPerModel: next))
                        })
                }
                MenuRow(
                    label: "Add a model limit",
                    options: modelOptions(defaults).filter { overrides[$0.value] == nil },
                    enabled: !locked.contains("compactionTokenLimitPerModel")
                ) { modelId in
                    var next = overrides
                    next[modelId] = defaults.compactionTokenLimit ?? defaultCompactionLimit
                    save(SessionDefaultsPatch(compactionTokenLimitPerModel: next))
                }
            }

            ListSection(
                title: "Subagents",
                footer: "The model and reasoning level for subagents started with light, medium or heavy complexity."
            ) {
                ListPicker(
                    label: "Subagent autonomy",
                    choices: [(value: inherit, label: "Inherit")]
                        + defaults.availableAutonomyLevels.map { (value: $0, label: autonomyLabel($0)) },
                    selection: defaults.subagentAutonomyLevel,
                    onChange: editable("subagentAutonomyLevel") { save(SessionDefaultsPatch(subagentAutonomyLevel: $0)) })
                ForEach(SubagentTier.allCases, id: \.self) { tier in
                    let model = subagents.model(for: tier)
                    let effort = subagents.reasoningEffort(for: tier)?.rawValue
                    ListPicker(
                        label: "\(tier.label) model",
                        choices: [(value: inherit, label: "Inherit")] + modelOptions(defaults, keeping: model),
                        selection: model ?? inherit,
                        onChange: editable("subagent.\(tier.rawValue)Model") { value in
                            save(
                                SessionDefaultsPatch(
                                    subagentModelSettings: withSubagentTier(
                                        subagents, tier, .model(value == inherit ? nil : value))))
                        })
                    if let model {
                        ListPicker(
                            label: "\(tier.label) reasoning",
                            choices: [(value: modelDefault, label: "Model default")]
                                + effortOptions(reasoningChoices(defaults.models, modelId: model, current: effort)),
                            selection: effort ?? modelDefault,
                            onChange: editable("subagent.\(tier.rawValue)ReasoningEffort") { value in
                                save(
                                    SessionDefaultsPatch(
                                        subagentModelSettings: withSubagentTier(
                                            subagents, tier, .reasoningEffort(value == modelDefault ? nil : value))))
                            })
                    }
                }
            }
        }
    }
}

/// A row that only offers a menu of things to add.
private struct MenuRow: View {
    let label: String
    let options: [Option]
    var enabled = true
    let onPick: (String) -> Void
    @Environment(\.palette) private var palette

    var body: some View {
        HStack(spacing: Space.sm) {
            Text(label).font(.sans(FontSize.base)).foregroundStyle(palette.foreground)
            Spacer(minLength: Space.sm)
            if enabled, !options.isEmpty {
                Menu {
                    ForEach(options, id: \.value) { option in
                        Button(option.label) { onPick(option.value) }
                    }
                } label: {
                    Image(systemName: "plus").font(.system(size: 13, weight: .medium))
                        .foregroundStyle(palette.mutedForeground)
                        .frame(width: 32, height: 32)
                        .contentShape(Rectangle())
                }
                .accessibilityLabel(label)
            }
        }
        .padding(.horizontal, Space.lg)
        .frame(minHeight: 46)
    }
}

private struct CustomSpecFolder: View {
    let stored: String
    let onSave: (String) -> Void
    @State private var value: String

    init(stored: String, onSave: @escaping (String) -> Void) {
        self.stored = stored
        self.onSave = onSave
        _value = State(initialValue: stored)
    }

    var body: some View {
        let trimmed = value.trimmingCharacters(in: .whitespacesAndNewlines)
        VStack(spacing: Space.sm) {
            DroiTextField(label: "Spec folder path", text: $value, placeholder: "/Users/you/specs", mono: true) {
                if !trimmed.isEmpty, trimmed != stored { onSave(trimmed) }
            }
            DroiButton(title: "Save folder", variant: .secondary, disabled: trimmed.isEmpty || trimmed == stored) {
                onSave(trimmed)
            }
        }
    }
}
