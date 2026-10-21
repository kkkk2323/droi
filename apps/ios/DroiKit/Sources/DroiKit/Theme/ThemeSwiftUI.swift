#if canImport(SwiftUI)
import SwiftUI

extension Color {
    public init(_ color: ThemeColor) {
        self.init(.sRGB, red: color.red, green: color.green, blue: color.blue, opacity: color.alpha)
    }
}

extension ThemeName {
    public var colorScheme: SwiftUI.ColorScheme { scheme == .dark ? .dark : .light }
}
#endif
