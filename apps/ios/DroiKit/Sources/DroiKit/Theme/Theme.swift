// The design tokens: the web Client's colours (oklch translated to sRGB),
// its rem radii as points, and its type scale. Plain data; ThemeSwiftUI.swift
// turns them into SwiftUI colours.

public enum ThemeScheme: String, Sendable, Hashable {
    case light, dark
}

public enum ThemeName: String, Sendable, Hashable, CaseIterable {
    case light
    case dark
    case solarizedLight = "solarized-light"

    /// Whether a theme is drawn light or dark: the status bar and the system's own sheets follow it.
    public var scheme: ThemeScheme { self == .dark ? .dark : .light }

    public var colors: ThemeColors {
        switch self {
        case .light: .light
        case .dark: .dark
        case .solarizedLight: .solarizedLight
        }
    }
}

/// An sRGB colour, components 0...1.
public struct ThemeColor: Sendable, Hashable, CustomStringConvertible {
    public var red: Double
    public var green: Double
    public var blue: Double
    public var alpha: Double

    public init(red: Double, green: Double, blue: Double, alpha: Double = 1) {
        self.red = red
        self.green = green
        self.blue = blue
        self.alpha = alpha
    }

    /// `0xRRGGBB`.
    public init(hex: UInt32, alpha: Double = 1) {
        self.init(
            red: Double((hex >> 16) & 0xFF) / 255, green: Double((hex >> 8) & 0xFF) / 255,
            blue: Double(hex & 0xFF) / 255, alpha: alpha)
    }

    /// CSS `rgba(r, g, b, a)` with 0...255 channels.
    public init(rgb red: Int, _ green: Int, _ blue: Int, alpha: Double) {
        self.init(red: Double(red) / 255, green: Double(green) / 255, blue: Double(blue) / 255, alpha: alpha)
    }

    /// `#rrggbb`, or CSS `rgba(…)` when not opaque.
    public var description: String {
        let channels = [red, green, blue].map { Int(($0 * 255).rounded()) }
        if alpha < 1 {
            return "rgba(\(channels[0]), \(channels[1]), \(channels[2]), \(JSONText.number(alpha)))"
        }
        return "#" + channels.map { ($0 < 16 ? "0" : "") + String($0, radix: 16) }.joined()
    }
}

public struct ThemeColors: Sendable, Hashable {
    public var background: ThemeColor
    public var foreground: ThemeColor
    public var card: ThemeColor
    public var popover: ThemeColor
    public var primary: ThemeColor
    public var primaryForeground: ThemeColor
    public var secondary: ThemeColor
    public var muted: ThemeColor
    public var mutedForeground: ThemeColor
    public var accent: ThemeColor
    public var destructive: ThemeColor
    public var destructiveForeground: ThemeColor
    public var border: ThemeColor
    public var input: ThemeColor
    public var ring: ThemeColor
    public var sidebar: ThemeColor
    public var sidebarForeground: ThemeColor
    public var sidebarAccent: ThemeColor
    public var sidebarBorder: ThemeColor
    public var codeBackground: ThemeColor
    /// Inline code: mono type in a warm ink reads as code without a pill behind it.
    public var codeInline: ThemeColor
    // Status only, as the web Client uses them.
    public var success: ThemeColor
    public var attention: ThemeColor
    public var working: ThemeColor
    public var unread: ThemeColor
    public var addedBackground: ThemeColor
    public var removedBackground: ThemeColor
    public var backdrop: ThemeColor

    public static let light = ThemeColors(
        background: .init(hex: 0xffffff),
        foreground: .init(hex: 0x161616),
        card: .init(hex: 0xf0f0f0),
        popover: .init(hex: 0xffffff),
        primary: .init(hex: 0x161616),
        primaryForeground: .init(hex: 0xfafafa),
        secondary: .init(hex: 0xebebeb),
        muted: .init(hex: 0xf0f0f0),
        mutedForeground: .init(hex: 0x636363),
        accent: .init(hex: 0xe6e6e6),
        destructive: .init(hex: 0xe7000b),
        destructiveForeground: .init(hex: 0xc20000),
        border: .init(hex: 0xdedede),
        input: .init(hex: 0xd7d7d7),
        ring: .init(hex: 0x808080),
        sidebar: .init(hex: 0xf0f0f0),
        sidebarForeground: .init(hex: 0x2e2e2e),
        sidebarAccent: .init(hex: 0xdfdfdf),
        sidebarBorder: .init(hex: 0xdedede),
        codeBackground: .init(hex: 0xf0f0f0),
        codeInline: .init(hex: 0x9a5528),
        // emerald-600, amber-600, sky-600 for Working, and sky-500 for the unread dot.
        success: .init(hex: 0x009966),
        attention: .init(hex: 0xdd7400),
        working: .init(hex: 0x0084d1),
        unread: .init(hex: 0x00a6f4),
        // emerald-500 and rose-500 at 12%.
        addedBackground: .init(rgb: 16, 185, 129, alpha: 0.12),
        removedBackground: .init(rgb: 244, 63, 94, alpha: 0.12),
        backdrop: .init(rgb: 0, 0, 0, alpha: 0.3))

    public static let dark = ThemeColors(
        background: .init(hex: 0x161616),
        foreground: .init(hex: 0xeeeeee),
        card: .init(hex: 0x222222),
        popover: .init(hex: 0x1d1d1d),
        primary: .init(hex: 0xeeeeee),
        primaryForeground: .init(hex: 0x161616),
        secondary: .init(hex: 0x292929),
        muted: .init(hex: 0x222222),
        mutedForeground: .init(hex: 0x989898),
        accent: .init(hex: 0x2e2e2e),
        destructive: .init(hex: 0x82181a),
        destructiveForeground: .init(hex: 0xff645f),
        border: .init(hex: 0x2e2e2e),
        input: .init(hex: 0x333333),
        ring: .init(hex: 0x636363),
        sidebar: .init(hex: 0x0e0e0e),
        sidebarForeground: .init(hex: 0xbebebe),
        sidebarAccent: .init(hex: 0x242424),
        sidebarBorder: .init(hex: 0x242424),
        codeBackground: .init(hex: 0x262626),
        codeInline: .init(hex: 0xe0a882),
        // emerald-400, amber-400, sky-400 and sky-500.
        success: .init(hex: 0x00d492),
        attention: .init(hex: 0xffb900),
        working: .init(hex: 0x00bcff),
        unread: .init(hex: 0x00a6f4),
        addedBackground: .init(rgb: 16, 185, 129, alpha: 0.12),
        removedBackground: .init(rgb: 244, 63, 94, alpha: 0.12),
        backdrop: .init(rgb: 0, 0, 0, alpha: 0.5))

    // Solarized Light+ as the web Client draws it.
    public static let solarizedLight = ThemeColors(
        background: .init(hex: 0xfdf6e3),
        foreground: .init(hex: 0x586e75),
        card: .init(hex: 0xeee8d5),
        popover: .init(hex: 0xeee8d5),
        primary: .init(hex: 0xac9d57),
        primaryForeground: .init(hex: 0xffffff),
        secondary: .init(hex: 0xeee8d5),
        muted: .init(hex: 0xeee8d5),
        mutedForeground: .init(hex: 0x657b83),
        accent: .init(hex: 0xddd6c1),
        destructive: .init(hex: 0xdc322f),
        destructiveForeground: .init(hex: 0xdc322f),
        border: .init(hex: 0xddd6c1),
        input: .init(hex: 0xddd6c1),
        ring: .init(hex: 0xd3af86),
        sidebar: .init(hex: 0xeee8d5),
        sidebarForeground: .init(hex: 0x616161),
        sidebarAccent: .init(hex: 0xd1cbb8),
        sidebarBorder: .init(hex: 0xddd6c1),
        codeBackground: .init(hex: 0xeee8d5),
        codeInline: .init(hex: 0x2aa198),
        success: .init(hex: 0x859900),
        attention: .init(hex: 0xb58900),
        working: .init(hex: 0x268bd2),
        unread: .init(hex: 0x268bd2),
        addedBackground: .init(rgb: 133, 153, 0, alpha: 0.12),
        removedBackground: .init(rgb: 220, 50, 47, alpha: 0.12),
        backdrop: .init(rgb: 0, 0, 0, alpha: 0.3))
}

/// Corner radii in points; --radius is 0.625rem at the web Client's 16px root.
public enum Radius {
    public static let sm = 6.0
    public static let md = 8.0
    public static let lg = 10.0
    public static let xl = 14.0
    public static let full = 999.0
    /// The composer's card, a little rounder than the rest.
    public static let composer = xl + 4
}

/// Spacing in points.
public enum Space {
    public static let xs = 4.0
    public static let sm = 8.0
    public static let md = 12.0
    public static let lg = 16.0
    public static let xl = 24.0
}

/// Type sizes in points; the web Client's html font-size is 14px, its text-sm and text-xs follow.
public enum FontSize {
    public static let xs = 12.0
    public static let sm = 13.0
    public static let base = 15.0
    public static let lg = 17.0
    public static let xl = 20.0
    /// Prose line height in the transcript.
    public static let proseLineHeight = 22.0
}

/// The bundled Geist faces (variable fonts); Geist for the UI, Geist Mono for code.
public enum FontFamily {
    public static let sans = "Geist"
    public static let mono = "Geist Mono"
}
