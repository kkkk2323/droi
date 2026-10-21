import SwiftUI
import UIKit

/// The activity spinner: an arc turning once a second. Every spinner takes
/// its angle from the clock, so a column of them points the same way.
struct Spinner: View {
    var size: CGFloat = 12
    var color: Color?
    @Environment(\.palette) private var palette
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        TimelineView(.animation(paused: reduceMotion)) { context in
            let turn = context.date.timeIntervalSinceReferenceDate.truncatingRemainder(dividingBy: 1)
            Circle()
                .trim(from: 0, to: 0.7)
                .stroke(color ?? palette.mutedForeground, style: StrokeStyle(lineWidth: max(1.5, size / 8), lineCap: .round))
                .rotationEffect(.degrees(turn * 360))
        }
        .frame(width: size, height: size)
        .accessibilityHidden(true)
    }
}

/// Three dots bouncing in turn: the Session is working.
struct BouncingDots: View {
    var color: Color
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        TimelineView(.animation(paused: reduceMotion)) { context in
            let t = context.date.timeIntervalSinceReferenceDate
            HStack(spacing: 3) {
                ForEach(0..<3) { i in
                    let phase = (t * 2.2 - Double(i) * 0.18).truncatingRemainder(dividingBy: 1)
                    Circle()
                        .fill(color)
                        .frame(width: 5, height: 5)
                        .offset(y: reduceMotion ? 0 : -3 * max(0, sin(phase * .pi * 2)))
                }
            }
        }
        .frame(height: 12)
        .accessibilityHidden(true)
    }
}

/// A dot that fades in and out.
struct PulsingDot: View {
    var color: Color
    @State private var on = false

    var body: some View {
        Circle().fill(color).frame(width: 7, height: 7)
            .opacity(on ? 1 : 0.35)
            .animation(.easeInOut(duration: 0.8).repeatForever(), value: on)
            .onAppear { on = true }
    }
}

/// Text in the UI face and scale: the chrome's sizes, scaled with the
/// reader's text size where the environment says so.
struct ThemedText: View {
    let text: String
    var size: CGFloat = FontSize.base
    var weight: FontWeight = .regular
    var tone: Tone = .default
    var mono = false
    var scaled = true
    @Environment(\.palette) private var palette
    @Environment(\.textScale) private var scale

    enum Tone { case `default`, muted, destructive, success, attention, working }

    init(
        _ text: String, size: CGFloat = FontSize.base, weight: FontWeight = .regular, tone: Tone = .default,
        mono: Bool = false, scaled: Bool = true
    ) {
        self.text = text
        self.size = size
        self.weight = weight
        self.tone = tone
        self.mono = mono
        self.scaled = scaled
    }

    var body: some View {
        let s = scaled ? size * scale : size
        Text(verbatim: text)
            .font(mono ? .mono(s, weight) : .sans(s, weight))
            .foregroundStyle(color)
    }

    private var color: Color {
        switch tone {
        case .default: palette.foreground
        case .muted: palette.mutedForeground
        case .destructive: palette.destructiveForeground
        case .success: palette.success
        case .attention: palette.attention
        case .working: palette.working
        }
    }
}

/// A round icon button for toolbars and rows.
struct IconButton: View {
    let systemName: String
    let label: String
    var size: CGFloat = 17
    var action: () -> Void
    @Environment(\.palette) private var palette

    var body: some View {
        Button(action: action) {
            Image(systemName: systemName)
                .font(.system(size: size, weight: .regular))
                .foregroundStyle(palette.foreground)
                .frame(width: 36, height: 36)
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel(label)
    }
}

/// Decoded transcript images, kept while memory allows.
@MainActor
enum ImageCache {
    private static let cache: NSCache<NSString, UIImage> = {
        let cache = NSCache<NSString, UIImage>()
        cache.totalCostLimit = 64 << 20
        return cache
    }()

    static func image(key: String, data: () -> Data?) -> UIImage? {
        if let hit = cache.object(forKey: key as NSString) { return hit }
        guard let data = data(), let image = UIImage(data: data) else { return nil }
        cache.setObject(image, forKey: key as NSString, cost: data.count)
        return image
    }
}

extension View {
    /// A hairline in the theme's border colour.
    func hairlineBorder(_ color: Color, radius: CGFloat) -> some View {
        modifier(HairlineBorder(color: color, radius: radius))
    }
}

private struct HairlineBorder: ViewModifier {
    let color: Color
    let radius: CGFloat
    @Environment(\.displayScale) private var displayScale

    func body(content: Content) -> some View {
        content.overlay(RoundedRectangle(cornerRadius: radius).strokeBorder(color, lineWidth: 1 / displayScale))
    }
}
