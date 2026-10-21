// What every new Session starts with, as the Daemon keeps it in
// ~/.factory/settings.json (shared with the droid CLI and the Factory App):
// the view the settings pages show, and the pure rules they edit it by.
import DroidProtocol
import Foundation

public enum SubagentTier: String, Sendable, Hashable, CaseIterable {
    case light, medium, heavy

    public var label: String {
        switch self {
        case .light: "Light task"
        case .medium: "Medium task"
        case .heavy: "Heavy task"
        }
    }

    var model: WritableKeyPath<SubagentModelSettings, String?> {
        switch self {
        case .light: \.lightModel
        case .medium: \.mediumModel
        case .heavy: \.heavyModel
        }
    }

    var reasoningEffort: WritableKeyPath<SubagentModelSettings, ReasoningEffort?> {
        switch self {
        case .light: \.lightReasoningEffort
        case .medium: \.mediumReasoningEffort
        case .heavy: \.heavyReasoningEffort
        }
    }
}

extension SubagentModelSettings {
    public func model(for tier: SubagentTier) -> String? { self[keyPath: tier.model] }
    public func reasoningEffort(for tier: SubagentTier) -> ReasoningEffort? { self[keyPath: tier.reasoningEffort] }
}

public struct SessionDefaultsView: Sendable, Hashable {
    public var modelId: String?
    public var reasoningEffort: String?
    public var interactionMode: String
    public var autonomyLevel: String?
    public var specModeModelId: String?
    public var specModeReasoningEffort: String?
    public var specSaveDir: String?
    public var compactionTokenLimit: Double?
    public var compactionTokenLimitPerModel: [String: Double]
    public var compactionModel: String
    public var compactionThresholdCheckEnabled: Bool
    public var subagentAutonomyLevel: String
    public var subagentModelSettings: SubagentModelSettings
    public var models: [ModelChoice]
    public var availableAutonomyLevels: [String]
    /// Where the Daemon keeps the user's own files (~/.factory), for the spec folder choices.
    public var userFactoryDir: String?
    /// Settings the organization manages; shown but not editable. Nested ones are dotted: `subagent.lightModel`.
    public var locked: Set<String>

    /// The Daemon's `get_default_settings` answer (or `update_session_defaults`' `defaults`) as the view.
    public init(raw: JSONValue) {
        func str(_ key: String) -> String? { raw[key]?.stringValue }
        modelId = str("modelId")
        reasoningEffort = str("reasoningEffort")
        interactionMode = str("interactionMode") ?? "auto"
        autonomyLevel = str("autonomyLevel")
        specModeModelId = str("specModeModelId")
        specModeReasoningEffort = str("specModeReasoningEffort")
        specSaveDir = str("specSaveDir")
        compactionTokenLimit = raw["compactionTokenLimit"]?.doubleValue
        compactionTokenLimitPerModel = (raw["compactionTokenLimitPerModel"]?.objectValue ?? [:])
            .compactMapValues(\.doubleValue)
        compactionModel = str("compactionModel") ?? currentModel
        compactionThresholdCheckEnabled = raw["compactionThresholdCheckEnabled"] != .bool(false)
        subagentAutonomyLevel = str("subagentAutonomyLevel") ?? "inherit"
        subagentModelSettings =
            (try? raw["subagentModelSettings"]?.decode(SubagentModelSettings.self)) ?? SubagentModelSettings()
        models = toModelChoices(raw["availableModels"]?.arrayValue ?? [])
        availableAutonomyLevels =
            raw["availableAutonomyLevels"]?.arrayValue?.compactMap(\.stringValue) ?? autonomyLevels
        userFactoryDir = raw["specSavePresets"]?["userFactoryDir"]?.stringValue
        locked = lockedKeys(raw["management"])
    }

    public init(_ result: GetDefaultSettingsResult) {
        self.init(raw: (try? JSONValue(encoding: result)) ?? .object([:]))
    }

    /// The view after a patch, before the Daemon confirms it.
    public func applying(_ patch: SessionDefaultsPatch) -> SessionDefaultsView {
        var next = self
        if let value = patch.modelId { next.modelId = value }
        if let value = patch.reasoningEffort { next.reasoningEffort = value }
        if let value = patch.interactionMode { next.interactionMode = value }
        if let value = patch.autonomyLevel { next.autonomyLevel = value }
        if let value = patch.specModeModelId { next.specModeModelId = value }
        if let value = patch.specModeReasoningEffort { next.specModeReasoningEffort = value }
        if let value = patch.specSaveDir { next.specSaveDir = value }
        if let value = patch.compactionTokenLimit { next.compactionTokenLimit = value }
        if let value = patch.compactionTokenLimitPerModel { next.compactionTokenLimitPerModel = value }
        if let value = patch.compactionModel { next.compactionModel = value }
        if let value = patch.compactionThresholdCheckEnabled { next.compactionThresholdCheckEnabled = value }
        if let value = patch.subagentAutonomyLevel { next.subagentAutonomyLevel = value }
        if let value = patch.subagentModelSettings { next.subagentModelSettings = value }
        return next
    }
}

/// `management` marks org-managed keys `{disabled: true}`; nested groups become `subagent.lightModel`.
private func lockedKeys(_ management: JSONValue?, prefix: String = "") -> Set<String> {
    guard case .object(let object)? = management else { return [] }
    var locked: Set<String> = []
    for (key, value) in object {
        guard case .object(let entry) = value else { continue }
        if let disabled = entry["disabled"] {
            if disabled == .bool(true) { locked.insert(prefix + key) }
        } else {
            locked.formUnion(lockedKeys(value, prefix: "\(prefix)\(key)."))
        }
    }
    return locked
}

/// What `daemon.update_session_defaults` takes. For the three "same as main"
/// choices, `.some(nil)` clears the setting.
public struct SessionDefaultsPatch: Sendable, Hashable {
    public var modelId: String?
    public var reasoningEffort: String?
    public var interactionMode: String?
    public var autonomyLevel: String?
    public var specModeModelId: String??
    public var specModeReasoningEffort: String??
    public var specSaveDir: String??
    public var compactionTokenLimit: Double?
    public var compactionTokenLimitPerModel: [String: Double]?
    public var compactionModel: String?
    public var compactionThresholdCheckEnabled: Bool?
    public var subagentAutonomyLevel: String?
    /// The Daemon replaces it whole (see withSubagentTier).
    public var subagentModelSettings: SubagentModelSettings?

    public init(
        modelId: String? = nil, reasoningEffort: String? = nil, interactionMode: String? = nil,
        autonomyLevel: String? = nil, specModeModelId: String?? = nil,
        specModeReasoningEffort: String?? = nil, specSaveDir: String?? = nil,
        compactionTokenLimit: Double? = nil, compactionTokenLimitPerModel: [String: Double]? = nil,
        compactionModel: String? = nil, compactionThresholdCheckEnabled: Bool? = nil,
        subagentAutonomyLevel: String? = nil, subagentModelSettings: SubagentModelSettings? = nil
    ) {
        self.modelId = modelId
        self.reasoningEffort = reasoningEffort
        self.interactionMode = interactionMode
        self.autonomyLevel = autonomyLevel
        self.specModeModelId = specModeModelId
        self.specModeReasoningEffort = specModeReasoningEffort
        self.specSaveDir = specSaveDir
        self.compactionTokenLimit = compactionTokenLimit
        self.compactionTokenLimitPerModel = compactionTokenLimitPerModel
        self.compactionModel = compactionModel
        self.compactionThresholdCheckEnabled = compactionThresholdCheckEnabled
        self.subagentAutonomyLevel = subagentAutonomyLevel
        self.subagentModelSettings = subagentModelSettings
    }

    /// The request's params, with a JSON null for each cleared choice.
    public var params: JSONValue {
        var object: [String: JSONValue] = [:]
        func set(_ key: String, _ value: String?) { if let value { object[key] = .string(value) } }
        func clearable(_ key: String, _ value: String??) {
            if let value { object[key] = value.map(JSONValue.string) ?? .null }
        }
        set("modelId", modelId)
        set("reasoningEffort", reasoningEffort)
        set("interactionMode", interactionMode)
        set("autonomyLevel", autonomyLevel)
        clearable("specModeModelId", specModeModelId)
        clearable("specModeReasoningEffort", specModeReasoningEffort)
        clearable("specSaveDir", specSaveDir)
        if let compactionTokenLimit { object["compactionTokenLimit"] = .number(compactionTokenLimit) }
        if let compactionTokenLimitPerModel {
            object["compactionTokenLimitPerModel"] = .object(compactionTokenLimitPerModel.mapValues(JSONValue.number))
        }
        set("compactionModel", compactionModel)
        if let compactionThresholdCheckEnabled {
            object["compactionThresholdCheckEnabled"] = .bool(compactionThresholdCheckEnabled)
        }
        set("subagentAutonomyLevel", subagentAutonomyLevel)
        if let subagentModelSettings, let value = try? JSONValue(encoding: subagentModelSettings) {
            object["subagentModelSettings"] = value
        }
        return .object(object)
    }
}

public let interactionModes: [(value: String, label: String)] = [("auto", "Auto"), ("spec", "Spec")]

/// The compaction limits Factory offers; a stored value outside them is still shown.
public let compactionLimits: [Double] = [
    100_000, 200_000, 250_000, 300_000, 400_000, 500_000, 600_000, 700_000, 800_000, 900_000, 1_000_000,
]
public let defaultCompactionLimit: Double = 250_000
/// `compactionModel` for "compact with the Session's own model".
public let currentModel = "current-model"

/// `250000` → `250K`, `1000000` → `1M`.
public func tokenLimitLabel(_ tokens: Double) -> String {
    tokens >= 1_000_000 ? "\(JSONText.number(tokens / 1_000_000))M" : "\(JSONText.number(tokens / 1_000))K"
}

/// The limits a picker offers: Factory's, with the stored one among them, smallest first.
public func compactionLimitChoices(current: Double?) -> [Double] {
    Array(Set(compactionLimits + (current.map { [$0] } ?? []))).sorted()
}

/// The reasoning levels a model supports. Without the model (none chosen, or
/// one the Daemon no longer lists) the current value is all there is.
public func reasoningChoices(_ models: [ModelChoice], modelId: String?, current: String?) -> [String] {
    let supported = models.first { $0.id == modelId }?.reasoningEfforts ?? []
    if !supported.isEmpty { return supported }
    return [current ?? "medium"]
}

/// Models a setting may pick: listed and not disabled; the router is no use for compaction.
public func pickableModels(_ models: [ModelChoice], routers: Bool = true) -> [ModelChoice] {
    models.filter { !$0.disabled && (routers || $0.provider != nil) }
}

public enum SubagentTierChange: Sendable, Hashable {
    case model(String?)
    case reasoningEffort(String?)
}

/// The Daemon replaces `subagentModelSettings` as a whole, so a change to one
/// tier sends every tier. A nil model hands the tier back to the calling
/// Session's model ("Inherit") and drops its reasoning level with it; a nil
/// level leaves the tier on its model's own default.
public func withSubagentTier(
    _ settings: SubagentModelSettings, _ tier: SubagentTier, _ change: SubagentTierChange
) -> SubagentModelSettings {
    var next = settings
    switch change {
    case .model(let model):
        next[keyPath: tier.reasoningEffort] = nil
        next[keyPath: tier.model] = model
    case .reasoningEffort(let effort):
        next[keyPath: tier.reasoningEffort] = effort.map(ReasoningEffort.init(rawValue:))
    }
    return next
}

public enum SpecSaveChoice: String, Sendable, Hashable {
    case user, project, custom
}

public struct SpecSavePaths: Sendable, Hashable {
    public var user: String
    public var project: String
}

/// Where spec files go, as the Factory App offers it: under the user's
/// ~/.factory/docs (the Daemon's default, stored as nothing), under the
/// project's .factory/docs (a path relative to the Workspace), or a folder of
/// the user's choosing.
public func specSavePaths(userFactoryDir: String?) -> SpecSavePaths {
    let folder =
        userFactoryDir?.split(whereSeparator: { $0 == "/" || $0 == "\\" }).last.map(String.init) ?? ".factory"
    return SpecSavePaths(user: "~/\(folder)/docs", project: "\(folder)/docs")
}

public func specSaveChoice(_ stored: String?, paths: SpecSavePaths) -> SpecSaveChoice {
    if stored == nil || stored == paths.user { return .user }
    if stored == paths.project { return .project }
    return .custom
}
