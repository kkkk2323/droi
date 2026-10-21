import SwiftUI
import UIKit

/// The colours of a theme, translated from the web Client's global.css.
struct Palette: Equatable {
    var background, foreground, card, popover, primary, primaryForeground, secondary, muted,
        mutedForeground, accent, destructive, destructiveForeground, border, input, ring, sidebar,
        sidebarForeground, sidebarAccent, sidebarBorder, codeBackground, codeInline, success, attention,
        working, unread, addedBackground, removedBackground, backdrop: Color

    static let light = Palette(
        background: .hex(0xffffff), foreground: .hex(0x161616), card: .hex(0xf0f0f0), popover: .hex(0xffffff),
        primary: .hex(0x161616), primaryForeground: .hex(0xfafafa), secondary: .hex(0xebebeb),
        muted: .hex(0xf0f0f0), mutedForeground: .hex(0x636363), accent: .hex(0xe6e6e6),
        destructive: .hex(0xe7000b), destructiveForeground: .hex(0xc20000), border: .hex(0xdedede),
        input: .hex(0xd7d7d7), ring: .hex(0x808080), sidebar: .hex(0xf0f0f0), sidebarForeground: .hex(0x2e2e2e),
        sidebarAccent: .hex(0xdfdfdf), sidebarBorder: .hex(0xdedede), codeBackground: .hex(0xf0f0f0),
        codeInline: .hex(0x9a5528), success: .hex(0x009966), attention: .hex(0xdd7400), working: .hex(0x0084d1),
        unread: .hex(0x00a6f4), addedBackground: Color(red: 16 / 255, green: 185 / 255, blue: 129 / 255, opacity: 0.12),
        removedBackground: Color(red: 244 / 255, green: 63 / 255, blue: 94 / 255, opacity: 0.12),
        backdrop: Color.black.opacity(0.3))

    static let dark = Palette(
        background: .hex(0x161616), foreground: .hex(0xeeeeee), card: .hex(0x222222), popover: .hex(0x1d1d1d),
        primary: .hex(0xeeeeee), primaryForeground: .hex(0x161616), secondary: .hex(0x292929),
        muted: .hex(0x222222), mutedForeground: .hex(0x989898), accent: .hex(0x2e2e2e),
        destructive: .hex(0x82181a), destructiveForeground: .hex(0xff645f), border: .hex(0x2e2e2e),
        input: .hex(0x333333), ring: .hex(0x636363), sidebar: .hex(0x0e0e0e), sidebarForeground: .hex(0xbebebe),
        sidebarAccent: .hex(0x242424), sidebarBorder: .hex(0x242424), codeBackground: .hex(0x262626),
        codeInline: .hex(0xe0a882), success: .hex(0x00d492), attention: .hex(0xffb900), working: .hex(0x00bcff),
        unread: .hex(0x00a6f4), addedBackground: Color(red: 16 / 255, green: 185 / 255, blue: 129 / 255, opacity: 0.12),
        removedBackground: Color(red: 244 / 255, green: 63 / 255, blue: 94 / 255, opacity: 0.12),
        backdrop: Color.black.opacity(0.5))

    static let solarizedLight = Palette(
        background: .hex(0xfdf6e3), foreground: .hex(0x586e75), card: .hex(0xeee8d5), popover: .hex(0xeee8d5),
        primary: .hex(0xac9d57), primaryForeground: .hex(0xffffff), secondary: .hex(0xeee8d5),
        muted: .hex(0xeee8d5), mutedForeground: .hex(0x657b83), accent: .hex(0xddd6c1),
        destructive: .hex(0xdc322f), destructiveForeground: .hex(0xdc322f), border: .hex(0xddd6c1),
        input: .hex(0xddd6c1), ring: .hex(0xd3af86), sidebar: .hex(0xeee8d5), sidebarForeground: .hex(0x616161),
        sidebarAccent: .hex(0xd1cbb8), sidebarBorder: .hex(0xddd6c1), codeBackground: .hex(0xeee8d5),
        codeInline: .hex(0x2aa198), success: .hex(0x859900), attention: .hex(0xb58900), working: .hex(0x268bd2),
        unread: .hex(0x268bd2), addedBackground: Color(red: 133 / 255, green: 153 / 255, blue: 0, opacity: 0.12),
        removedBackground: Color(red: 220 / 255, green: 50 / 255, blue: 47 / 255, opacity: 0.12),
        backdrop: Color.black.opacity(0.3))
}

extension Color {
    static func hex(_ value: UInt32) -> Color {
        Color(
            red: Double((value >> 16) & 0xff) / 255, green: Double((value >> 8) & 0xff) / 255,
            blue: Double(value & 0xff) / 255)
    }
}

/// Space, radius and type scale, in points (the web Client's at a 16 px root).
enum Space {
    static let xs: CGFloat = 4, sm: CGFloat = 8, md: CGFloat = 12, lg: CGFloat = 16, xl: CGFloat = 24
}

enum Radius {
    static let sm: CGFloat = 6, md: CGFloat = 8, lg: CGFloat = 10, xl: CGFloat = 14
}

enum FontSize {
    static let xs: CGFloat = 12, sm: CGFloat = 13, base: CGFloat = 15, lg: CGFloat = 17, xl: CGFloat = 20
}

enum FontWeight {
    case regular, medium, semibold

    var sansName: String {
        switch self {
        case .regular: "Geist-Regular"
        case .medium: "Geist-Medium"
        case .semibold: "Geist-SemiBold"
        }
    }

    var monoName: String {
        switch self {
        case .regular: "GeistMono-Regular"
        case .medium, .semibold: "GeistMono-Medium"
        }
    }
}

extension Font {
    static func sans(_ size: CGFloat, _ weight: FontWeight = .regular) -> Font {
        .custom(weight.sansName, fixedSize: size)
    }

    static func mono(_ size: CGFloat, _ weight: FontWeight = .regular) -> Font {
        .custom(weight.monoName, fixedSize: size)
    }

    /// Geist has no italic; emphasis slants the upright face, as browsers do.
    static func sansOblique(_ size: CGFloat, _ weight: FontWeight = .regular) -> Font {
        let slant = CGAffineTransform(a: 1, b: 0, c: 0.2, d: 1, tx: 0, ty: 0)
        let descriptor = UIFont.sans(size, weight).fontDescriptor.addingAttributes([.matrix: slant])
        return Font(UIFont(descriptor: descriptor, size: size) as CTFont)
    }
}

extension UIFont {
    static func sans(_ size: CGFloat, _ weight: FontWeight = .regular) -> UIFont {
        UIFont(name: weight.sansName, size: size) ?? .systemFont(ofSize: size)
    }

    static func mono(_ size: CGFloat, _ weight: FontWeight = .regular) -> UIFont {
        UIFont(name: weight.monoName, size: size) ?? .monospacedSystemFont(ofSize: size, weight: .regular)
    }
}

private struct PaletteKey: EnvironmentKey {
    static let defaultValue = Palette.light
}

private struct TextScaleKey: EnvironmentKey {
    static let defaultValue: CGFloat = 1
}

extension EnvironmentValues {
    var palette: Palette {
        get { self[PaletteKey.self] }
        set { self[PaletteKey.self] = newValue }
    }

    /// The reader's text size, for the transcript and the composer only.
    var textScale: CGFloat {
        get { self[TextScaleKey.self] }
        set { self[TextScaleKey.self] = newValue }
    }
}
