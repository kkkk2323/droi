// Client-side preferences that never reach the Daemon or the native app,
// stored per device. Keys and formats are the Phone App's, so a store the app
// hands in (UserDefaults) reads the way the old one wrote.
import DroidProtocol
import Foundation

/// Synchronous key-value storage the app supplies.
public protocol PreferenceStorage: AnyObject, Sendable {
    func string(forKey key: String) -> String?
    func set(_ value: String, forKey key: String)
    func removeValue(forKey key: String)
}

/// Storage in memory, for tests and previews.
public final class MemoryPreferenceStorage: PreferenceStorage, @unchecked Sendable {
    private let lock = NSLock()
    private var values: [String: String]

    public init(_ values: [String: String] = [:]) {
        self.values = values
    }

    public func string(forKey key: String) -> String? {
        lock.withLock { values[key] }
    }

    public func set(_ value: String, forKey key: String) {
        lock.withLock { values[key] = value }
    }

    public func removeValue(forKey key: String) {
        lock.withLock { _ = values.removeValue(forKey: key) }
    }
}

/// UserDefaults as preference storage.
public final class UserDefaultsPreferenceStorage: PreferenceStorage, @unchecked Sendable {
    private let defaults: UserDefaults

    public init(_ defaults: UserDefaults = .standard) {
        self.defaults = defaults
    }

    public func string(forKey key: String) -> String? { defaults.string(forKey: key) }
    public func set(_ value: String, forKey key: String) { defaults.set(value, forKey: key) }
    public func removeValue(forKey key: String) { defaults.removeObject(forKey: key) }
}

/// One preference: its key, what it reads as when unset or unreadable, and how it is written.
public struct Preference<Value: Sendable>: Sendable {
    public let key: String
    public let fallback: Value
    private let parse: @Sendable (String) -> Value?
    private let serialize: @Sendable (Value) -> String

    public init(
        _ key: String, fallback: Value, parse: @escaping @Sendable (String) -> Value?,
        serialize: @escaping @Sendable (Value) -> String
    ) {
        self.key = key
        self.fallback = fallback
        self.parse = parse
        self.serialize = serialize
    }

    public func get(_ storage: some PreferenceStorage) -> Value {
        storage.string(forKey: key).flatMap(parse) ?? fallback
    }

    public func set(_ value: Value, in storage: some PreferenceStorage) {
        storage.set(serialize(value), forKey: key)
    }
}

extension Preference where Value == Bool {
    public static func bool(_ key: String, fallback: Bool) -> Preference {
        Preference(key, fallback: fallback, parse: { $0 == "true" }, serialize: { $0 ? "true" : "false" })
    }
}

extension Preference where Value == String? {
    /// A string; empty reads as unset.
    public static func string(_ key: String) -> Preference {
        Preference(key, fallback: nil, parse: { .some($0.isEmpty ? nil : $0) }, serialize: { $0 ?? "" })
    }
}

extension Preference where Value == [String] {
    public static func stringList(_ key: String) -> Preference {
        Preference(
            key, fallback: [],
            parse: { raw in JSONText.parse(raw)?.arrayValue.map { $0.compactMap(\.stringValue) } },
            serialize: { JSONText.stringify(.array($0.map(JSONValue.string))) })
    }
}

extension Preference where Value: Codable {
    /// A JSON value; one that does not decode reads as the fallback.
    public static func json(_ key: String, fallback: Value) -> Preference {
        Preference(
            key, fallback: fallback,
            parse: { try? JSONDecoder().decode(Value.self, from: Data($0.utf8)) },
            serialize: { (try? String(decoding: JSONEncoder().encode($0), as: UTF8.self)) ?? "" })
    }
}

extension Preference where Value: RawRepresentable, Value.RawValue == String {
    /// One of a fixed set; anything else reads as the fallback.
    public static func choice(_ key: String, fallback: Value) -> Preference {
        Preference(key, fallback: fallback, parse: Value.init(rawValue:), serialize: \.rawValue)
    }
}

/// Toggles one entry of a list preference.
public func toggleListed(_ preference: Preference<[String]>, _ entry: String, in storage: some PreferenceStorage) {
    let current = preference.get(storage)
    preference.set(current.contains(entry) ? current.filter { $0 != entry } : current + [entry], in: storage)
}

/// The theme a device draws with; `system` follows the phone's light or dark.
public enum ThemeChoice: String, Sendable, Hashable, CaseIterable, Codable {
    case system
    case light
    case dark
    case solarizedLight = "solarized-light"

    public var label: String {
        switch self {
        case .system: "System"
        case .light: "Light"
        case .dark: "Dark"
        case .solarizedLight: "Solarized Light+"
        }
    }

    /// The theme drawn, given whether the system is dark now.
    public func theme(systemIsDark: Bool) -> ThemeName {
        switch self {
        case .system: systemIsDark ? .dark : .light
        case .light: .light
        case .dark: .dark
        case .solarizedLight: .solarizedLight
        }
    }
}

public enum Preferences {
    /// Whether the New session page starts in a fresh Git worktree.
    public static let worktreeByDefault = Preference<Bool>.bool("droi.worktreeByDefault", fallback: false)
    /// The lifecycle the last worktree was made with.
    public static let worktreeLifecycle = Preference<WorktreeLifecycle>(
        "droi.worktreeLifecycle", fallback: defaultWorktreeLifecycle,
        parse: { isWorktreeLifecycle($0) ? WorktreeLifecycle(rawValue: $0) : nil }, serialize: \.rawValue)
    public static let showArchivedSessions = Preference<Bool>.bool("droi.showArchived", fallback: false)
    /// Model ids starred in the picker, in the order they were starred.
    public static let favoriteModels = Preference<[String]>.stringList("droi.favoriteModels")
    /// Workspace groups the user folded away in the Session list.
    public static let foldedWorkspaces = Preference<[String]>.stringList("droi.foldedWorkspaces")
    /// Workspace groups and Sessions kept at the top of the Session list.
    public static let pinnedWorkspaces = Preference<[String]>.stringList("droi.pinnedWorkspaces")
    public static let pinnedSessions = Preference<[String]>.stringList("droi.pinnedSessions")
    public static let textSize = Preference<TextSize>.choice("droi.textSize", fallback: .default)
    /// What new Sessions on this device start with; nil leaves it to droid.
    public static let defaultToolMode = Preference<ToolMode?>(
        "droi.toolExecutionMode", fallback: nil, parse: { .some(ToolMode(rawValue: $0)) },
        serialize: { $0?.rawValue ?? "" })
    public static let theme = Preference<ThemeChoice>.choice("droi.phoneTheme", fallback: .system)
    public static let alertSwitches = Preference<AlertSwitches>.json("droi.phoneAlerts", fallback: AlertSwitches())
    public static let pairedComputers = Preference<[PairedComputer]>(
        "droi.pairedComputers", fallback: [],
        parse: { raw in
            JSONText.parse(raw)?.arrayValue.map { $0.compactMap { try? $0.decode(PairedComputer.self) } }
        },
        serialize: { (try? String(decoding: JSONEncoder().encode($0), as: UTF8.self)) ?? "[]" })
    public static let selectedComputer = Preference<String?>.string("droi.selectedComputer")
    /// The dismissed version notice (see versionNoticeKey).
    public static let versionNoticeDismissed = Preference<String?>.string("droi.versionNoticeDismissed")

    /// What was last seen of a computer's Session list, so switching to it or a
    /// cold start is not a blank list. Transcripts are never kept.
    public static func sessionSummaries(_ computerId: String) -> Preference<[SessionSummaryCache]> {
        .json("droi.sessions.\(computerId)", fallback: [])
    }

    /// The Session open when the app last showed this computer.
    public static func lastSession(_ computerId: String) -> Preference<String?> {
        .string("droi.lastSession.\(computerId)")
    }

    /// The composer's text for a Session (or the New session page); images stay in memory.
    public static func draft(_ key: String) -> Preference<String?> {
        .string("droi.draft.\(key)")
    }
}
