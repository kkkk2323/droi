import DroiKit
import SwiftUI

/// Shows the content while the computer is connected or coming back, and in
/// its place what is wrong when it is not paired or not reachable. The Session
/// list keeps its (cached) content under the "can't reach" panel.
struct ConnectionGate<Content: View>: View {
    let connection: ComputerConnection
    var keepWhenUnreachable = false
    @ViewBuilder var content: () -> Content

    var body: some View {
        switch connection.state {
        case .unpaired:
            ScrollView { NoLongerPaired(computer: connection.computer).padding(Space.xl) }
        case .unreachable:
            if keepWhenUnreachable {
                VStack(spacing: 0) {
                    Unreachable(connection: connection).padding(Space.lg)
                    content()
                }
            } else {
                ScrollView { Unreachable(connection: connection).padding(Space.xl) }
            }
        default:
            content().overlay(alignment: .top) { Notices(connection: connection) }
        }
    }
}

private struct NoLongerPaired: View {
    let computer: PairedComputer

    var body: some View {
        VStack(alignment: .leading, spacing: Space.lg) {
            ThemedText("\(computer.name) is no longer paired", size: FontSize.lg, weight: .semibold, scaled: false)
                .accessibilityAddTraits(.isHeader)
            ThemedText(
                "Its Pairing Token was reset in Droi on the computer. Pair again from Droi → Settings → Remote Access; the phone keeps this computer and its name.",
                tone: .muted, scaled: false
            )
            .fixedSize(horizontal: false, vertical: true)
            PairingForm()
        }
    }
}

private struct Unreachable: View {
    let connection: ComputerConnection
    @Environment(\.palette) private var palette

    var body: some View {
        VStack(alignment: .leading, spacing: Space.md) {
            ThemedText("Can't reach \(connection.computer.name)", size: FontSize.lg, weight: .semibold, scaled: false)
                .accessibilityAddTraits(.isHeader)
            ThemedText("The phone is trying", tone: .muted, scaled: false)
            Text(verbatim: connection.computer.address)
                .font(.mono(FontSize.sm))
                .foregroundStyle(palette.foreground)
                .padding(.horizontal, Space.sm)
                .padding(.vertical, 4)
                .background(palette.card, in: Capsule())
            VStack(alignment: .leading, spacing: Space.xs) {
                bullet("Remote Access is on in Droi → Settings on the computer.")
                bullet("The iPhone is on the same network, or Tailscale is on for both.")
                bullet("The computer is awake and Droi is running.")
            }
            .accessibilityElement(children: .contain)
            .accessibilityLabel("What to check")
            HStack(spacing: Space.sm) {
                Spinner(size: 13)
                ThemedText("Trying again…", size: FontSize.sm, tone: .muted, scaled: false)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private func bullet(_ text: String) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: Space.sm) {
            Text("•").foregroundStyle(palette.mutedForeground)
            ThemedText(text, size: FontSize.sm, tone: .muted, scaled: false).fixedSize(horizontal: false, vertical: true)
        }
    }
}

/// The banners over the content: reconnecting, and a computer running
/// another release of Droi than this app.
private struct Notices: View {
    let connection: ComputerConnection
    @Environment(AppModel.self) private var app
    @Environment(\.palette) private var palette

    var body: some View {
        VStack(spacing: Space.sm) {
            if connection.state == .reconnecting {
                banner {
                    HStack(spacing: Space.sm) {
                        PulsingDot(color: palette.attention)
                        ThemedText("Reconnecting…", size: FontSize.sm, scaled: false)
                    }
                }
                .accessibilityElement(children: .ignore)
                .accessibilityLabel("Reconnecting")
            }
            if let key = versionNotice, let meta = connection.meta {
                banner {
                    HStack(alignment: .top, spacing: Space.sm) {
                        ThemedText(
                            "\(connection.computer.name) runs Droi \(meta.version); this app is \(AppInfo.version). Run pnpm install:phone to match.",
                            size: FontSize.sm, scaled: false
                        )
                        .fixedSize(horizontal: false, vertical: true)
                        Spacer(minLength: 0)
                        IconButton(systemName: "xmark", label: "Dismiss", size: 13) {
                            app.preferences.versionNoticeDismissed = key
                        }
                        .frame(width: 24, height: 24)
                    }
                }
            }
        }
        .padding(.horizontal, Space.md)
        .padding(.top, Space.xs)
        .animation(.snappy(duration: 0.2), value: connection.state)
    }

    /// The key a dismissal is stored under; nil when there is nothing to say.
    private var versionNotice: String? {
        guard let meta = connection.meta, versionMismatch(AppInfo.version, meta.version) else { return nil }
        let key = versionNoticeKey(computerId: connection.computer.id, appVersion: AppInfo.version, computerVersion: meta.version)
        return app.preferences.versionNoticeDismissed == key ? nil : key
    }

    private func banner<Inner: View>(@ViewBuilder _ inner: () -> Inner) -> some View {
        inner()
            .padding(.horizontal, Space.md)
            .padding(.vertical, Space.sm)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(palette.popover, in: RoundedRectangle(cornerRadius: Radius.lg))
            .hairlineBorder(palette.border, radius: Radius.lg)
            .transition(.opacity.combined(with: .move(edge: .top)))
    }
}
