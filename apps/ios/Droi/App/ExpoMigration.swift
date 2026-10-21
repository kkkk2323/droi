import CryptoKit
import DroiKit
import Foundation
import Security

/// The Expo app this one replaces kept its preferences in AsyncStorage and its
/// Pairing Tokens in expo-secure-store's keychain items. Both stay readable
/// under the same bundle id, so the first launch carries them over and the
/// phone stays paired.
enum ExpoMigration {
    private static let doneKey = "droi.migratedFromExpo"

    @MainActor
    static func runOnce(defaults: UserDefaults = .standard) {
        guard !defaults.bool(forKey: doneKey) else { return }
        defer { defaults.set(true, forKey: doneKey) }
        let values = asyncStorage()
        guard !values.isEmpty else { return }
        for (key, raw) in values where key.hasPrefix("droi.") {
            // Session list caches are rebuilt from the Daemon on the next connection.
            if key.hasPrefix("droi.sessions.") || defaults.object(forKey: key) != nil { continue }
            let parsed = try? JSONSerialization.jsonObject(with: Data(raw.utf8), options: [.fragmentsAllowed])
            // Preferences were stored as JSON; drafts and plain strings as is.
            if let string = parsed as? String {
                defaults.set(string, forKey: key)
            } else if let number = parsed as? NSNumber, CFGetTypeID(number) == CFBooleanGetTypeID() {
                defaults.set(number.boolValue, forKey: key)
            } else if let array = parsed as? [String] {
                defaults.set(array, forKey: key)
            } else {
                defaults.set(raw, forKey: key)
            }
        }
        if let text = values["droi.pairedComputers"],
            let computers = try? JSONDecoder().decode([PairedComputer].self, from: Data(text.utf8))
        {
            for computer in computers where Keychain.token(for: computer.id) == nil {
                if let token = expoSecureStoreValue("pairingToken.\(computer.id)") {
                    Keychain.setToken(token, for: computer.id)
                }
            }
        }
    }

    private static func asyncStorage() -> [String: String] {
        guard let support = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask).first,
            let bundle = Bundle.main.bundleIdentifier
        else { return [:] }
        let directory = support.appendingPathComponent(bundle).appendingPathComponent("RCTAsyncLocalStorage_V1")
        guard let data = try? Data(contentsOf: directory.appendingPathComponent("manifest.json")),
            let manifest = try? JSONSerialization.jsonObject(with: data) as? [String: Any]
        else { return [:] }
        var out: [String: String] = [:]
        for (key, value) in manifest {
            if let value = value as? String {
                out[key] = value
            } else {
                // Values over 1 KB sit in a file named after the key's MD5.
                let name = Insecure.MD5.hash(data: Data(key.utf8)).map { String(format: "%02x", $0) }.joined()
                if let text = try? String(contentsOf: directory.appendingPathComponent(name), encoding: .utf8) {
                    out[key] = text
                }
            }
        }
        return out
    }

    private static func expoSecureStoreValue(_ key: String) -> String? {
        for service in ["app:no-auth", "app"] {
            let query: [String: Any] = [
                kSecClass as String: kSecClassGenericPassword,
                kSecAttrService as String: service,
                kSecAttrGeneric as String: Data(key.utf8),
                kSecAttrAccount as String: Data(key.utf8),
                kSecReturnData as String: true,
                kSecMatchLimit as String: kSecMatchLimitOne,
            ]
            var item: CFTypeRef?
            if SecItemCopyMatching(query as CFDictionary, &item) == errSecSuccess, let data = item as? Data {
                return String(decoding: data, as: UTF8.self)
            }
        }
        return nil
    }
}
