import AVFoundation
import DroiKit
import SwiftUI

/// Pairing: scan the QR code Droi shows, or paste its pairing link.
struct PairingScreen: View {
    var firstLaunch = false
    @Environment(AppModel.self) private var app
    @Environment(\.palette) private var palette

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: Space.lg) {
                ThemedText("Pair with a computer", size: FontSize.xl, weight: .semibold, scaled: false)
                    .accessibilityAddTraits(.isHeader)
                ThemedText(
                    "In Droi on your computer, open Settings → Remote Access and turn it on. Scan the QR code it shows, or paste its pairing link.",
                    tone: .muted, scaled: false
                )
                .fixedSize(horizontal: false, vertical: true)
                PairingForm()
                ThemedText(
                    "The phone reaches the computer over your network: the same Wi-Fi, Tailscale, or a proxy such as Surge Ponte. The pairing link carries the address to use.",
                    size: FontSize.sm, tone: .muted, scaled: false
                )
                .fixedSize(horizontal: false, vertical: true)
                if firstLaunch {
                    DroiButton(title: "Settings", variant: .ghost) { app.router.push(.settings) }
                }
            }
            .padding(Space.xl)
        }
        .scrollDismissesKeyboard(.interactively)
        .background(palette.background)
    }
}

/// The pairing link field and the way to the scanner.
struct PairingForm: View {
    @Environment(AppModel.self) private var app
    @State private var link = ""
    @State private var busy = false
    @State private var error: String?
    @Environment(\.palette) private var palette

    var body: some View {
        VStack(alignment: .leading, spacing: Space.md) {
            DroiButton(title: "Scan QR code") { app.router.push(.scan) }
            DroiTextField(
                label: "Pairing link", text: $link, placeholder: "http://192.168.1.10:41417/#pair=…", mono: true,
                keyboard: .URL, onSubmit: pair)
            if let error {
                ThemedText(error, size: FontSize.sm, tone: .destructive, scaled: false)
                    .fixedSize(horizontal: false, vertical: true)
                    .accessibilityAddTraits(.updatesFrequently)
            }
            DroiButton(
                title: "Pair", variant: .secondary, busy: busy,
                disabled: link.trimmingCharacters(in: .whitespaces).isEmpty, action: pair)
        }
    }

    private func pair() {
        guard !busy, !link.trimmingCharacters(in: .whitespaces).isEmpty else { return }
        busy = true
        error = nil
        Task {
            defer { busy = false }
            do {
                try await app.pair(link)
                link = ""
            } catch {
                self.error = error.localizedDescription
            }
        }
    }
}

/// The camera, reading the QR code Droi shows in Settings → Remote Access.
struct ScanScreen: View {
    @Environment(AppModel.self) private var app
    @State private var busy = false
    @State private var error: String?
    @State private var permission = AVCaptureDevice.authorizationStatus(for: .video)
    @Environment(\.palette) private var palette

    var body: some View {
        VStack(spacing: 0) {
            Group {
                switch permission {
                case .authorized:
                    QRScanner(paused: busy) { text in pair(text) }
                case .notDetermined:
                    Color.black.task {
                        _ = await AVCaptureDevice.requestAccess(for: .video)
                        permission = AVCaptureDevice.authorizationStatus(for: .video)
                    }
                default:
                    VStack(spacing: Space.md) {
                        ThemedText("Droi may not use the camera. Allow it in Settings to scan the code.", tone: .muted, scaled: false)
                            .multilineTextAlignment(.center)
                        DroiButton(title: "Open Settings", variant: .secondary) {
                            if let url = URL(string: UIApplication.openSettingsURLString) { UIApplication.shared.open(url) }
                        }
                    }
                    .padding(Space.xl)
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                }
            }
            VStack(spacing: Space.sm) {
                if let error {
                    ThemedText(error, size: FontSize.sm, tone: .destructive, scaled: false).multilineTextAlignment(.center)
                } else {
                    ThemedText(busy ? "Pairing…" : "Point the camera at the QR code in Droi → Settings.", size: FontSize.sm, tone: .muted, scaled: false)
                }
            }
            .padding(Space.lg)
            .frame(maxWidth: .infinity)
            .background(palette.background)
        }
    }

    private func pair(_ text: String) {
        guard !busy else { return }
        busy = true
        error = nil
        Task {
            do {
                try await app.pair(text)
            } catch {
                self.error = error.localizedDescription
            }
            busy = false
        }
    }
}

/// An AVFoundation preview that reports QR codes.
struct QRScanner: UIViewRepresentable {
    var paused: Bool
    var onScanned: (String) -> Void

    func makeCoordinator() -> Coordinator { Coordinator(onScanned: onScanned) }

    func makeUIView(context: Context) -> PreviewView {
        let view = PreviewView()
        let session = AVCaptureSession()
        if let device = AVCaptureDevice.default(.builtInWideAngleCamera, for: .video, position: .back),
            let input = try? AVCaptureDeviceInput(device: device), session.canAddInput(input)
        {
            session.addInput(input)
            let output = AVCaptureMetadataOutput()
            if session.canAddOutput(output) {
                session.addOutput(output)
                output.setMetadataObjectsDelegate(context.coordinator, queue: .main)
                output.metadataObjectTypes = [.qr]
            }
        }
        view.previewLayer.session = session
        view.previewLayer.videoGravity = .resizeAspectFill
        context.coordinator.session = session
        let box = CaptureBox(session)
        DispatchQueue.global(qos: .userInitiated).async { box.session.startRunning() }
        return view
    }

    func updateUIView(_ view: PreviewView, context: Context) {
        context.coordinator.paused = paused
        context.coordinator.onScanned = onScanned
    }

    static func dismantleUIView(_ view: PreviewView, coordinator: Coordinator) {
        guard let session = coordinator.session else { return }
        let box = CaptureBox(session)
        DispatchQueue.global(qos: .userInitiated).async { box.session.stopRunning() }
    }

    /// AVCaptureSession is thread-safe but not marked Sendable; starting and
    /// stopping it blocks, so it happens off the main thread.
    private final class CaptureBox: @unchecked Sendable {
        let session: AVCaptureSession
        init(_ session: AVCaptureSession) { self.session = session }
    }

    final class PreviewView: UIView {
        override class var layerClass: AnyClass { AVCaptureVideoPreviewLayer.self }
        var previewLayer: AVCaptureVideoPreviewLayer { layer as! AVCaptureVideoPreviewLayer }
    }

    final class Coordinator: NSObject, AVCaptureMetadataOutputObjectsDelegate, @unchecked Sendable {
        var onScanned: (String) -> Void
        var paused = false
        var session: AVCaptureSession?

        init(onScanned: @escaping (String) -> Void) { self.onScanned = onScanned }

        func metadataOutput(
            _ output: AVCaptureMetadataOutput, didOutput objects: [AVMetadataObject], from connection: AVCaptureConnection
        ) {
            guard !paused, let code = objects.compactMap({ $0 as? AVMetadataMachineReadableCodeObject }).first?.stringValue
            else { return }
            MainActor.assumeIsolated { onScanned(code) }
        }
    }
}

/// The computers this phone is paired with.
struct ComputersScreen: View {
    @Environment(AppModel.self) private var app
    @Environment(\.palette) private var palette

    var body: some View {
        ScrollView {
            VStack(spacing: Space.xl) {
                if !app.computers.computers.isEmpty {
                    ListSection {
                        ForEach(app.computers.computers) { computer in
                            ListRow(label: computer.name, value: displayAddress(computer.address)) {
                                app.router.push(.computer(computer.id))
                            }
                        }
                    }
                }
                ListSection {
                    ListRow(label: "Add a computer") { app.router.push(.pair) }
                }
            }
            .padding(Space.lg)
        }
        .background(palette.background)
    }
}

func displayAddress(_ address: String) -> String {
    address.replacingOccurrences(of: "https://", with: "").replacingOccurrences(of: "http://", with: "")
}

/// One Paired Computer: rename, re-address, or remove it.
struct ComputerScreen: View {
    let id: String
    @Environment(AppModel.self) private var app
    @Environment(\.palette) private var palette
    @State private var name = ""
    @State private var address = ""
    @State private var error: String?
    @State private var confirming = false

    var body: some View {
        if let computer = app.computers.computers.first(where: { $0.id == id }) {
            ScrollView {
                VStack(alignment: .leading, spacing: Space.md) {
                    ThemedText("Name", size: FontSize.sm, tone: .muted, scaled: false)
                    DroiTextField(label: "Name", text: $name, placeholder: computer.name)
                    ThemedText("Address", size: FontSize.sm, tone: .muted, scaled: false)
                    DroiTextField(label: "Address", text: $address, mono: true, keyboard: .URL)
                    ThemedText(
                        "Where the phone reaches Droi on this computer, such as 192.168.1.10:41417 or my-mac.tailnet.ts.net:41417.",
                        size: FontSize.xs, tone: .muted, scaled: false
                    )
                    .fixedSize(horizontal: false, vertical: true)
                    if let error { ThemedText(error, size: FontSize.sm, tone: .destructive, scaled: false) }
                    DroiButton(title: "Save") {
                        guard let normalized = normalizeAddress(address) else {
                            error = "Enter an address like 192.168.1.10:41417 or my-mac.tailnet.ts.net:41417."
                            return
                        }
                        let trimmed = name.trimmingCharacters(in: .whitespaces)
                        app.computers.update(id, name: trimmed.isEmpty ? computer.name : trimmed, address: normalized)
                        app.router.pop()
                    }
                    Spacer().frame(height: Space.lg)
                    if confirming {
                        ThemedText(
                            "Remove \(computer.name)? The phone forgets its Pairing Token; pair again to come back.",
                            size: FontSize.sm, scaled: false
                        )
                        .fixedSize(horizontal: false, vertical: true)
                        HStack(spacing: Space.md) {
                            DroiButton(title: "Cancel", variant: .secondary) { confirming = false }
                            DroiButton(title: "Remove", variant: .destructive) {
                                app.computers.remove(id, preferences: app.preferences)
                                app.router.pop()
                            }
                        }
                    } else {
                        DroiButton(title: "Remove computer", variant: .ghost) { confirming = true }
                    }
                }
                .padding(Space.xl)
            }
            .background(palette.background)
            .onAppear {
                name = computer.name
                address = displayAddress(computer.address)
            }
        }
    }
}

/// Theme, text size, alerts, and what this build is.
struct SettingsScreen: View {
    @Environment(AppModel.self) private var app
    @Environment(\.palette) private var palette

    var body: some View {
        @Bindable var preferences = app.preferences
        ScrollView {
            VStack(spacing: Space.xl) {
                ListSection {
                    ListRow(label: "Paired computers", value: String(app.computers.computers.count)) {
                        app.router.push(.computers)
                    }
                }
                ListChoices(
                    title: "Theme", choices: Preferences.Theme.allCases.map { ($0, $0.label) },
                    selection: $preferences.theme)
                ListChoices(
                    title: "Text size", choices: TextSize.allCases.map { ($0, $0.label) },
                    selection: $preferences.textSize)
                ListSection(title: "Sessions") {
                    ListSwitch(label: "Show archived sessions", isOn: $preferences.showArchived)
                }
                if let computer = app.computers.selected {
                    ListSection(
                        title: "New sessions",
                        footer: "Model, autonomy, spec mode, compaction and subagents for new Sessions on this computer."
                    ) {
                        ListRow(label: "Session defaults", value: computer.name) { app.router.push(.sessionDefaults) }
                    }
                }
                ListSection(
                    title: "Alerts",
                    footer: "While the app is open, for Sessions other than the one on screen. Sounds follow the silent switch."
                ) {
                    ListSwitch(label: "Sound when finished", isOn: $preferences.alerts.finishedSound)
                    ListSwitch(label: "Haptic when finished", isOn: $preferences.alerts.finishedHaptic)
                    ListSwitch(label: "Sound when it needs input", isOn: $preferences.alerts.needsInputSound)
                    ListSwitch(label: "Haptic when it needs input", isOn: $preferences.alerts.needsInputHaptic)
                }
                ListSection(title: "About") {
                    ListRow(label: "Version", value: AppInfo.version, chevron: false) {}
                    ListRow(label: "Signature", value: AppInfo.signatureLabel(), chevron: false) {}
                }
            }
            .padding(Space.lg)
        }
        .background(palette.background)
    }
}

enum AppInfo {
    static var version: String {
        Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "0.0.0"
    }

    /// When the build's provisioning profile expires: a free Apple account's
    /// lasts seven days.
    static var signatureExpiry: Date? {
        guard let url = Bundle.main.url(forResource: "embedded", withExtension: "mobileprovision"),
            let data = try? Data(contentsOf: url),
            let text = String(data: data, encoding: .isoLatin1),
            let start = text.range(of: "<?xml"), let end = text.range(of: "</plist>")
        else { return nil }
        let plist = Data(text[start.lowerBound..<end.upperBound].utf8)
        let object = try? PropertyListSerialization.propertyList(from: plist, format: nil) as? [String: Any]
        return object?["ExpirationDate"] as? Date
    }

    static func signatureLabel(now: Date = Date()) -> String {
        guard let expiry = signatureExpiry else { return "Not signed for a device" }
        let days = Int(ceil(expiry.timeIntervalSince(now) / 86_400))
        if days <= 0 { return "Expired" }
        return days == 1 ? "1 day left" : "\(days) days left"
    }
}
