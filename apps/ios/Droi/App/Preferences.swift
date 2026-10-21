import DroiKit
import Foundation
import Observation
import Security

/// The phone's own preferences, kept in UserDefaults under the `droi.` keys
/// the Expo app used, so nothing is lost on the way.
@MainActor
@Observable
final class Preferences {
    enum Theme: String, CaseIterable {
        case system, light, dark, solarizedLight = "solarized-light"

        var label: String {
            switch self {
            case .system: "System"
            case .light: "Light"
            case .dark: "Dark"
            case .solarizedLight: "Solarized Light+"
            }
        }
    }

    @ObservationIgnored private let defaults: UserDefaults

    var theme: Theme { didSet { defaults.set(theme.rawValue, forKey: "droi.phoneTheme") } }
    var textSize: TextSize { didSet { defaults.set(textSize.rawValue, forKey: "droi.textSize") } }
    var showArchived: Bool { didSet { defaults.set(showArchived, forKey: "droi.showArchived") } }
    var alerts: AlertSwitches { didSet { save(alerts, "droi.phoneAlerts") } }
    var favoriteModels: [String] { didSet { defaults.set(favoriteModels, forKey: "droi.favoriteModels") } }
    var foldedWorkspaces: [String] { didSet { defaults.set(foldedWorkspaces, forKey: "droi.foldedWorkspaces") } }
    var pinnedWorkspaces: [String] { didSet { defaults.set(pinnedWorkspaces, forKey: "droi.pinnedWorkspaces") } }
    var pinnedSessions: [String] { didSet { defaults.set(pinnedSessions, forKey: "droi.pinnedSessions") } }
    var worktreeByDefault: Bool { didSet { defaults.set(worktreeByDefault, forKey: "droi.worktreeByDefault") } }
    var worktreeLifecycle: String { didSet { defaults.set(worktreeLifecycle, forKey: "droi.worktreeLifecycle") } }
    var toolMode: ToolMode? { didSet { defaults.set(toolMode?.rawValue, forKey: "droi.toolExecutionMode") } }

    init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
        theme = defaults.string(forKey: "droi.phoneTheme").flatMap(Theme.init) ?? .system
        textSize = defaults.string(forKey: "droi.textSize").flatMap(TextSize.init) ?? .default
        showArchived = defaults.bool(forKey: "droi.showArchived")
        alerts = Self.load(defaults, "droi.phoneAlerts") ?? AlertSwitches()
        favoriteModels = defaults.stringArray(forKey: "droi.favoriteModels") ?? []
        foldedWorkspaces = defaults.stringArray(forKey: "droi.foldedWorkspaces") ?? []
        pinnedWorkspaces = defaults.stringArray(forKey: "droi.pinnedWorkspaces") ?? []
        pinnedSessions = defaults.stringArray(forKey: "droi.pinnedSessions") ?? []
        worktreeByDefault = defaults.bool(forKey: "droi.worktreeByDefault")
        worktreeLifecycle = defaults.string(forKey: "droi.worktreeLifecycle") ?? "ephemeral"
        toolMode = defaults.string(forKey: "droi.toolExecutionMode").flatMap(ToolMode.init)
    }

    func toggle(_ value: String, in keyPath: ReferenceWritableKeyPath<Preferences, [String]>) {
        if let index = self[keyPath: keyPath].firstIndex(of: value) {
            self[keyPath: keyPath].remove(at: index)
        } else {
            self[keyPath: keyPath].append(value)
        }
    }

    // MARK: Per-computer and per-Session values

    func draft(_ key: String) -> String { defaults.string(forKey: "droi.draft.\(key)") ?? "" }

    func setDraft(_ text: String, for key: String) {
        if text.isEmpty {
            defaults.removeObject(forKey: "droi.draft.\(key)")
        } else {
            defaults.set(text, forKey: "droi.draft.\(key)")
        }
    }

    func lastSession(of computerId: String) -> String? { defaults.string(forKey: "droi.lastSession.\(computerId)") }

    func setLastSession(_ sessionId: String?, of computerId: String) {
        defaults.set(sessionId, forKey: "droi.lastSession.\(computerId)")
    }

    func cachedSessions(of computerId: String) -> [SessionSummary] {
        Self.load(defaults, "droi.sessions.\(computerId)") ?? []
    }

    func setCachedSessions(_ sessions: [SessionSummary], of computerId: String) {
        save(sessions, "droi.sessions.\(computerId)")
    }

    var versionNoticeDismissed: String? {
        get { defaults.string(forKey: "droi.versionNoticeDismissed") }
        set { defaults.set(newValue, forKey: "droi.versionNoticeDismissed") }
    }

    private func save<T: Encodable>(_ value: T, _ key: String) {
        if let data = try? JSONEncoder().encode(value) {
            defaults.set(String(decoding: data, as: UTF8.self), forKey: key)
        }
    }

    private static func load<T: Decodable>(_ defaults: UserDefaults, _ key: String) -> T? {
        guard let text = defaults.string(forKey: key) else { return nil }
        return try? JSONDecoder().decode(T.self, from: Data(text.utf8))
    }
}

/// Pairing Tokens live in the keychain, only on this device and only while it
/// is unlocked; everything else about a Paired Computer is in UserDefaults.
enum Keychain {
    private static let service = "com.kkkk2323.droi"

    static func token(for computerId: String) -> String? {
        var query = base("pairingToken.\(computerId)")
        query[kSecReturnData as String] = true
        query[kSecMatchLimit as String] = kSecMatchLimitOne
        var item: CFTypeRef?
        guard SecItemCopyMatching(query as CFDictionary, &item) == errSecSuccess, let data = item as? Data else {
            return nil
        }
        return String(decoding: data, as: UTF8.self)
    }

    static func setToken(_ token: String, for computerId: String) {
        let query = base("pairingToken.\(computerId)")
        SecItemDelete(query as CFDictionary)
        var add = query
        add[kSecValueData as String] = Data(token.utf8)
        add[kSecAttrAccessible as String] = kSecAttrAccessibleWhenUnlockedThisDeviceOnly
        SecItemAdd(add as CFDictionary, nil)
    }

    static func removeToken(for computerId: String) {
        SecItemDelete(base("pairingToken.\(computerId)") as CFDictionary)
    }

    #if DEBUG
    static func removeAll() {
        SecItemDelete([kSecClass as String: kSecClassGenericPassword, kSecAttrService as String: service] as CFDictionary)
    }
    #endif

    private static func base(_ account: String) -> [String: Any] {
        [kSecClass as String: kSecClassGenericPassword, kSecAttrService as String: service, kSecAttrAccount as String: account]
    }
}

@MainActor
@Observable
final class ComputersStore {
    @ObservationIgnored private let defaults: UserDefaults
    private(set) var computers: [PairedComputer]
    var selectedId: String? { didSet { defaults.set(selectedId, forKey: "droi.selectedComputer") } }

    init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
        computers = defaults.string(forKey: "droi.pairedComputers")
            .flatMap { try? JSONDecoder().decode([PairedComputer].self, from: Data($0.utf8)) } ?? []
        selectedId = defaults.string(forKey: "droi.selectedComputer")
        if selected == nil { selectedId = computers.first?.id }
    }

    var selected: PairedComputer? { computers.first { $0.id == selectedId } }

    /// Adds a computer, or for one already known keeps its name and takes the
    /// new address; selects it and keeps its Pairing Token.
    func add(_ pairing: PairingResult) {
        if let index = computers.firstIndex(where: { $0.id == pairing.id }) {
            computers[index].address = pairing.address
        } else {
            computers.append(PairedComputer(id: pairing.id, name: pairing.name, address: pairing.address))
        }
        Keychain.setToken(pairing.token, for: pairing.id)
        selectedId = pairing.id
        save()
    }

    func update(_ id: String, name: String, address: String) {
        guard let index = computers.firstIndex(where: { $0.id == id }) else { return }
        computers[index].name = name
        computers[index].address = address
        save()
    }

    func remove(_ id: String, preferences: Preferences) {
        Keychain.removeToken(for: id)
        computers.removeAll { $0.id == id }
        preferences.setCachedSessions([], of: id)
        preferences.setLastSession(nil, of: id)
        if selectedId == id { selectedId = computers.first?.id }
        save()
    }

    private func save() {
        if let data = try? JSONEncoder().encode(computers) {
            defaults.set(String(decoding: data, as: UTF8.self), forKey: "droi.pairedComputers")
        }
    }
}
