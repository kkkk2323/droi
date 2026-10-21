// Which company a model comes from, for grouping and the brand mark. The
// Daemon's `modelProvider` is the wire dialect (a GLM served through Factory
// still says "anthropic" or "generic-chat-completion-api"), so the model id's
// prefix wins and the provider is only a fallback.
public enum Brand: String, Sendable, Hashable, CaseIterable {
    // Declared in the picker's rail order; brands the Daemon does not offer are skipped.
    case anthropic, openai, google, xai, zhipu, moonshot, deepseek, minimax, other

    public var label: String {
        switch self {
        case .anthropic: "Anthropic"
        case .openai: "OpenAI"
        case .google: "Google"
        case .xai: "xAI"
        case .zhipu: "Zhipu"
        case .moonshot: "Moonshot"
        case .deepseek: "DeepSeek"
        case .minimax: "MiniMax"
        case .other: "Other"
        }
    }
}

private let idPrefixes: [(String, Brand)] = [
    ("claude", .anthropic),
    ("gpt", .openai),
    ("o1", .openai),
    ("o3", .openai),
    ("o4", .openai),
    ("gemini", .google),
    ("grok", .xai),
    ("glm", .zhipu),
    ("kimi", .moonshot),
    ("deepseek", .deepseek),
    ("minimax", .minimax),
]

private let providerBrands: [String: Brand] = [
    "anthropic": .anthropic,
    "openai": .openai,
    "google": .google,
    "xai": .xai,
]

public func brandOf(_ modelId: String, provider: String?) -> Brand {
    let id = modelId.lowercased()
    for (prefix, brand) in idPrefixes where id.hasPrefix(prefix) {
        return brand
    }
    return provider.flatMap { providerBrands[$0] } ?? .other
}
