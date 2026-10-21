import Foundation
import Markdown

/// A reply's Markdown as plain values: what the view draws. Equatable, so a
/// block that did not change between two renders of a streaming reply is not
/// drawn again.
enum MDBlock: Hashable, Sendable {
    case paragraph([MDInline])
    case heading(level: Int, [MDInline])
    case code(language: String?, String)
    case math(String)
    case quote([MDBlock])
    case list(ordered: Bool, start: Int, items: [MDListItem])
    case table(MDTable)
    case rule
    case html(String)
}

struct MDListItem: Hashable, Sendable {
    var checked: Bool?
    var blocks: [MDBlock]
}

struct MDTable: Hashable, Sendable {
    var header: [[MDInline]]
    var rows: [[[MDInline]]]
}

indirect enum MDInline: Hashable, Sendable {
    case text(String)
    case strong([MDInline])
    case emphasis([MDInline])
    case strikethrough([MDInline])
    case code(String)
    case math(String)
    case link(URL?, [MDInline])
    case lineBreak
    case softBreak
    case image(alt: String)

    /// The inline's text without formatting, for table widths and labels.
    var plainText: String {
        switch self {
        case .text(let s), .code(let s), .math(let s): s
        case .strong(let c), .emphasis(let c), .strikethrough(let c), .link(_, let c): c.map(\.plainText).joined()
        case .lineBreak, .softBreak: " "
        case .image(let alt): alt.isEmpty ? "" : "[\(alt)]"
        }
    }
}

enum MarkdownParser {
    /// Parses CommonMark with GitHub's tables, task lists, strikethrough and
    /// autolinks, and TeX math between $ and $$.
    static func parse(_ text: String) -> [MDBlock] {
        let document = Document(parsing: MathMarkup.encode(text))
        return document.children.compactMap(block)
    }

    private static func block(_ markup: any Markup) -> MDBlock? {
        switch markup {
        case let p as Paragraph:
            return .paragraph(inlines(p.children))
        case let h as Heading:
            return .heading(level: h.level, inlines(h.children))
        case let c as CodeBlock:
            let code = c.code.hasSuffix("\n") ? String(c.code.dropLast()) : c.code
            if c.language == MathMarkup.blockLanguage { return .math(code) }
            return .code(language: c.language.flatMap { $0.isEmpty ? nil : $0 }, code)
        case let q as BlockQuote:
            return .quote(q.children.compactMap(block))
        case let l as OrderedList:
            return .list(ordered: true, start: Int(l.startIndex), items: l.listItems.map(item))
        case let l as UnorderedList:
            return .list(ordered: false, start: 1, items: l.listItems.map(item))
        case let t as Markdown.Table:
            let header = t.head.cells.map { inlines($0.children) }
            let rows = t.body.rows.map { row in Array(row.cells.map { inlines($0.children) }) }
            return .table(MDTable(header: Array(header), rows: Array(rows)))
        case is ThematicBreak:
            return .rule
        case let h as HTMLBlock:
            return .html(h.rawHTML.trimmingCharacters(in: .newlines))
        default:
            let children = markup.children.compactMap(block)
            return children.isEmpty ? nil : .quote(children)
        }
    }

    private static func item(_ item: ListItem) -> MDListItem {
        let checked: Bool? =
            switch item.checkbox {
            case .checked: true
            case .unchecked: false
            case nil: nil
            }
        return MDListItem(checked: checked, blocks: item.children.compactMap(block))
    }

    private static func inlines(_ children: some Sequence<any Markup>) -> [MDInline] {
        var out: [MDInline] = []
        for child in children {
            let next: MDInline?
            switch child {
            case let t as Markdown.Text: next = .text(t.string)
            case let s as Strong: next = .strong(inlines(s.children))
            case let e as Emphasis: next = .emphasis(inlines(e.children))
            case let s as Strikethrough: next = .strikethrough(inlines(s.children))
            case let c as InlineCode:
                next = MathMarkup.decodeInline(c.code).map(MDInline.math) ?? .code(c.code)
            case let l as Markdown.Link:
                next = .link(l.destination.flatMap(URL.init(string:)), inlines(l.children))
            case is LineBreak: next = .lineBreak
            case is SoftBreak: next = .softBreak
            case let i as Markdown.Image: next = .image(alt: i.plainText)
            case let h as InlineHTML: next = .text(h.rawHTML)
            default: next = .text(child.format())
            }
            // Adjacent text runs (cmark splits at brackets and escapes) read as one.
            if case .text(let s)? = next, case .text(let previous)? = out.last {
                out[out.count - 1] = .text(previous + s)
            } else if let next {
                out.append(next)
            }
        }
        return out
    }
}

/// cmark does not know TeX math, so math is turned into markup it keeps as
/// written before parsing: a `$$` block becomes a fenced block of language
/// `math`, `$…$` an inline code span that starts with a private-use marker.
/// The rules follow micromark-extension-math, as the old renderer did:
/// a run of dollars opens and the next run of the same length closes,
/// nothing inside code counts, and `\$` is a dollar.
enum MathMarkup {
    static let blockLanguage = "\u{E000}math"
    private static let marker: Character = "\u{E000}"

    static func decodeInline(_ code: String) -> String? {
        guard code.first == marker else { return nil }
        return String(code.dropFirst())
    }

    static func encode(_ text: String) -> String {
        guard text.contains("$") else { return text }
        var out: [Substring] = []
        var lines = text.split(separator: "\n", omittingEmptySubsequences: false)[...]
        var fence: (char: Character, count: Int)?
        while let line = lines.popFirst() {
            let trimmed = line.drop { $0 == " " }
            let indent = line.count - trimmed.count
            if let open = fence {
                out.append(line)
                if indent < 4, run(of: open.char, in: trimmed) >= open.count,
                    trimmed.drop(while: { $0 == open.char }).allSatisfy(\.isWhitespace)
                {
                    fence = nil
                }
                continue
            }
            if indent < 4, let c = trimmed.first, c == "`" || c == "~", run(of: c, in: trimmed) >= 3 {
                fence = (c, run(of: c, in: trimmed))
                out.append(line)
                continue
            }
            if indent < 4, trimmed.hasPrefix("$$") {
                let dollars = run(of: "$", in: trimmed)
                let rest = trimmed.dropFirst(dollars)
                // `$$x$$` on one line is inline math, as micromark reads it.
                if !rest.contains("$") {
                    var tex: [Substring] = []
                    var closed = false
                    while let next = lines.popFirst() {
                        let t = next.drop { $0 == " " }
                        if run(of: "$", in: t) >= dollars, t.drop(while: { $0 == "$" }).allSatisfy(\.isWhitespace) {
                            closed = true
                            break
                        }
                        tex.append(next)
                    }
                    _ = closed
                    let body = tex.joined(separator: "\n")
                    let ticks = String(repeating: "`", count: max(3, longestRun(of: "`", in: body) + 1))
                    out.append(Substring(ticks + blockLanguage))
                    out.append(contentsOf: tex)
                    out.append(Substring(ticks))
                    continue
                }
            }
            out.append(Substring(encodeInline(line)))
        }
        return out.joined(separator: "\n")
    }

    private static func encodeInline(_ line: Substring) -> String {
        guard line.contains("$") else { return String(line) }
        let chars = Array(line)
        var out = ""
        var i = 0
        while i < chars.count {
            let c = chars[i]
            if c == "\\", i + 1 < chars.count {
                out.append(c)
                out.append(chars[i + 1])
                i += 2
                continue
            }
            if c == "`" {
                // A code span is copied as is; an unclosed run is literal.
                let n = runLength(chars, from: i, of: "`")
                if let close = findRun(chars, from: i + n, of: "`", length: n) {
                    out.append(contentsOf: chars[i..<(close + n)])
                    i = close + n
                } else {
                    out.append(contentsOf: chars[i..<(i + n)])
                    i += n
                }
                continue
            }
            if c == "$" {
                let n = runLength(chars, from: i, of: "$")
                if let close = findRun(chars, from: i + n, of: "$", length: n), close > i + n {
                    var tex = String(chars[(i + n)..<close])
                    // micromark strips one space on each side when both are there.
                    if tex.count >= 2, tex.first == " ", tex.last == " ", tex.contains(where: { $0 != " " }) {
                        tex = String(tex.dropFirst().dropLast())
                    }
                    let ticks = String(repeating: "`", count: longestRun(of: "`", in: tex) + 1)
                    let pad = tex.hasPrefix("`") || tex.hasSuffix("`") ? " " : ""
                    out += ticks + pad + String(marker) + tex + pad + ticks
                    i = close + n
                    continue
                }
                out.append(contentsOf: chars[i..<(i + n)])
                i += n
                continue
            }
            out.append(c)
            i += 1
        }
        return out
    }

    private static func run(of c: Character, in s: Substring) -> Int {
        s.prefix { $0 == c }.count
    }

    private static func runLength(_ chars: [Character], from i: Int, of c: Character) -> Int {
        var j = i
        while j < chars.count, chars[j] == c { j += 1 }
        return j - i
    }

    /// The start of the next run of exactly `length` of `c` at or after `from`.
    private static func findRun(_ chars: [Character], from: Int, of c: Character, length: Int) -> Int? {
        var j = from
        while j < chars.count {
            if chars[j] == c {
                let n = runLength(chars, from: j, of: c)
                if n == length { return j }
                j += n
            } else {
                if c == "$", chars[j] == "\\" { j += 2; continue }
                j += 1
            }
        }
        return nil
    }

    private static func longestRun(of c: Character, in s: String) -> Int {
        var best = 0, cur = 0
        for ch in s {
            cur = ch == c ? cur + 1 : 0
            best = max(best, cur)
        }
        return best
    }
}
