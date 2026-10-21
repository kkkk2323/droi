// Rich terminal output in an assistant's reply: a `<json-render>` tag holding
// one line of JSON that describes a small tree of display components (a
// table, a status line, a bar chart). The Droid CLI draws it in the terminal;
// the Clients split the reply around it and draw the tree with their own
// components. A tag inside a code fence is text about the format, not output.
import DroidProtocol
import Foundation

public struct RenderElement: Sendable, Hashable {
    public var type: String
    public var props: [String: JSONValue]
    public var children: [String]

    public init(type: String, props: [String: JSONValue] = [:], children: [String] = []) {
        self.type = type
        self.props = props
        self.children = children
    }
}

public struct RenderSpec: Sendable, Hashable {
    public var root: String
    public var elements: [String: RenderElement]

    public init(root: String, elements: [String: RenderElement]) {
        self.root = root
        self.elements = elements
    }
}

public enum ReplySegment: Sendable, Hashable {
    case markdown(String)
    case render(RenderSpec)
    /// A tag still open: the reply is streaming, or it ended without closing it.
    case pending(raw: String)
}

private let openTag = "<json-render>"
private let closeTag = "</json-render>"

public func hasRenderTag(_ text: String) -> Bool {
    text.contains(openTag)
}

/// "```" or "~~~" after at most three blanks opening the line.
private func fenceMarker(_ line: String) -> String? {
    var rest = Substring(line)
    var blanks = 0
    while blanks < 3, let first = rest.unicodeScalars.first, first.properties.isWhitespace {
        rest = Substring(rest.unicodeScalars.dropFirst())
        blanks += 1
    }
    if rest.hasPrefix("```") { return "```" }
    if rest.hasPrefix("~~~") { return "~~~" }
    return nil
}

public func splitReply(_ text: String) -> [ReplySegment] {
    if !hasRenderTag(text) { return [.markdown(text)] }
    var segments: [ReplySegment] = []
    var markdown: [String] = []
    var capturing: String?
    var fence: String?

    func flush() {
        let joined = markdown.joined(separator: "\n")
        if !joined.isBlank { segments.append(.markdown(joined)) }
        markdown = []
    }
    func close(_ raw: String) {
        flush()
        // What cannot be drawn is still shown, as the JSON it is.
        if let spec = parseRenderSpec(raw) {
            segments.append(.render(spec))
        } else {
            segments.append(.markdown("```json\n" + raw.trimmed + "\n```"))
        }
    }

    for whole in text.lines() {
        var line = whole
        if let captured = capturing {
            guard let end = line.range(of: closeTag) else {
                capturing = captured + "\n" + line
                continue
            }
            close(captured + "\n" + line[..<end.lowerBound])
            capturing = nil
            line = String(line[end.upperBound...])
            if line.isBlank { continue }
        }
        if let marker = fenceMarker(line) {
            if fence == nil {
                fence = marker
            } else if marker == fence {
                fence = nil
            }
            markdown.append(line)
            continue
        }
        if fence != nil {
            markdown.append(line)
            continue
        }
        while true {
            guard let start = openTagIndex(line) else {
                if !line.isBlank || line == whole { markdown.append(line) }
                break
            }
            let before = String(line[..<start])
            if !before.isBlank { markdown.append(before) }
            let after = String(line[line.utf8.index(start, offsetBy: openTag.utf8.count)...])
            guard let end = after.range(of: closeTag) else {
                capturing = after
                break
            }
            close(String(after[..<end.lowerBound]))
            line = String(after[end.upperBound...])
            if line.isBlank { break }
        }
    }
    if let captured = capturing {
        flush()
        segments.append(.pending(raw: captured))
    }
    flush()
    return segments
}

/// Where the line's first tag opens; one inside an inline code span is text.
private func openTagIndex(_ line: String) -> String.Index? {
    let spans = codeSpans(line)
    var from = line.startIndex
    while let found = line.range(of: openTag, range: from..<line.endIndex) {
        let at = found.lowerBound
        if !spans.contains(where: { at > $0.lowerBound && at < $0.upperBound }) { return at }
        from = line.index(after: at)
    }
    return nil
}

/// The line's code spans: a backtick run up to the next run of the same length.
private func codeSpans(_ line: String) -> [Range<String.Index>] {
    var spans: [Range<String.Index>] = []
    var open: (at: String.Index, length: Int)?
    var index = line.startIndex
    while index < line.endIndex {
        guard line[index] == "`" else {
            index = line.index(after: index)
            continue
        }
        let start = index
        var length = 0
        while index < line.endIndex, line[index] == "`" {
            length += 1
            index = line.index(after: index)
        }
        if let current = open {
            if current.length == length {
                spans.append(current.at..<start)
                open = nil
            }
        } else {
            open = (start, length)
        }
    }
    return spans
}

public func parseRenderSpec(_ raw: String) -> RenderSpec? {
    guard case .object(let parsed)? = JSONText.parse(raw.trimmed),
        let root = parsed["root"]?.stringValue,
        case .object(let elements)? = parsed["elements"]
    else { return nil }
    var normalized: [String: RenderElement] = [:]
    for (id, value) in elements {
        guard case .object(let element) = value, let type = element["type"]?.stringValue else { continue }
        normalized[id] = RenderElement(
            type: type,
            props: element["props"]?.objectValue ?? [:],
            children: element["children"]?.arrayValue?.compactMap(\.stringValue) ?? [])
    }
    guard normalized[root] != nil else { return nil }
    return RenderSpec(root: root, elements: normalized)
}

/// The elements under `id` that exist, each drawn once: a spec that lists an
/// element twice, or loops back to an ancestor, still draws a finite tree.
public func childrenOf(_ spec: RenderSpec, _ id: String, seen: Set<String>) -> [String] {
    guard let element = spec.elements[id] else { return [] }
    return element.children.filter { spec.elements[$0] != nil && !seen.contains($0) }
}

// Prop readers: the model writes the props, so each takes what it can and
// falls back instead of throwing.
public enum RenderProps {
    public static func str(_ props: [String: JSONValue], _ key: String) -> String {
        props[key]?.scalarText ?? ""
    }

    public static func num(_ props: [String: JSONValue], _ key: String) -> Double? {
        switch props[key] {
        case .number(let value)? where value.isFinite:
            return value
        case .string(let text)? where !text.isBlank:
            guard let value = Double(text.trimmed), value.isFinite else { return nil }
            return value
        default:
            return nil
        }
    }

    public static func records(_ props: [String: JSONValue], _ key: String) -> [[String: JSONValue]] {
        props[key]?.arrayValue?.compactMap(\.objectValue) ?? []
    }

    public static func strings(_ props: [String: JSONValue], _ key: String) -> [String] {
        props[key]?.arrayValue?.compactMap { value in
            switch value {
            case .string(let text): text
            case .number: value.scalarText
            default: nil
            }
        } ?? []
    }

    public static func numbers(_ props: [String: JSONValue], _ key: String) -> [Double] {
        props[key]?.arrayValue?.compactMap { $0.doubleValue.flatMap { $0.isFinite ? $0 : nil } } ?? []
    }

    /// A cell's text: strings and numbers as they are, anything else as JSON.
    public static func cell(_ value: JSONValue?) -> String {
        guard let value, !value.isNull else { return "" }
        return value.scalarText ?? JSONText.stringify(value)
    }
}

/// The meaning behind a colour or status name, so each Client draws it with
/// its own tokens. Colour carries status only; anything else reads as plain.
public enum Tone: String, Sendable, Hashable {
    case `default`, muted, success, warning, error, info
}

private let tones: [String: Tone] = [
    "success": .success, "ok": .success, "done": .success, "completed": .success, "green": .success,
    "warning": .warning, "warn": .warning, "pending": .warning, "yellow": .warning,
    "error": .error, "danger": .error, "failed": .error, "red": .error,
    "info": .info, "tip": .info, "note": .info, "blue": .info, "cyan": .info,
    "gray": .muted, "grey": .muted, "dim": .muted, "muted": .muted, "secondary": .muted,
]

public func toneOf(_ name: String) -> Tone {
    tones[name.trimmed.lowercased()] ?? .default
}

/// A fraction for a bar, clamped to 0..1.
public func fraction(_ value: Double, max: Double) -> Double {
    guard max > 0 else { return 0 }
    return Swift.min(1, Swift.max(0, value / max))
}
