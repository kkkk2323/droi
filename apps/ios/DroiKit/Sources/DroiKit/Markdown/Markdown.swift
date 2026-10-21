// Markdown helpers for a reply that is still streaming. It may end inside a
// code fence or a span; repairStreaming closes it first so the half-written
// part renders as what it will become. StreamingMarkdown re-parses only the
// tail of a growing reply, so an update to a long reply costs what one to a
// short reply does and the settled blocks keep their identity.
import Foundation

/// Closes what a cut-off reply left open: a code fence, then (outside code) an
/// inline code span, bold and italic markers on the last line.
public func repairStreaming(_ text: String) -> String {
    var open: String?
    for line in text.lines() {
        guard let fence = fenceRun(line) else { continue }
        if let current = open {
            if fence.first == current.first && fence.count >= current.count && line.trimmed == fence {
                open = nil
            }
        } else {
            open = fence
        }
    }
    if let open { return text + (text.hasSuffix("\n") ? "" : "\n") + open }

    let lastLine = text.lines().last ?? ""
    var repaired = text
    var rest = lastLine
    if rest.occurrences(of: "`") % 2 == 1 {
        repaired += "`"
        if let tick = rest.lastIndex(of: "`") { rest = String(rest[..<tick]) }
    }
    // Markers inside a code span do not count.
    let outside = withoutCodeSpans(rest)
    if outside.occurrences(of: "**") % 2 == 1 {
        repaired += "**"
    } else if outside.replacingOccurrences(of: "**", with: "").occurrences(of: "*") % 2 == 1
        && !isBullet(outside)
    {
        repaired += "*"
    }
    return repaired
}

/// The run of three or more backticks or tildes opening a fence, after at most three spaces.
private func fenceRun(_ line: String) -> String? {
    var rest = Substring(line)
    var spaces = 0
    while spaces < 3, rest.first == " " {
        rest = rest.dropFirst()
        spaces += 1
    }
    guard let marker = rest.first, marker == "`" || marker == "~" else { return nil }
    let run = rest.prefix { $0 == marker }
    return run.count >= 3 ? String(run) : nil
}

/// The text with each "`…`" pair taken out, left to right.
private func withoutCodeSpans(_ text: String) -> String {
    var out = ""
    var rest = Substring(text)
    while let open = rest.firstIndex(of: "`") {
        let after = rest[rest.index(after: open)...]
        guard let close = after.firstIndex(of: "`") else { break }
        out += rest[..<open]
        rest = after[after.index(after: close)...]
    }
    return out + rest
}

/// `* item`: a list bullet, not an open italic.
private func isBullet(_ line: String) -> Bool {
    let rest = line.drop { $0 == " " || $0 == "\t" }
    guard rest.first == "*", let next = rest.dropFirst().first else { return false }
    return next.isWhitespace
}

/// A block of a parsed reply and where it starts in the whole text, which
/// also serves as its stable identity.
public struct MarkdownBlock<Node: Sendable>: Sendable {
    public var node: Node
    /// UTF-8 offset in the whole text.
    public var start: Int
    /// 0-based line in the whole text.
    public var line: Int

    public init(node: Node, start: Int, line: Int) {
        self.node = node
        self.start = start
        self.line = line
    }
}

/// Parses a streaming reply with any block parser, again only from the last
/// block that may still change. Markdown settles its blocks line by line, so a
/// block that began on a finished line ends every block before it for good;
/// only the line still being written can turn out to continue the block above
/// it (`1` becoming `1.` joins a list). The blocks before the last one that
/// began on a finished line are kept, and only the text from that block's
/// line on is parsed again. A reply with a link or footnote definition, which
/// resolves references anywhere in the document, is parsed whole.
public struct StreamingMarkdown<Node: Sendable> {
    /// Parses a text into its top-level blocks, each with its UTF-8 offset and 0-based line in that text.
    public typealias Parse = (String) -> [MarkdownBlock<Node>]

    private let parse: Parse
    private var last: (text: String, blocks: [MarkdownBlock<Node>], kept: Int, tailStart: Int, tailLine: Int)?

    public init(parse: @escaping Parse) {
        self.parse = parse
    }

    public mutating func blocks(_ text: String) -> [MarkdownBlock<Node>] {
        if let last, last.text == text { return last.blocks }
        let reuse = last.map { text.hasPrefix($0.text) } == true && !hasDefinition(text)
        let kept = reuse ? Array(last!.blocks.prefix(last!.kept)) : []
        let base = reuse ? last!.tailStart : 0
        let lines = reuse ? last!.tailLine : 0
        let utf8 = Array(text.utf8)
        let tailText = String(decoding: utf8[base...], as: UTF8.self)
        let tail = parse(repairStreaming(tailText)).map { block in
            MarkdownBlock(node: block.node, start: block.start + base, line: block.line + lines)
        }
        let lastLineStart = (utf8.lastIndex(of: UInt8(ascii: "\n")) ?? -1) + 1
        var next = (kept: kept.count, tailStart: base, tailLine: lines)
        for index in tail.indices.reversed() where tail[index].start < lastLineStart {
            let start = tail[index].start
            let lineStart = start == 0 ? 0 : (utf8[..<start].lastIndex(of: UInt8(ascii: "\n")) ?? -1) + 1
            next = (kept.count + index, lineStart, tail[index].line)
            break
        }
        let blocks = kept + tail
        last = (text, blocks, next.kept, next.tailStart, next.tailLine)
        return blocks
    }
}

/// `[label]: …` at a line's start, after at most three spaces.
private func hasDefinition(_ text: String) -> Bool {
    for line in text.lines() {
        var rest = Substring(line)
        var spaces = 0
        while spaces < 3, rest.first == " " {
            rest = rest.dropFirst()
            spaces += 1
        }
        guard rest.first == "[", let close = rest.firstIndex(of: "]") else { continue }
        let label = rest[rest.index(after: rest.startIndex)..<close]
        if !label.isEmpty, rest[rest.index(after: close)...].hasPrefix(":") { return true }
    }
    return false
}

// Geist's Latin glyphs average a little over half an em; wide (CJK, fullwidth,
// emoji) glyphs are a full em.
private let narrowEm = 0.6
private let wideEm = 1.0
// In ems of the table's text, so both grow with the reader's text size.
private let minEm = 4.0
private let maxEm = 17.0

private let wideRanges: [ClosedRange<UInt32>] = [
    0x1100...0x115F, 0x2E80...0x303E, 0x3041...0x33FF, 0x3400...0x4DBF, 0x4E00...0x9FFF, 0xA000...0xA4CF,
    0xAC00...0xD7A3, 0xF900...0xFAFF, 0xFE30...0xFE4F, 0xFF00...0xFF60, 0xFFE0...0xFFE6, 0x1F300...0x1FAFF,
    0x20000...0x3FFFD,
]

/// Column widths for a Markdown table, from each cell's visible text (line
/// breaks as "\n"). Giving every cell of a column the same width is what lines
/// the columns up. The width is estimated from the text rather than measured,
/// so a table renders in one pass and stays steady while a reply streams; a
/// cell that comes out wider than its estimate wraps.
public func columnWidths(_ rows: [[String]], fontSize: Double, padding: Double) -> [Double] {
    var ems: [Double] = []
    for row in rows {
        for (column, cell) in row.enumerated() {
            let widest = cell.lines().map(lineEm).max() ?? 0
            if column < ems.count {
                ems[column] = max(ems[column], widest)
            } else {
                ems.append(contentsOf: Array(repeating: 0, count: column - ems.count))
                ems.append(widest)
            }
        }
    }
    return ems.map { em in (min(max(em, minEm), maxEm) * fontSize).rounded(.up) + padding }
}

private func lineEm(_ line: String) -> Double {
    line.unicodeScalars.reduce(0) { em, scalar in
        em + (wideRanges.contains { $0.contains(scalar.value) } ? wideEm : narrowEm)
    }
}
