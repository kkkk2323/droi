import DroidProtocol
import Foundation
import Synchronization
import Testing

@testable import DroiKit

/// A fetch that answers every request the same way and records the URLs.
private final class FakeFetch: Sendable {
    let urls = Mutex<[String]>([])
    let methods = Mutex<[String]>([])
    let answer: @Sendable () throws -> (Int, String)

    init(status: Int, body: String = "") {
        answer = { (status, body) }
    }

    init(throwing error: some Error & Sendable) {
        answer = { throw error }
    }

    var fetch: HTTPFetch {
        { request in
            self.urls.withLock { $0.append(request.url!.absoluteString) }
            self.methods.withLock { $0.append(request.httpMethod ?? "GET") }
            let (status, body) = try self.answer()
            let response = HTTPURLResponse(url: request.url!, statusCode: status, httpVersion: nil, headerFields: nil)!
            return (Data(body.utf8), response)
        }
    }
}

private struct Down: Error, LocalizedError {
    var errorDescription: String? { "Network request failed" }
}

@Suite struct PairingTests {
    @Test func takesTheWholeLinkAndReadsTheGatewayFromIt() {
        #expect(
            parsePairingInput(" http://192.168.5.123:41417/#pair=abc-123 ")
                == Pairing(token: "abc-123", gateway: "http://192.168.5.123:41417"))
    }

    @Test func anExplicitGatewayWinsABareFragmentOrTokenAlsoWorks() {
        #expect(parsePairingInput("http://a:1/#pair=t&gateway=http://b:2") == Pairing(token: "t", gateway: "http://b:2"))
        #expect(parsePairingInput("#pair=t") == Pairing(token: "t", gateway: nil))
        #expect(parsePairingInput("t0k_en") == Pairing(token: "t0k_en", gateway: nil))
        #expect(parsePairingFragment("#pair=a%2Bb&gateway=http%3A%2F%2Fc%3A3") == Pairing(token: "a+b", gateway: "http://c:3"))
    }

    @Test func anythingElseIsRejected() {
        #expect(parsePairingInput("") == nil)
        #expect(parsePairingInput("http://a:1/#/settings") == nil)
        #expect(parsePairingInput("hello world") == nil)
    }

    @Test func readsTheAddressAndTokenFromTheLinkAndTheNameAndIdFromMeta() async throws {
        let fake = FakeFetch(
            status: 200,
            body: #"{"app":"Droi","version":"1.2.0","remoteAccess":true,"name":"Studio Mac","computerId":"c-1"}"#)
        let result = try await resolvePairing(" http://192.168.5.123:41417/#pair=abc-123 ", fetch: fake.fetch)
        #expect(
            result
                == PairingResult(
                    id: "c-1", name: "Studio Mac", address: "http://192.168.5.123:41417", token: "abc-123", version: "1.2.0"))
        #expect(fake.urls.withLock { $0 } == ["http://192.168.5.123:41417/meta"])
    }

    @Test func aBareTokenOrAnythingElseIsRefusedBeforeAnyRequest() async {
        let fake = FakeFetch(status: 200, body: "{}")
        await #expect(throws: PairingError("Paste the whole pairing link, with the computer’s address.")) {
            try await resolvePairing("abc-123", fetch: fake.fetch)
        }
        await #expect(throws: PairingError("That is not a Droi pairing link.")) {
            try await resolvePairing("hello world", fetch: fake.fetch)
        }
        #expect(fake.urls.withLock { $0 }.isEmpty)
    }

    @Test func anUnreachableGatewayOrAnOldDroiSaysWhatToCheck() async {
        func message(_ text: String, _ fake: FakeFetch) async -> String {
            do {
                _ = try await resolvePairing(text, fetch: fake.fetch)
                return ""
            } catch {
                return error.localizedDescription
            }
        }
        let down = await message("http://10.0.0.2:1/#pair=t", FakeFetch(throwing: Down()))
        #expect(down.hasPrefix("Cannot reach http://10.0.0.2:1 (Network request failed). "))
        #expect(down.contains("Remote Access"))
        let redirect = await message("http://laptop.myhome/#pair=t", FakeFetch(status: 302, body: "<html>"))
        #expect(redirect.hasPrefix("Cannot reach http://laptop.myhome (HTTP 302). The address has no port"))
        let old = await message(
            "http://10.0.0.2:1/#pair=t", FakeFetch(status: 200, body: #"{"app":"Droi","version":"1.1.0","remoteAccess":true}"#))
        #expect(old.contains("Update Droi"))
    }

    @Test func addsHttpAndKeepsOnlyTheOrigin() {
        #expect(normalizeAddress(" 192.168.1.10:41417 ") == "http://192.168.1.10:41417")
        #expect(normalizeAddress("my-mac.tail1.ts.net:41417/") == "http://my-mac.tail1.ts.net:41417")
        #expect(normalizeAddress("https://mac.example.com/#pair=x") == "https://mac.example.com")
        #expect(normalizeAddress("HTTP://Mac.Local:80") == "http://mac.local")
        #expect(normalizeAddress("[::1]:41417") == "http://[::1]:41417")
    }

    @Test func refusesWhatIsNotAnHttpAddress() {
        #expect(normalizeAddress("") == nil)
        #expect(normalizeAddress("ftp://mac:21") == nil)
        #expect(normalizeAddress("http://") == nil)
        #expect(normalizeAddress("not a url at all") == nil)
        #expect(normalizeAddress("http://my%20mac:1") == nil)
    }
}

@Suite struct GatewayTests {
    let gateway = URL(string: "http://laptop.local:41417")!

    @Test func buildsTheDaemonAndPairingCheckURLs() {
        #expect(Gateway.daemonURL(gateway, pairingToken: "t k").absoluteString == "ws://laptop.local:41417/daemon?token=t+k")
        #expect(
            Gateway.daemonURL(URL(string: "https://mac.ts.net/x")!, pairingToken: "t").absoluteString
                == "wss://mac.ts.net/daemon?token=t")
        #expect(Gateway.pairingCheckURL(gateway, pairingToken: "t").absoluteString == "http://laptop.local:41417/daemon?token=t")
        #expect(
            Gateway.sessionFileURL(URL(string: "http://127.0.0.1:4000")!, pairingToken: "tok", sessionId: "a1b2c3").absoluteString
                == "http://127.0.0.1:4000/session-file?token=tok&sessionId=a1b2c3")
    }

    @Test func asksTheGatewayForAFolderAndAnswersItsPath() async throws {
        let fake = FakeFetch(status: 201, body: #"{"path":"/u/.droi/chats/2026-09-26-abcdef"}"#)
        let client = GatewayClient(gateway: gateway, pairingToken: "tok", fetch: fake.fetch)
        #expect(try await client.createScratchWorkspace() == "/u/.droi/chats/2026-09-26-abcdef")
        #expect(fake.urls.withLock { $0 } == ["http://laptop.local:41417/scratch-workspaces?token=tok"])
        #expect(fake.methods.withLock { $0 } == ["POST"])
    }

    @Test func namesTheFolderToTrashOrRestoreInTheQuery() async throws {
        let fake = FakeFetch(status: 204)
        let client = GatewayClient(gateway: gateway, pairingToken: "tok", fetch: fake.fetch)
        try await client.trashScratchWorkspace("/u/.droi/chats/2026-09-26-abcdef")
        try await client.restoreScratchWorkspace("/u/.droi/chats/2026-09-26-abcdef")
        let urls = fake.urls.withLock { $0 }.map { URLComponents(string: $0)! }
        #expect(urls.map(\.path) == ["/scratch-workspaces/trash", "/scratch-workspaces/restore"])
        #expect(urls.map { $0.queryItems?.first { $0.name == "path" }?.value } == [
            "/u/.droi/chats/2026-09-26-abcdef", "/u/.droi/chats/2026-09-26-abcdef",
        ])
    }

    @Test func aRefusalThrowsTheGatewaysReasonOrSaysWhatFailed() async {
        let refused = GatewayClient(
            gateway: gateway, pairingToken: "tok",
            fetch: FakeFetch(status: 400, body: #"{"error":"/etc is not a Scratch Workspace"}"#).fetch)
        await #expect(throws: GatewayError(message: "/etc is not a Scratch Workspace")) {
            try await refused.trashScratchWorkspace("/etc")
        }
        let missing = GatewayClient(gateway: gateway, pairingToken: "tok", fetch: FakeFetch(status: 404).fetch)
        await #expect(throws: GatewayError(message: "The computer could not make a folder (HTTP 404).")) {
            try await missing.createScratchWorkspace()
        }
    }

    @Test func aSessionFileIsAskedForWithTheTokenAndTheSessionId() async {
        let fake = FakeFetch(status: 200, body: #"{"path":"/x/a1b2c3.jsonl"}"#)
        let client = GatewayClient(gateway: URL(string: "http://127.0.0.1:4000")!, pairingToken: "tok", fetch: fake.fetch)
        #expect(await client.sessionFile("a1b2c3") == "/x/a1b2c3.jsonl")
        #expect(fake.urls.withLock { $0 } == ["http://127.0.0.1:4000/session-file?token=tok&sessionId=a1b2c3"])
        let missing = GatewayClient(gateway: gateway, pairingToken: "tok", fetch: FakeFetch(status: 404).fetch)
        #expect(await missing.sessionFile("a") == nil)
        let down = GatewayClient(gateway: gateway, pairingToken: "tok", fetch: FakeFetch(throwing: Down()).fetch)
        #expect(await down.sessionFile("a") == nil)
    }

    @Test func thePairingCheckTellsABadTokenFromAnUnreachableGateway() async {
        func check(_ fake: FakeFetch) async -> PairingCheck {
            await GatewayClient(gateway: gateway, pairingToken: "t", fetch: fake.fetch).checkPairing()
        }
        #expect(await check(FakeFetch(status: 204)) == .paired)
        #expect(await check(FakeFetch(status: 401)) == .rejected)
        #expect(await check(FakeFetch(status: 503)) == .unreachable("Gateway not reachable"))
        #expect(await check(FakeFetch(throwing: Down())) == .unreachable("Network request failed"))
    }

    @Test func metaDecodes() throws {
        let meta = try JSONDecoder().decode(
            GatewayMeta.self,
            from: Data(#"{"app":"Droi","version":"1.2.0","remoteAccess":true,"name":"Mac","computerId":"c"}"#.utf8))
        #expect(meta == GatewayMeta(version: "1.2.0", remoteAccess: true, name: "Mac", computerId: "c"))
    }
}
