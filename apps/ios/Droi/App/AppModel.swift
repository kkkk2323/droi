import DroiKit
import Observation
import SwiftUI

/// Where the navigation stack can go.
enum Route: Hashable {
    case session(String)
    case newSession(workspace: String?, scratch: Bool)
    case settings
    case sessionDefaults
    case pair
    case scan
    case computers
    case computer(String)
}

@MainActor
@Observable
final class Router {
    var path: [Route] = []

    func push(_ route: Route) { path.append(route) }

    func pop() { if !path.isEmpty { path.removeLast() } }

    /// Back to the Session list.
    func popToRoot() { path.removeAll() }

    /// Opening a Session already on the stack goes back to it.
    func open(session id: String) {
        if let index = path.lastIndex(of: .session(id)) {
            path.removeSubrange((index + 1)...)
        } else {
            path.append(.session(id))
        }
    }

    /// Swaps the screen on top for another (New session → its Session, a
    /// compaction's handoff → the new Session).
    func replaceTop(with route: Route) {
        if !path.isEmpty { path.removeLast() }
        path.append(route)
    }
}

/// The app: preferences, Paired Computers, navigation, and the connection to
/// the selected computer.
@MainActor
@Observable
final class AppModel {
    let preferences: Preferences
    let computers: ComputersStore
    let router = Router()
    let alerts = AlertPlayer()
    private(set) var connection: ComputerConnection?
    /// The last open Session opens again once per launch.
    var reopenedLastSession = false

    init(preferences: Preferences = Preferences(), computers: ComputersStore = ComputersStore()) {
        self.preferences = preferences
        self.computers = computers
        connectToSelected()
    }

    /// Opens the connection to the selected computer, replacing the last one.
    func connectToSelected() {
        connection?.dispose()
        connection = nil
        guard let computer = computers.selected else { return }
        let connection = ComputerConnection(
            computer: computer, token: Keychain.token(for: computer.id), preferences: preferences, alerts: alerts)
        self.connection = connection
        connection.start()
    }

    func select(_ computerId: String) {
        guard computers.selectedId != computerId else { return }
        computers.selectedId = computerId
        router.popToRoot()
        connectToSelected()
    }

    /// Pairs with the computer a pairing link or QR code names, and goes
    /// back to the Session list connected to it.
    func pair(_ text: String) async throws {
        let result = try await resolvePairing(text)
        computers.add(result)
        router.popToRoot()
        connectToSelected()
    }

    var palette: Palette {
        switch preferences.theme {
        case .light: .light
        case .dark: .dark
        case .solarizedLight: .solarizedLight
        case .system: systemScheme == .dark ? .dark : .light
        }
    }

    var colorScheme: ColorScheme? {
        switch preferences.theme {
        case .system: nil
        case .dark: .dark
        case .light, .solarizedLight: .light
        }
    }

    var systemScheme: ColorScheme = .light
}

@main
struct DroiApp: App {
    @State private var app: AppModel = {
        ExpoMigration.runOnce()
        let app = AppModel()
        #if DEBUG
        // `-DroiPairingLink <link>` pairs at launch, for the simulator, which has no camera to scan with.
        if let link = UserDefaults.standard.string(forKey: "DroiPairingLink") {
            Task { try? await app.pair(link) }
        }
        #endif
        return app
    }()
    @Environment(\.scenePhase) private var scenePhase

    var body: some Scene {
        WindowGroup {
            RootView()
                .environment(app)
                .onChange(of: scenePhase) { _, phase in
                    switch phase {
                    case .background: app.connection?.suspend()
                    case .active: app.connection?.resume()
                    default: break
                    }
                }
        }
    }
}

struct RootView: View {
    @Environment(AppModel.self) private var app
    @Environment(\.colorScheme) private var systemScheme

    var body: some View {
        @Bindable var router = app.router
        let palette = app.palette
        NavigationStack(path: $router.path) {
            Group {
                if let connection = app.connection {
                    SessionsScreen(connection: connection)
                } else {
                    PairingScreen(firstLaunch: true).toolbar(.hidden, for: .navigationBar)
                }
            }
            .navigationDestination(for: Route.self) { route in
                destination(route)
                    .background(palette.background)
            }
        }
        .environment(\.palette, palette)
        .tint(palette.foreground)
        .preferredColorScheme(app.colorScheme)
        .onChange(of: systemScheme, initial: true) { _, scheme in app.systemScheme = scheme }
        // droi-phone:///session/<id> opens a Session of the selected computer.
        .onOpenURL { url in
            let parts = url.pathComponents.filter { $0 != "/" }
            if parts.count == 2, parts[0] == "session", app.connection != nil { app.router.open(session: parts[1]) }
        }
        .font(.sans(FontSize.base))
    }

    @ViewBuilder
    private func destination(_ route: Route) -> some View {
        switch route {
        case .session(let id):
            if let connection = app.connection { SessionScreen(connection: connection, sessionId: id) }
        case .newSession(let workspace, let scratch):
            if let connection = app.connection { NewSessionScreen(connection: connection, workspace: workspace, scratch: scratch) }
        case .settings:
            SettingsScreen().navigationTitle("Settings")
        case .sessionDefaults:
            if let connection = app.connection { SessionDefaultsScreen(connection: connection).navigationTitle("Session defaults") }
        case .pair:
            PairingScreen().navigationTitle("Add a computer")
        case .scan:
            ScanScreen().navigationTitle("Scan QR code")
        case .computers:
            ComputersScreen().navigationTitle("Paired computers")
        case .computer(let id):
            ComputerScreen(id: id).navigationTitle("Computer")
        }
    }
}
