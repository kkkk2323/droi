import SwiftUI
import UIKit

/// Parsed replies, so a row that scrolls back into view does not parse again.
/// A streaming reply is parsed on every change and not kept.
@MainActor
enum MarkdownCache {
    private static let cache: NSCache<NSString, Box> = {
        let cache = NSCache<NSString, Box>()
        cache.countLimit = 200
        return cache
    }()

    private final class Box {
        let blocks: [MDBlock]
        init(_ blocks: [MDBlock]) { self.blocks = blocks }
    }

    static func blocks(for text: String, streaming: Bool) -> [MDBlock] {
        if streaming { return MarkdownParser.parse(text) }
        if let hit = cache.object(forKey: text as NSString) { return hit.blocks }
        let blocks = MarkdownParser.parse(text)
        cache.setObject(Box(blocks), forKey: text as NSString)
        return blocks
    }
}

/// Droi's Markdown, drawn with SwiftUI text in the web Client's type scale.
/// Inline runs share one Text so lines wrap as prose; blocks stack.
struct MarkdownView: View {
    let blocks: [MDBlock]
    var muted = false

    var body: some View {
        VStack(alignment: .leading, spacing: Space.sm) {
            ForEach(Array(blocks.enumerated()), id: \.offset) { _, block in
                BlockView(block: block, muted: muted).equatable()
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

private struct BlockView: View, Equatable {
    let block: MDBlock
    let muted: Bool
    @Environment(\.palette) private var palette
    @Environment(\.displayScale) private var displayScale
    @Environment(\.textScale) private var scale

    nonisolated static func == (a: Self, b: Self) -> Bool { a.block == b.block && a.muted == b.muted }

    var body: some View {
        switch block {
        case .paragraph(let inlines):
            prose(inlines)
        case .heading(let level, let inlines):
            prose(inlines, size: level <= 1 ? FontSize.xl : level == 2 ? FontSize.lg : FontSize.base, weight: .semibold)
                .padding(.top, Space.xs)
                .accessibilityAddTraits(.isHeader)
        case .code(let language, let code):
            CodeBlockView(language: language, code: code)
        case .math(let tex):
            MathBlockView(tex: tex, muted: muted)
        case .quote(let blocks):
            MarkdownView(blocks: blocks, muted: true)
                .padding(.leading, Space.md)
                .overlay(alignment: .leading) { Rectangle().fill(palette.border).frame(width: 2) }
        case .list(let ordered, let start, let items):
            VStack(alignment: .leading, spacing: Space.xs) {
                ForEach(Array(items.enumerated()), id: \.offset) { index, item in
                    HStack(alignment: .firstTextBaseline, spacing: Space.sm) {
                        marker(item, ordered: ordered, number: start + index)
                            .foregroundStyle(palette.mutedForeground)
                            .frame(minWidth: 14, alignment: .leading)
                        MarkdownView(blocks: item.blocks, muted: muted)
                    }
                }
            }
        case .table(let table):
            TableView(table: table, muted: muted)
        case .rule:
            Rectangle().fill(palette.border).frame(height: 1 / displayScale).padding(.vertical, Space.sm)
        case .html(let raw):
            prose([.text(raw)])
        }
    }

    @ViewBuilder
    private func marker(_ item: MDListItem, ordered: Bool, number: Int) -> some View {
        switch item.checked {
        case let checked?:
            Image(systemName: checked ? "checkmark.square" : "square")
                .font(.system(size: FontSize.base * scale * 0.9))
                .accessibilityLabel(checked ? "Done" : "Not done")
        case nil:
            Text(ordered ? "\(number)." : "•").font(.sans(FontSize.base * scale))
        }
    }

    private func prose(_ inlines: [MDInline], size: CGFloat = FontSize.base, weight: FontWeight = .regular) -> some View {
        InlineText(inlines: inlines, size: size * scale, weight: weight, muted: muted)
            .lineSpacing(22 * scale - size * scale * 1.2)
            .textSelection(.enabled)
            .fixedSize(horizontal: false, vertical: true)
    }
}

/// Inline Markdown as one Text: styled runs in an AttributedString, formulas
/// as images on the baseline.
struct InlineText: View {
    let inlines: [MDInline]
    var size: CGFloat = FontSize.base
    var weight: FontWeight = .regular
    var muted = false
    @Environment(\.palette) private var palette

    var body: some View {
        let ink = muted ? palette.mutedForeground : palette.foreground
        var builder = InlineBuilder(size: size, palette: palette, ink: UIColor(ink))
        builder.add(inlines, style: InlineStyle(weight: weight))
        return builder.text().foregroundStyle(ink).tint(ink)
    }
}

private struct InlineStyle {
    var weight: FontWeight
    var italic = false
    var mono = false
    var strike = false
    var link: URL?
}

@MainActor
private struct InlineBuilder {
    let size: CGFloat
    let palette: Palette
    let ink: UIColor
    private var parts: [Text] = []
    private var run = AttributedString()

    init(size: CGFloat, palette: Palette, ink: UIColor) {
        self.size = size
        self.palette = palette
        self.ink = ink
    }

    mutating func add(_ inlines: [MDInline], style: InlineStyle) {
        for inline in inlines {
            switch inline {
            case .text(let s): append(s, style)
            case .strong(let c):
                var s = style
                s.weight = .semibold
                add(c, style: s)
            case .emphasis(let c):
                var s = style
                s.italic = true
                add(c, style: s)
            case .strikethrough(let c):
                var s = style
                s.strike = true
                add(c, style: s)
            case .code(let code):
                var s = style
                s.mono = true
                append(code, s)
            case .link(let url, let c):
                var s = style
                s.link = url
                add(c, style: s)
            case .lineBreak: append("\n", style)
            case .softBreak: append(" ", style)
            case .image(let alt): if !alt.isEmpty { append("[\(alt)]", style) }
            case .math(let tex):
                if let formula = MathTypesetter.typeset(tex, display: false, fontSize: size, color: ink) {
                    flush()
                    parts.append(Text(Image(uiImage: formula.image)).baselineOffset(-formula.descent))
                } else {
                    var s = style
                    s.mono = true
                    append(tex, s)
                }
            }
        }
    }

    private mutating func append(_ string: String, _ style: InlineStyle) {
        var piece = AttributedString(string)
        if style.mono {
            piece.font = .mono(FontSize.sm / FontSize.base * size)
            piece.foregroundColor = palette.codeInline
        } else {
            piece.font = style.italic ? .sansOblique(size, style.weight) : .sans(size, style.weight)
        }
        if style.strike { piece.strikethroughStyle = .single }
        if let link = style.link {
            piece.link = link
            piece.underlineStyle = .single
        }
        run.append(piece)
    }

    private mutating func flush() {
        guard !run.characters.isEmpty else { return }
        parts.append(Text(run))
        run = AttributedString()
    }

    mutating func text() -> Text {
        flush()
        guard var out = parts.first else { return Text(verbatim: "") }
        for part in parts.dropFirst() { out = Text("\(out)\(part)") }
        return out
    }
}

private struct CodeBlockView: View {
    let language: String?
    let code: String
    @Environment(\.palette) private var palette
    @State private var copied = false

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack {
                Text(language ?? "").font(.mono(FontSize.xs)).foregroundStyle(palette.mutedForeground)
                Spacer()
                Button {
                    UIPasteboard.general.string = code
                    copied = true
                    Task {
                        try? await Task.sleep(for: .seconds(1.5))
                        copied = false
                    }
                } label: {
                    Image(systemName: copied ? "checkmark" : "doc.on.doc")
                        .font(.system(size: 12))
                        .foregroundStyle(copied ? palette.success : palette.mutedForeground)
                        .frame(width: 28, height: 28)
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityLabel(copied ? "Copied" : "Copy code")
            }
            .padding(.leading, Space.md)
            .padding(.trailing, Space.xs)
            .padding(.top, Space.xs)
            ScrollView(.horizontal, showsIndicators: false) {
                Text(verbatim: code)
                    .font(.mono(FontSize.xs))
                    .foregroundStyle(palette.foreground)
                    .lineSpacing(4)
                    .textSelection(.enabled)
                    .fixedSize(horizontal: true, vertical: true)
                    .padding(Space.md)
            }
        }
        .background(palette.codeBackground, in: RoundedRectangle(cornerRadius: Radius.lg))
        .overlay(RoundedRectangle(cornerRadius: Radius.lg).strokeBorder(palette.border, lineWidth: 0.5))
        .accessibilityElement(children: .contain)
        .accessibilityLabel(language.map { "\($0) code" } ?? "Code")
    }
}

private struct MathBlockView: View {
    let tex: String
    let muted: Bool
    @Environment(\.palette) private var palette
    @Environment(\.textScale) private var scale

    var body: some View {
        let ink = UIColor(muted ? palette.mutedForeground : palette.foreground)
        if let formula = MathTypesetter.typeset(tex, display: true, fontSize: FontSize.base * scale, color: ink) {
            ScrollView(.horizontal, showsIndicators: false) {
                Image(uiImage: formula.image)
                    .accessibilityLabel(tex)
                    .padding(.vertical, Space.xs)
                    .containerRelativeFrame(.horizontal, alignment: .center) { length, _ in
                        max(length, formula.image.size.width)
                    }
            }
        } else {
            CodeBlockView(language: "latex", code: tex)
        }
    }
}

private struct TableView: View {
    let table: MDTable
    let muted: Bool
    @Environment(\.palette) private var palette
    @Environment(\.textScale) private var scale

    var body: some View {
        let widths = TableLayout.columnWidths(
            rows: [table.header.map(cellText)] + table.rows.map { $0.map(cellText) },
            fontSize: FontSize.sm * scale, padding: 2 * Space.sm)
        ScrollView(.horizontal, showsIndicators: false) {
            VStack(alignment: .leading, spacing: 0) {
                row(table.header, widths: widths, header: true)
                ForEach(Array(table.rows.enumerated()), id: \.offset) { _, cells in
                    Rectangle().fill(palette.border).frame(height: 0.5)
                    row(cells, widths: widths, header: false)
                }
            }
            .overlay(RoundedRectangle(cornerRadius: Radius.md).strokeBorder(palette.border, lineWidth: 0.5))
        }
    }

    private func cellText(_ cell: [MDInline]) -> String { cell.map(\.plainText).joined() }

    private func row(_ cells: [[MDInline]], widths: [CGFloat], header: Bool) -> some View {
        HStack(alignment: .top, spacing: 0) {
            ForEach(Array(widths.enumerated()), id: \.offset) { index, width in
                InlineText(
                    inlines: index < cells.count ? cells[index] : [], size: FontSize.sm * scale,
                    weight: header ? .semibold : .regular, muted: muted
                )
                .fixedSize(horizontal: false, vertical: true)
                .padding(.horizontal, Space.sm)
                .padding(.vertical, Space.xs)
                .frame(width: width, alignment: .leading)
            }
        }
    }
}

/// Column widths estimated from the widest cell text, so a table renders in
/// one pass and stays steady while it streams; a cell that is wider wraps.
enum TableLayout {
    static let narrowEm: CGFloat = 0.6, wideEm: CGFloat = 1, minEm: CGFloat = 4, maxEm: CGFloat = 17

    static func columnWidths(rows: [[String]], fontSize: CGFloat, padding: CGFloat) -> [CGFloat] {
        let columns = rows.map(\.count).max() ?? 0
        return (0..<columns).map { column in
            let em = rows.map { row in column < row.count ? ems(row[column]) : 0 }.max() ?? 0
            return ceil(min(maxEm, max(minEm, em)) * fontSize) + padding
        }
    }

    private static func ems(_ text: String) -> CGFloat {
        text.unicodeScalars.reduce(0) { $0 + (isWide($1) ? wideEm : narrowEm) }
    }

    private static func isWide(_ s: Unicode.Scalar) -> Bool {
        switch s.value {
        case 0x1100...0x115F, 0x2E80...0xA4CF, 0xAC00...0xD7A3, 0xF900...0xFAFF, 0xFE30...0xFE4F,
            0xFF00...0xFF60, 0xFFE0...0xFFE6, 0x1F300...0x1FAFF, 0x20000...0x3FFFD:
            true
        default: false
        }
    }
}
