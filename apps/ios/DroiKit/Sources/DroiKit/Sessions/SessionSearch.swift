// Full-text search over every Session on the computer, done by the Daemon
// (`daemon.search_sessions`), which indexes the session files; no Client keeps
// an index of its own. A hit comes back with snippets in which the Daemon
// wraps the matched words in <mark> tags.
import DroidProtocol
import Foundation

public struct SnippetRun: Sendable, Hashable {
    public var text: String
    public var match: Bool
    /// Where the run starts in the snippet, in UTF-16 units; a stable key for rendering.
    public var offset: Int

    public init(text: String, match: Bool, offset: Int) {
        self.text = text
        self.match = match
        self.offset = offset
    }
}

public struct SessionSearchHit: Sendable, Hashable, Identifiable {
    public var sessionId: String
    public var title: String
    /// Unix epoch seconds, like SessionSummary.
    public var updatedAt: Double?
    /// The best snippet, cut into plain runs and matched runs.
    public var snippet: [SnippetRun]

    public var id: String { sessionId }

    public init(_ result: DaemonSearchSessionsSessionResult) {
        sessionId = result.sessionId
        let title = result.title?.trimmed ?? ""
        self.title = title.isEmpty ? "Untitled session" : title
        updatedAt = result.updatedAt.flatMap { $0 > 0 ? ($0 / 1000).rounded(.down) : nil }
        snippet = snippetRuns(result.hits.first?.snippets.first ?? "")
    }
}

/// Fewer characters and the Daemon scans thousands of files for nothing much.
public let searchMinLength = 2
public let searchLimit = 20
public let searchDebounce: Duration = .milliseconds(250)

/// Whether a query is long enough to be searched.
public func isSearchable(_ query: String) -> Bool {
    query.trimmed.count >= searchMinLength
}

/// What `daemon.search_sessions` is asked for a query.
public func searchParams(_ query: String) -> SearchSessionsParams {
    SearchSessionsParams(
        query: query.trimmed, kind: .messageText, limitSessions: Double(searchLimit), limitHitsPerSession: 1,
        contextChars: 80)
}

/// Splits a Daemon snippet on its <mark> tags; any other tag is dropped.
public func snippetRuns(_ snippet: String) -> [SnippetRun] {
    var runs: [SnippetRun] = []
    var offset = 0
    var parts: [String] = []
    var rest = Substring(snippet)
    while let next = nextMarkTag(rest) {
        parts.append(String(rest[..<next.lowerBound]))
        rest = rest[next.upperBound...]
    }
    parts.append(String(rest))
    for (index, part) in parts.enumerated() {
        let text = collapseWhitespace(stripTags(part))
        if !text.isEmpty { runs.append(SnippetRun(text: text, match: index % 2 == 1, offset: offset)) }
        offset += text.utf16.count
    }
    return runs
}

/// The first `<mark>` or `</mark>`.
private func nextMarkTag(_ text: Substring) -> Range<Substring.Index>? {
    let open = text.range(of: "<mark>")
    let close = text.range(of: "</mark>")
    switch (open, close) {
    case (let open?, let close?): return open.lowerBound < close.lowerBound ? open : close
    case (let open?, nil): return open
    case (nil, let close?): return close
    default: return nil
    }
}

private func stripTags(_ text: String) -> String {
    var out = ""
    var inTag = false
    var pending = ""
    for character in text {
        if inTag {
            if character == ">" && pending == "<" {
                out += "<>"
                inTag = false
                pending = ""
                continue
            }
            if character == "<" {
                out += pending
                pending = "<"
                continue
            }
            pending.append(character)
            if character == ">" {
                inTag = false
                pending = ""
            }
        } else if character == "<" {
            inTag = true
            pending = "<"
        } else {
            out.append(character)
        }
    }
    // A "<" that never closes is text, as the pattern `<[^>]+>` leaves it.
    return out + pending
}

private func collapseWhitespace(_ text: String) -> String {
    var out = String.UnicodeScalarView()
    var inSpace = false
    for scalar in text.unicodeScalars {
        if scalar.properties.isWhitespace {
            if !inSpace { out.append(" ") }
            inSpace = true
        } else {
            out.append(scalar)
            inSpace = false
        }
    }
    return String(out)
}
