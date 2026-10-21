import Foundation

/// The Phone App carries the desktop version it was built from; a computer
/// running another version still works, but may lack or change something.
public func versionMismatch(_ appVersion: String, _ computerVersion: String) -> Bool {
    func clean(_ version: String) -> String {
        let trimmed = version.trimmed
        return trimmed.hasPrefix("v") ? String(trimmed.dropFirst()) : trimmed
    }
    let app = clean(appVersion)
    let computer = clean(computerVersion)
    return !app.isEmpty && !computer.isEmpty && app != computer
}

/// What the version notice stores once dismissed, so a new version or another computer shows it again.
public func versionNoticeKey(computerId: String, appVersion: String, computerVersion: String) -> String {
    "\(computerId) \(appVersion) \(computerVersion)"
}

/// A free Apple account signs the Phone App for seven days. The expiry is in
/// the provisioning profile embedded in the app: a signed container holding a
/// plain XML property list, so the date can be read without verifying it.
public func profileExpiry(_ profile: Data) -> Date? {
    let text = String(decoding: profile.map { $0 < 0x80 ? $0 : 0x3F }, as: UTF8.self)
    guard let key = text.range(of: "<key>ExpirationDate</key>") else { return nil }
    let rest = text[key.upperBound...].drop(while: { $0.isWhitespace })
    guard rest.hasPrefix("<date>"), let end = rest.range(of: "</date>") else { return nil }
    let value = rest[rest.index(rest.startIndex, offsetBy: 6)..<end.lowerBound]
    return try? Date(String(value), strategy: .iso8601)
}

/// Whole days left, counting a part-day as a day; 0 once it has lapsed.
public func daysLeft(_ expiry: Date, now: Date) -> Int {
    let seconds = expiry.timeIntervalSince(now)
    return seconds <= 0 ? 0 : Int((seconds / 86_400).rounded(.up))
}

/// How full the Session's context window is. "Used" is what the last model
/// call actually sent (input + cache-read tokens), which the Daemon reports
/// after every call; the budget comes from its context breakdown once per
/// model. The breakdown's own `usedTokens` is a chars/4 guess over the stored
/// history and counts base64 images at full length, so it only stands in
/// before the first call.
public struct ContextUsage: Sendable, Hashable {
    public var usedTokens: Double
    public var budgetTokens: Double
    /// 0...1
    public var ratio: Double

    public init(lastCallTokens: Double?, estimatedTokens: Double, budgetTokens: Double) {
        usedTokens = lastCallTokens ?? estimatedTokens
        self.budgetTokens = budgetTokens
        ratio = budgetTokens > 0 ? min(1, usedTokens / budgetTokens) : 0
    }
}

public func formatTokens(_ count: Double) -> String {
    // One decimal with halves rounded up, as JavaScript's toFixed(1) writes it.
    func oneDecimal(_ value: Double) -> String {
        let tenths = Int((value * 10).rounded())
        return "\(tenths / 10).\(tenths % 10)"
    }
    if count >= 1_000_000 { return "\(oneDecimal(count / 1_000_000))M" }
    if count >= 10_000 { return "\(Int((count / 1_000).rounded()))k" }
    if count >= 1_000 { return "\(oneDecimal(count / 1_000))k" }
    return JSONText.number(count)
}

/// The lines "Copy session details" puts on the clipboard; unknown ones are left out.
public func sessionDetails(sessionId: String, title: String, cwd: String?, file: String?) -> String {
    [
        "Title: \(title)",
        "Session ID: \(sessionId)",
        cwd.map { "Workspace: \($0)" },
        file.map { "Transcript: \($0)" },
    ].compactMap { $0 }.joined(separator: "\n")
}

/// Keys for a list drawn in order, from each item's own name and numbered
/// when a name repeats, so an item keeps its key as items are added after it.
public func withKeys<T>(_ items: [T], name: (T) -> String) -> [(key: String, item: T)] {
    var seen: [String: Int] = [:]
    return items.map { item in
        let base = name(item)
        let count = seen[base, default: 0]
        seen[base] = count + 1
        return (count == 0 ? base : "\(base)#\(count)", item)
    }
}
