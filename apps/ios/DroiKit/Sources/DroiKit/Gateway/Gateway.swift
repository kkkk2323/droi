// Contract between the Gateway (the native app's Remote Access) and the
// Client: the WebSocket path, the query parameter, /meta, and the Gateway's
// own HTTP requests (pairing check, Scratch Workspaces, session files).
import Foundation

public enum Gateway {
    /// WebSocket path the Client connects to; frames are forwarded to the Daemon.
    /// A plain GET on the same path answers 204 or 401 so a Client can tell a bad
    /// Pairing Token apart from an unreachable Daemon.
    public static let daemonPath = "/daemon"
    /// Query parameter carrying the Pairing Token.
    public static let tokenQuery = "token"
    /// Read-only endpoint describing the computer. Never contains secrets.
    public static let metaPath = "/meta"
    /// Value the Client puts in `daemon.authenticate.params.apiKey`, and in
    /// `daemon.initialize_session`'s `token`. The Gateway replaces it with the
    /// real Factory credential, so its content never matters; a fixed marker
    /// keeps the frame recognisable in logs and tests.
    public static let apiKeyPlaceholder = "droi-gateway"
    /// Scratch Workspaces (ADR 0008): POST with the token in the query.
    /// Creating answers 201 with `{"path":…}`; trash and restore take the
    /// folder in the `path` query parameter and answer 204.
    public static let scratchPath = "/scratch-workspaces"
    public static let scratchTrashPath = "/scratch-workspaces/trash"
    public static let scratchRestorePath = "/scratch-workspaces/restore"
    public static let scratchPathQuery = "path"
    /// Where the Daemon keeps a Session's transcript on the computer; answers
    /// 200 with `{"path":…}`, or 404 when there is no such file.
    public static let sessionFilePath = "/session-file"
    public static let sessionIdQuery = "sessionId"

    public static func daemonURL(_ gateway: URL, pairingToken: String) -> URL {
        let scheme = gateway.scheme?.lowercased() == "https" ? "wss" : "ws"
        return url(gateway, path: daemonPath, query: [(tokenQuery, pairingToken)], scheme: scheme)
    }

    public static func pairingCheckURL(_ gateway: URL, pairingToken: String) -> URL {
        url(gateway, path: daemonPath, query: [(tokenQuery, pairingToken)])
    }

    public static func metaURL(_ gateway: URL) -> URL {
        url(gateway, path: metaPath, query: [])
    }

    public static func scratchURL(_ gateway: URL, pairingToken: String, endpoint: String, path: String? = nil) -> URL {
        var query = [(tokenQuery, pairingToken)]
        if let path { query.append((scratchPathQuery, path)) }
        return url(gateway, path: endpoint, query: query)
    }

    public static func sessionFileURL(_ gateway: URL, pairingToken: String, sessionId: String) -> URL {
        url(gateway, path: sessionFilePath, query: [(tokenQuery, pairingToken), (sessionIdQuery, sessionId)])
    }

    private static func url(_ gateway: URL, path: String, query: [(String, String)], scheme: String? = nil) -> URL {
        var text = webOrigin(gateway, scheme: scheme) + path
        if !query.isEmpty {
            text += "?" + query.map { "\(formEncode($0.0))=\(formEncode($0.1))" }.joined(separator: "&")
        }
        return URL(string: text)!
    }
}

/// What `/meta` says about the computer.
public struct GatewayMeta: Codable, Sendable, Hashable {
    /// Always "Droi".
    public var app: String
    public var version: String
    public var remoteAccess: Bool
    /// The computer's display name, as the user named it in the system settings.
    public var name: String
    /// Stable id of the computer; survives restarts, Pairing Token resets and address changes.
    public var computerId: String

    public init(app: String = "Droi", version: String, remoteAccess: Bool, name: String, computerId: String) {
        self.app = app
        self.version = version
        self.remoteAccess = remoteAccess
        self.name = name
        self.computerId = computerId
    }
}

/// One HTTP exchange; the Client passes URLSession's, the tests a fake.
public typealias HTTPFetch = @Sendable (URLRequest) async throws -> (Data, HTTPURLResponse)

public enum HTTP {
    public static let urlSession: HTTPFetch = { request in
        let (data, response) = try await URLSession.shared.data(for: request)
        guard let http = response as? HTTPURLResponse else { throw URLError(.badServerResponse) }
        return (data, http)
    }
}

/// What a pairing check (GET /daemon) says about a Pairing Token.
public enum PairingCheck: Sendable, Hashable {
    /// 204: the token is good and the Daemon is up.
    case paired
    /// 401: the token was reset on the computer; no retry helps.
    case rejected
    /// Anything else: the Gateway or the Daemon is not reachable now.
    case unreachable(String)
}

/// The Gateway's own HTTP requests for one Paired Computer.
public struct GatewayClient: Sendable {
    public var gateway: URL
    public var pairingToken: String
    public var fetch: HTTPFetch

    public init(gateway: URL, pairingToken: String, fetch: @escaping HTTPFetch = HTTP.urlSession) {
        self.gateway = gateway
        self.pairingToken = pairingToken
        self.fetch = fetch
    }

    public func checkPairing() async -> PairingCheck {
        var request = URLRequest(url: Gateway.pairingCheckURL(gateway, pairingToken: pairingToken))
        request.cachePolicy = .reloadIgnoringLocalAndRemoteCacheData
        do {
            let (_, response) = try await fetch(request)
            switch response.statusCode {
            case 204: return .paired
            case 401: return .rejected
            default: return .unreachable("Gateway not reachable")
            }
        } catch {
            return .unreachable(error.localizedDescription)
        }
    }

    /// A fresh folder on the computer to start a Session in.
    public func createScratchWorkspace() async throws -> String {
        let data = try await post(Gateway.scratchPath)
        guard let path = JSONText.parse(String(decoding: data, as: UTF8.self))?["path"]?.stringValue else {
            throw GatewayError(message: "The computer answered without a folder.")
        }
        return path
    }

    /// Moves the folder to the Trash; an empty one is simply removed.
    public func trashScratchWorkspace(_ path: String) async throws {
        _ = try await post(Gateway.scratchTrashPath, path: path)
    }

    /// Recreates the folder if it is gone.
    public func restoreScratchWorkspace(_ path: String) async throws {
        _ = try await post(Gateway.scratchRestorePath, path: path)
    }

    /// Where the Session's transcript is on the computer; nil when unknown or unreachable.
    public func sessionFile(_ sessionId: String) async -> String? {
        var request = URLRequest(
            url: Gateway.sessionFileURL(gateway, pairingToken: pairingToken, sessionId: sessionId))
        request.cachePolicy = .reloadIgnoringLocalAndRemoteCacheData
        guard let (data, response) = try? await fetch(request), (200..<300).contains(response.statusCode) else {
            return nil
        }
        return JSONText.parse(String(decoding: data, as: UTF8.self))?["path"]?.stringValue
    }

    private func post(_ endpoint: String, path: String? = nil) async throws -> Data {
        var request = URLRequest(
            url: Gateway.scratchURL(gateway, pairingToken: pairingToken, endpoint: endpoint, path: path))
        request.httpMethod = "POST"
        request.cachePolicy = .reloadIgnoringLocalAndRemoteCacheData
        let (data, response) = try await fetch(request)
        if (200..<300).contains(response.statusCode) { return data }
        let reason = JSONText.parse(String(decoding: data, as: UTF8.self))?["error"]?.stringValue
        throw GatewayError(
            message: reason ?? "The computer could not make a folder (HTTP \(response.statusCode)).")
    }
}

public struct GatewayError: Error, LocalizedError, Sendable, Hashable {
    public var message: String

    public init(message: String) {
        self.message = message
    }

    public var errorDescription: String? { message }
}

/// A URL's origin as a browser writes it: scheme, lower-case host, and the
/// port unless it is the scheme's default.
func webOrigin(_ url: URL, scheme: String? = nil) -> String {
    let components = URLComponents(url: url, resolvingAgainstBaseURL: false)
    let original = (components?.scheme ?? "http").lowercased()
    var host = (components?.percentEncodedHost ?? "").lowercased()
    if host.contains(":") && !host.hasPrefix("[") { host = "[\(host)]" }
    var origin = "\(scheme ?? original)://\(host)"
    if let port = components?.port, port != defaultPort(original) { origin += ":\(port)" }
    return origin
}

private func defaultPort(_ scheme: String) -> Int? {
    switch scheme {
    case "http", "ws": 80
    case "https", "wss": 443
    default: nil
    }
}

/// application/x-www-form-urlencoded, as URLSearchParams writes a value.
func formEncode(_ text: String) -> String {
    var out = ""
    for byte in text.utf8 {
        switch byte {
        case UInt8(ascii: "a")...UInt8(ascii: "z"), UInt8(ascii: "A")...UInt8(ascii: "Z"),
            UInt8(ascii: "0")...UInt8(ascii: "9"), UInt8(ascii: "*"), UInt8(ascii: "-"), UInt8(ascii: "."),
            UInt8(ascii: "_"):
            out.unicodeScalars.append(Unicode.Scalar(byte))
        case UInt8(ascii: " "):
            out += "+"
        default:
            out += String(format: "%%%02X", byte)
        }
    }
    return out
}

/// The values of an application/x-www-form-urlencoded text, by name; the first of a repeated name wins.
func formDecode(_ text: String) -> [String: String] {
    var values: [String: String] = [:]
    for pair in text.split(separator: "&", omittingEmptySubsequences: true) {
        let parts = pair.split(separator: "=", maxSplits: 1, omittingEmptySubsequences: false)
        func decode(_ part: Substring) -> String {
            let spaced = part.replacingOccurrences(of: "+", with: " ")
            return spaced.removingPercentEncoding ?? spaced
        }
        let name = decode(parts[0])
        if values[name] == nil { values[name] = parts.count > 1 ? decode(parts[1]) : "" }
    }
    return values
}
