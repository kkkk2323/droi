import Foundation

/// A Fake Daemon the host started (Harness/fake-daemon-host.mjs). The host is
/// outside the simulator, so the tests reach it over HTTP.
struct FakeDaemon {
    struct Session {
        let id: String
        let title: String
        let cwd: String
    }

    let id: String
    let link: String
    let url: String
    let sessions: [Session]

    func session(_ title: String) -> Session {
        guard let session = sessions.first(where: { $0.title == title }) else { fatalError("no session titled \(title)") }
        return session
    }

    /// Sends the Fake Daemon a command (goDown, comeBack, requests, waitForRequest, ...) and waits for its answer.
    @discardableResult
    func command(_ op: String, _ fields: [String: Any] = [:]) -> [String: Any] {
        FakeDaemonHost.post("command/\(id)", fields.merging(["op": op]) { $1 })
    }

    /// The JSON-RPC requests the app has sent, oldest first.
    var requests: [(method: String, params: [String: Any])] {
        let list = command("requests")["requests"] as? [[String: Any]] ?? []
        return list.map { ($0["method"] as? String ?? "", $0["params"] as? [String: Any] ?? [:]) }
    }

    /// Params of every request with this method.
    func requests(_ method: String) -> [[String: Any]] {
        requests.filter { $0.method == method }.map(\.params)
    }

    /// Waits for the app to send a request with this method; nil when it never does.
    func waitForRequest(_ method: String, nth: Int = 1, timeout: TimeInterval = 10) -> [String: Any]? {
        let answer = command("waitForRequest", ["method": method, "nth": nth, "timeoutMs": Int(timeout * 1000)])
        return (answer["request"] as? [String: Any])?["params"] as? [String: Any]
    }

    func notify(_ session: Session, _ notification: [String: Any]) {
        command("notify", ["sessionId": session.id, "notification": notification])
    }

    func stop() {
        FakeDaemonHost.post("stop/\(id)", [:])
    }
}

enum FakeDaemonHost {
    private static var port: String { ProcessInfo.processInfo.environment["DROI_HOST_PORT"] ?? "41999" }

    /// Starts a Fake Daemon with the JSON scenario serve.ts reads (sessions, turn, validDirectories, input).
    static func start(_ scenario: [String: Any]) -> FakeDaemon {
        let answer = post("start", ["scenario": scenario])
        guard let id = answer["id"] as? String, let link = answer["link"] as? String, let url = answer["url"] as? String else {
            fatalError("the Fake Daemon did not start: \(answer)")
        }
        let sessions = (answer["sessions"] as? [[String: Any]] ?? []).map {
            FakeDaemon.Session(id: $0["sessionId"] as? String ?? "", title: $0["title"] as? String ?? "", cwd: $0["cwd"] as? String ?? "")
        }
        return FakeDaemon(id: id, link: link, url: url, sessions: sessions)
    }

    @discardableResult
    static func post(_ path: String, _ body: [String: Any]) -> [String: Any] {
        var request = URLRequest(url: URL(string: "http://127.0.0.1:\(port)/\(path)")!)
        request.httpMethod = "POST"
        request.httpBody = try? JSONSerialization.data(withJSONObject: body)
        request.timeoutInterval = 30
        let done = DispatchSemaphore(value: 0)
        nonisolated(unsafe) var result: [String: Any] = [:]
        URLSession.shared.dataTask(with: request) { data, _, error in
            if let data, let object = try? JSONSerialization.jsonObject(with: data) as? [String: Any] {
                result = object
            } else {
                result = ["error": error.map { String(describing: $0) } ?? "no answer"]
            }
            done.signal()
        }.resume()
        done.wait()
        return result
    }
}
