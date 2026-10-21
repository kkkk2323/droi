// The Client's text size, kept per device like the theme.
public enum TextSize: String, Sendable, Hashable, CaseIterable, Codable {
    case small, `default`, large, largest

    public var label: String {
        switch self {
        case .small: "Small"
        case .default: "Default"
        case .large: "Large"
        case .largest: "Largest"
        }
    }

    /// How much this size scales the transcript and composer text, relative to the default.
    public var scale: Double {
        switch self {
        case .small: 0.90625
        case .default: 1
        case .large: 1.09375
        case .largest: 1.1875
        }
    }
}
