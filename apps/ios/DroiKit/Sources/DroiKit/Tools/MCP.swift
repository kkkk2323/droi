// MCP Servers as the Daemon reports them for a Session, and what a Client may
// do with each. The Daemon owns the configuration (`~/.factory/mcp.json` at
// the user level, the Workspace's `.factory/mcp.json` at the project level,
// the organization's policy above both) and the connections; a Client lists,
// switches, adds and removes through it, as the droid CLI's `/mcp` does.
import DroidProtocol
import Foundation

extension MCPServerStatus {
    public var label: String {
        switch rawValue {
        case "connecting": "Connecting"
        case "connected": "Connected"
        case "disconnected": "Disconnected"
        case "failed": "Failed"
        case "disabled": "Disabled"
        default: rawValue
        }
    }
}

/// Droi's Memory Server (ADR 0010), which the native app attaches through the
/// Runtime Overlay under this name.
public let droiMemoryServer = "droi-memory"

/// Its only switch is the Memory setting in the native app.
public func isDroiMemory(_ server: MCPServerStatusInfo) -> Bool {
    server.name == droiMemoryServer
}

/// The organization's servers are policy and Droi's Memory is the native app's; nothing here changes them.
public func isReadOnly(_ server: MCPServerStatusInfo) -> Bool {
    server.source == .org || isDroiMemory(server)
}

/// Only a server in the user's own `mcp.json` can be removed from here.
public func canRemove(_ server: MCPServerStatusInfo) -> Bool {
    server.source == .user && !isDroiMemory(server)
}

public func needsSignIn(_ server: MCPServerStatusInfo) -> Bool {
    server.requiresAuth == true && server.hasAuthTokens != true
}

/// What a Client's form collects for a new server; every field a string.
public struct ServerForm: Sendable, Hashable {
    public var name = ""
    public var type: MCPServerType = .stdio
    public var command = ""
    /// Whitespace-separated.
    public var args = ""
    public var url = ""
    /// One `Name: value` per line.
    public var headers = ""

    public init(
        name: String = "", type: MCPServerType = .stdio, command: String = "", args: String = "", url: String = "",
        headers: String = ""
    ) {
        self.name = name
        self.type = type
        self.command = command
        self.args = args
        self.url = url
        self.headers = headers
    }

    /// A catalogue entry as the form would hold it, ready to adjust and add.
    public init(registry entry: MCPRegistryServer) {
        self.init(
            name: entry.name, type: entry.type, command: entry.command ?? "",
            args: (entry.args ?? []).joined(separator: " "), url: entry.url ?? "")
    }
}

/// A new server as `daemon.add_mcp_server` takes it, without the Session.
public struct McpServerInput: Sendable, Hashable {
    public var name: String
    public var type: MCPServerType
    public var command: String?
    public var args: [String]?
    public var url: String?
    public var headers: [String: String]?

    public init(
        name: String, type: MCPServerType, command: String? = nil, args: [String]? = nil, url: String? = nil,
        headers: [String: String]? = nil
    ) {
        self.name = name
        self.type = type
        self.command = command
        self.args = args
        self.url = url
        self.headers = headers
    }

    public func params(sessionId: String) -> AddMCPServerParams {
        AddMCPServerParams(
            name: name, type: type, url: url, headers: headers, command: command, args: args, sessionId: sessionId)
    }
}

/// The Daemon's parameters for the form, or what is wrong with it.
public struct ServerFormError: Error, LocalizedError, Sendable, Hashable {
    public var message: String

    public init(_ message: String) {
        self.message = message
    }

    public var errorDescription: String? { message }
}

public func parseServerForm(_ form: ServerForm) -> Result<McpServerInput, ServerFormError> {
    let name = form.name.trimmed
    if name.isEmpty { return .failure(ServerFormError("Give the server a name.")) }
    let nameCharacters = CharacterSet(charactersIn: "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-")
    if !name.unicodeScalars.allSatisfy(nameCharacters.contains) {
        return .failure(ServerFormError("A name has letters, digits, \"-\" and \"_\" only."))
    }
    if form.type == .stdio {
        let command = form.command.trimmed
        if command.isEmpty { return .failure(ServerFormError("Give the command that starts the server.")) }
        let args = form.args.split(whereSeparator: \.isWhitespace).map(String.init)
        return .success(McpServerInput(name: name, type: .stdio, command: command, args: args.isEmpty ? nil : args))
    }
    let url = form.url.trimmed
    if !(url.hasPrefix("http://") || url.hasPrefix("https://")) {
        return .failure(ServerFormError("The URL starts with http:// or https://."))
    }
    var headers: [String: String] = [:]
    for line in form.headers.lines() where !line.isBlank {
        guard let colon = line.firstIndex(of: ":"), colon > line.startIndex else {
            return .failure(ServerFormError("A header is \"Name: value\"; \"\(line.trimmed)\" is not."))
        }
        headers[String(line[..<colon]).trimmed] = String(line[line.index(after: colon)...]).trimmed
    }
    return .success(McpServerInput(name: name, type: form.type, url: url, headers: headers.isEmpty ? nil : headers))
}

/// Whether a catalogue entry has a value to fill in before it can be added as
/// it is: an upper-case name in its command or URL (`--adyenApiKey=ADYEN_API_KEY`).
public func needsSetup(_ entry: MCPRegistryServer) -> Bool {
    ((entry.args ?? []) + [entry.url ?? ""]).contains(where: endsInPlaceholder)
}

// `(?:^|=)[A-Z][A-Z0-9]*_[A-Z0-9_]+$`
private func endsInPlaceholder(_ part: String) -> Bool {
    let candidate: Substring
    if let equals = part.lastIndex(of: "=") {
        let after = part[part.index(after: equals)...]
        candidate = isPlaceholder(after) ? after : Substring(part)
    } else {
        candidate = Substring(part)
    }
    return isPlaceholder(candidate)
}

private func isPlaceholder(_ text: Substring) -> Bool {
    let scalars = Array(text.unicodeScalars)
    guard let first = scalars.first, ("A"..."Z").contains(first) else { return false }
    let upper = CharacterSet(charactersIn: "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")
    guard let underscore = scalars.firstIndex(of: "_"), underscore + 1 < scalars.count else { return false }
    return scalars[1..<underscore].allSatisfy(upper.contains)
        && scalars[(underscore + 1)...].allSatisfy { upper.contains($0) || $0 == "_" }
}

/// The catalogue entries not added yet whose name or description matches the search.
public func filterRegistry(_ entries: [MCPRegistryServer], query: String, taken: [String]) -> [MCPRegistryServer] {
    let words = query.lowercased().split(whereSeparator: \.isWhitespace).map(String.init)
    return entries.filter { entry in
        if taken.contains(entry.name) { return false }
        let text = "\(entry.name) \(entry.description)".lowercased()
        return words.allSatisfy { text.contains($0) }
    }
}

/// The servers sorted for a list: the user's and the project's first, by name; the organization's after.
public func sortServers(_ servers: [MCPServerStatusInfo]) -> [MCPServerStatusInfo] {
    servers.sorted { a, b in
        if isReadOnly(a) != isReadOnly(b) { return !isReadOnly(a) }
        return a.name.localizedCompare(b.name) == .orderedAscending
    }
}
