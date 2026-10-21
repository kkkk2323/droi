// What "/" can start in the composer: the commands the Client runs itself,
// the Workspace's custom commands and the skills a user may invoke. The Daemon
// expands "/name args" itself when the message arrives, so for the latter two
// the Client only has to offer the names.
import DroidProtocol

public struct SlashItem: Sendable, Hashable, Identifiable {
    public enum Kind: String, Sendable, Hashable {
        case command, skill

        public var label: String { self == .command ? "Command" : "Skill" }
    }

    public var name: String
    public var description: String
    /// Hint for what follows the name, e.g. "command arguments".
    public var argumentHint: String?
    public var kind: Kind

    public var id: String { name }

    public init(name: String, description: String, argumentHint: String? = nil, kind: Kind) {
        self.name = name
        self.description = description
        self.argumentHint = argumentHint
        self.kind = kind
    }
}

/// Commands the composer handles before anything reaches the Daemon.
public let builtinSlashItems = [
    SlashItem(
        name: "compact", description: "Summarise the conversation and continue in a new session",
        argumentHint: "[instructions]", kind: .command)
]

/// The Daemon's commands, then the skills a user may invoke that are on and not
/// named like a command already.
public func slashItems(commands: [CustomCommandInfo], skills: [SkillInfo]) -> [SlashItem] {
    var items: [SlashItem] = []
    var taken: Set<String> = []
    for command in commands where !taken.contains(command.name) {
        taken.insert(command.name)
        items.append(
            SlashItem(
                name: command.name, description: command.description, argumentHint: command.argumentHint,
                kind: .command))
    }
    for skill in skills {
        if skill.userInvocable == false || skill.enabled == false || taken.contains(skill.name) { continue }
        items.append(SlashItem(name: skill.name, description: skill.description ?? "", kind: .skill))
    }
    return items
}

/// `builtins` are the commands the caller handles itself; they come first and
/// win over a Workspace command of the same name.
public func mergeSlashItems(_ builtins: [SlashItem], _ fromDaemon: [SlashItem]) -> [SlashItem] {
    if fromDaemon.isEmpty { return builtins }
    let names = Set(builtins.map(\.name))
    return builtins + fromDaemon.filter { !names.contains($0.name) }
}

/// The "/word" being typed at the start of the composer, or nil. `caret` is in
/// UTF-16 units, as a text view's selection is.
public func slashQuery(_ text: String, caret: Int) -> String? {
    let units = Array(text.utf16)
    guard caret >= 0, caret <= units.count else { return nil }
    let head = String(decoding: units[..<caret], as: UTF16.self)
    let tail = String(decoding: units[caret...], as: UTF16.self)
    guard head.hasPrefix("/") else { return nil }
    let word = head.dropFirst()
    if word.unicodeScalars.contains(where: { $0.properties.isWhitespace }) { return nil }
    if tail.unicodeScalars.contains(where: { $0.properties.isWhitespace }) { return nil }
    return String(word)
}

public func filterSlashItems(_ items: [SlashItem], query: String, limit: Int = 8) -> [SlashItem] {
    let q = query.lowercased()
    let starts = items.filter { $0.name.lowercased().hasPrefix(q) }
    let contains = items.filter { item in
        !starts.contains(item) && (q.isEmpty || item.name.lowercased().contains(q))
    }
    return Array((starts + contains).prefix(limit))
}

/// The command or skill a message starts with, once its name is complete
/// ("/name " then the rest): the composer shows it as a tag before the text.
public func pickedSlashItem(_ text: String, items: [SlashItem]) -> (item: SlashItem, rest: String)? {
    guard text.hasPrefix("/") else { return nil }
    let afterSlash = text.unicodeScalars.dropFirst()
    guard let space = afterSlash.firstIndex(where: { $0.properties.isWhitespace }),
        afterSlash[space] == " ", space > afterSlash.startIndex
    else { return nil }
    let name = String(String.UnicodeScalarView(afterSlash[..<space]))
    guard let item = items.first(where: { $0.name == name }) else { return nil }
    return (item, String(String.UnicodeScalarView(afterSlash[afterSlash.index(after: space)...])))
}
