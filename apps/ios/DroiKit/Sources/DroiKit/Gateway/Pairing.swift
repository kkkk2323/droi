// Pairing: the Pairing Token and the Gateway's address from a pairing link
// (`<origin>/#pair=<token>[&gateway=<origin>]`), and the Paired Computer they
// become once the Gateway's /meta names the computer.
import DroidProtocol
import Foundation

public struct Pairing: Sendable, Hashable {
    public var token: String
    public var gateway: String?

    public init(token: String, gateway: String?) {
        self.token = token
        self.gateway = gateway
    }
}

public func parsePairingFragment(_ hash: String) -> Pairing? {
    let fragment = formDecode(hash.hasPrefix("#") ? String(hash.dropFirst()) : hash)
    guard let token = fragment["pair"], !token.isEmpty else { return nil }
    return Pairing(token: token, gateway: fragment["gateway"])
}

/// A pairing link pasted or scanned: the whole link, just its fragment, or
/// the bare token.
public func parsePairingInput(_ text: String) -> Pairing? {
    let trimmed = text.trimmed
    if trimmed.isEmpty { return nil }
    if let hashAt = trimmed.firstIndex(of: "#") {
        guard var pairing = parsePairingFragment(String(trimmed[hashAt...])) else { return nil }
        if pairing.gateway != nil { return pairing }
        if let url = URL(string: trimmed), url.scheme != nil, url.host() != nil {
            pairing.gateway = webOrigin(url)
        }
        return pairing
    }
    let tokenCharacters = CharacterSet(charactersIn: "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_-")
    if trimmed.unicodeScalars.allSatisfy(tokenCharacters.contains) { return Pairing(token: trimmed, gateway: nil) }
    return nil
}

/// A Gateway address as the user types it: "192.168.1.10:41417",
/// "my-mac.tailnet.ts.net:41417" or a full URL. Stored as the origin only.
public func normalizeAddress(_ text: String) -> String? {
    let trimmed = text.trimmed
    if trimmed.isEmpty || trimmed.unicodeScalars.contains(where: { $0.properties.isWhitespace }) { return nil }
    let withScheme = hasScheme(trimmed) ? trimmed : "http://\(trimmed)"
    guard let components = URLComponents(string: withScheme),
        let scheme = components.scheme?.lowercased(), scheme == "http" || scheme == "https",
        let host = components.percentEncodedHost, !host.isEmpty, isHostName(host),
        let url = components.url
    else { return nil }
    return webOrigin(url)
}

private func hasScheme(_ text: String) -> Bool {
    guard let colon = text.range(of: "://") else { return false }
    let scheme = text[..<colon.lowerBound]
    guard let first = scheme.unicodeScalars.first, first.isASCII, CharacterSet.letters.contains(first) else {
        return false
    }
    let allowed = CharacterSet(charactersIn: "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789+.-")
    return scheme.unicodeScalars.allSatisfy(allowed.contains)
}

// A URL parser may percent-encode what no host name may contain.
private func isHostName(_ host: String) -> Bool {
    let name = CharacterSet(charactersIn: "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789.-")
    let ipv6 = CharacterSet(charactersIn: "0123456789abcdefABCDEF:.")
    var bare = Substring(host)
    if bare.hasPrefix("[") && bare.hasSuffix("]") {
        bare = bare.dropFirst().dropLast()
        return !bare.isEmpty && bare.unicodeScalars.allSatisfy(ipv6.contains)
    }
    if bare.contains(":") { return bare.unicodeScalars.allSatisfy(ipv6.contains) }
    return bare.unicodeScalars.allSatisfy(name.contains)
}

/// A computer a pairing link leads to, ready to keep.
public struct PairingResult: Sendable, Hashable {
    public var id: String
    public var name: String
    public var address: String
    public var token: String
    public var version: String
}

public struct PairingError: Error, LocalizedError, Sendable, Hashable {
    public var message: String

    public init(_ message: String) {
        self.message = message
    }

    public var errorDescription: String? { message }
}

/// Turns a pasted (or scanned) pairing link into a Paired Computer: the link
/// gives the Gateway's address and the Pairing Token, and the Gateway's /meta
/// names the computer and gives the id that recognises it next time.
public func resolvePairing(_ text: String, fetch: HTTPFetch = HTTP.urlSession) async throws -> PairingResult {
    guard let pairing = parsePairingInput(text) else {
        throw PairingError("That is not a Droi pairing link.")
    }
    guard let address = pairing.gateway, let gateway = URL(string: address) else {
        throw PairingError("Paste the whole pairing link, with the computer’s address.")
    }
    let meta: JSONValue
    do {
        var request = URLRequest(url: Gateway.metaURL(gateway))
        request.setValue("application/json", forHTTPHeaderField: "Accept")
        let (data, response) = try await fetch(request)
        guard (200..<300).contains(response.statusCode) else {
            throw PairingError("HTTP \(response.statusCode)")
        }
        guard let parsed = JSONText.parse(String(decoding: data, as: UTF8.self)) else {
            throw PairingError("The answer is not JSON")
        }
        meta = parsed
    } catch {
        // Without a port the request goes to :80, where something else may answer.
        let hint =
            gateway.port != nil
            ? "Check that Remote Access is on in Droi on the computer and that the iPhone can reach it: the same network, Tailscale, or a proxy such as Surge."
            : "The address has no port; the pairing link Droi shows ends in one, such as :41417."
        throw PairingError("Cannot reach \(address) (\(error.localizedDescription)). \(hint)")
    }
    guard meta["app"]?.stringValue == "Droi", let id = meta["computerId"]?.stringValue, !id.isEmpty else {
        throw PairingError("\(address) is not a Droi that can pair with this app. Update Droi on the computer.")
    }
    let name = meta["name"]?.stringValue ?? ""
    return PairingResult(
        id: id,
        name: name.isEmpty ? (gateway.host() ?? address) : name,
        address: address,
        token: pairing.token,
        version: meta["version"]?.stringValue ?? "")
}
