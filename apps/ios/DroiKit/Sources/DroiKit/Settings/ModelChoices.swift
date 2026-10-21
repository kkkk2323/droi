// The model picker's rows and the settings' labels, for every Client.
import DroidProtocol
import Foundation

public struct ModelChoice: Sendable, Hashable, Identifiable {
    public var id: String
    public var label: String
    public var reasoningEfforts: [String]
    public var disabled: Bool
    /// Provider id from the Daemon (anthropic, openai, ...); nil for the Auto router.
    public var provider: String?
    /// Factory's usage multiplier for the model; nil for custom (BYOK) models.
    public var multiplier: Double?

    public init(
        id: String, label: String, reasoningEfforts: [String] = [], disabled: Bool = false,
        provider: String? = nil, multiplier: Double? = nil
    ) {
        self.id = id
        self.label = label
        self.reasoningEfforts = reasoningEfforts
        self.disabled = disabled
        self.provider = provider
        self.multiplier = multiplier
    }

    public var brand: Brand { brandOf(id, provider: provider) }
}

/// The Daemon's model list (a Session's available models, or the defaults'
/// `availableModels`) as picker rows.
public func toModelChoices(_ models: [JSONValue]) -> [ModelChoice] {
    models.compactMap { model in
        guard let id = model["id"]?.stringValue else { return nil }
        let custom = model["isCustom"]?.boolValue == true
        return ModelChoice(
            id: id,
            label: model["displayName"]?.stringValue ?? id,
            reasoningEfforts: model["supportedReasoningEfforts"]?.arrayValue?.compactMap(\.stringValue) ?? [],
            disabled: model["disabled"]?.boolValue == true,
            provider: model["kind"]?.stringValue == "router" ? nil : model["modelProvider"]?.stringValue,
            multiplier: custom ? nil : model["tokenMultiplier"]?.doubleValue)
    }
}

/// Which rows the picker lists: everything, the starred ones, or one brand.
public enum PickerFilter: Sendable, Hashable {
    case all
    case favorites
    case brand(Brand)
}

/// Rows in display order. A non-empty query searches every model regardless of
/// the filter; favorites keep the order they were starred in.
public func visibleModels(
    _ models: [ModelChoice], favorites: [String], filter: PickerFilter, query: String
) -> [ModelChoice] {
    let tokens = query.lowercased().split(whereSeparator: \.isWhitespace).map(String.init)
    if !tokens.isEmpty {
        return models.filter { row in
            let haystack = "\(row.label) \(row.id) \(row.brand.label)".lowercased()
            return tokens.allSatisfy { haystack.contains($0) }
        }
    }
    switch filter {
    case .all:
        return models
    case .favorites:
        return favorites.compactMap { id in models.first { $0.id == id } }
    case .brand(let brand):
        return models.filter { $0.brand == brand }
    }
}

/// Brands present in the list, in rail order.
public func brandsOf(_ models: [ModelChoice]) -> [Brand] {
    let present = Set(models.map(\.brand))
    return Brand.allCases.filter(present.contains)
}

/// `1.6` → `1.6×`, as Factory shows its usage multipliers.
public func formatMultiplier(_ multiplier: Double) -> String {
    "\(JSONText.number((multiplier * 100).rounded() / 100))×"
}

public let effortLabels: [String: String] = [
    "none": "None",
    "dynamic": "Dynamic",
    "off": "Off",
    "minimal": "Minimal",
    "low": "Low",
    "medium": "Medium",
    "high": "High",
    "xhigh": "Extra high",
    "max": "Max",
]

public func effortLabel(_ effort: String) -> String {
    effortLabels[effort] ?? effort
}

public let autonomyLevels = ["off", "low", "medium", "high"]

public let autonomyLabels: [String: String] = [
    "off": "Ask for everything",
    "low": "Low autonomy",
    "medium": "Medium autonomy",
    "high": "High autonomy",
]

public let autonomyDescriptions: [String: String] = [
    "off": "Require approval for all actions",
    "low": "Allow file edits and read-only commands",
    "medium": "Allow reversible commands",
    "high": "Allow all commands",
]

public func autonomyLabel(_ level: String) -> String {
    autonomyLabels[level] ?? level
}
