import Foundation
import Testing

@testable import DroiKit

/// A toy block parser: a block is a run of non-blank lines, a fenced code
/// block runs to its closing fence. Enough to check the streaming reuse.
private func parseBlocks(_ text: String) -> [MarkdownBlock<String>] {
    var blocks: [MarkdownBlock<String>] = []
    var offset = 0
    var current: (start: Int, line: Int, lines: [String])?
    var fence: String?
    for (index, line) in text.lines().enumerated() {
        defer { offset += line.utf8.count + 1 }
        let trimmed = line.trimmingCharacters(in: .whitespaces)
        if fence == nil, trimmed.isEmpty {
            if let block = current { blocks.append(MarkdownBlock(node: block.lines.joined(separator: "\n"), start: block.start, line: block.line)) }
            current = nil
            continue
        }
        if current == nil { current = (offset, index, []) }
        current!.lines.append(line)
        if trimmed.hasPrefix("```") { fence = fence == nil ? "```" : nil }
    }
    if let block = current { blocks.append(MarkdownBlock(node: block.lines.joined(separator: "\n"), start: block.start, line: block.line)) }
    return blocks
}

private func same(_ a: [MarkdownBlock<String>], _ b: [MarkdownBlock<String>]) -> Bool {
    a.map(\.node) == b.map(\.node) && a.map(\.start) == b.map(\.start) && a.map(\.line) == b.map(\.line)
}

@Suite struct MarkdownTests {
    let reply = [
        "## 修复方案", "", "First change `parse.ts` so the **settled** blocks are kept,", "then the *tail* is parsed again.",
        "", "- one", "- two with `code`", "", "```ts", "const a = 1", "", "function f() {}", "```", "", "Last paragraph.",
    ].joined(separator: "\n")

    @Test func everyPrefixParsesAsTheWholePrefixDoes() {
        var streaming = StreamingMarkdown<String>(parse: parseBlocks)
        let units = Array(reply.utf8)
        for end in 1...units.count {
            guard let prefix = String(bytes: units[..<end], encoding: .utf8) else { continue }
            let streamed = streaming.blocks(prefix)
            #expect(same(streamed, parseBlocks(repairStreaming(prefix))), "\(prefix)")
        }
    }

    @Test func keepsTheBlocksAboveAndReparsesWhenTheTextChangedRatherThanGrew() {
        var parses: [String] = []
        var streaming = StreamingMarkdown<String> { text in
            parses.append(text)
            return parseBlocks(text)
        }
        _ = streaming.blocks("# Title\n\nOne.\n\nTwo.\n\nTh")
        let next = streaming.blocks("# Title\n\nOne.\n\nTwo.\n\nThree")
        #expect(next.map(\.node) == ["# Title", "One.", "Two.", "Three"])
        #expect(parses.last == "Two.\n\nThree")
        let changed = streaming.blocks("# New\n\nText")
        #expect(parses.last == "# New\n\nText")
        #expect(changed.map(\.node) == ["# New", "Text"])
        _ = streaming.blocks("# New\n\nText\n\n[docs]: https://example.com")
        #expect(parses.last == "# New\n\nText\n\n[docs]: https://example.com")
    }

    @Test func closesAnOpenFenceWithTheSameMarker() {
        #expect(repairStreaming("```js\nlet a") == "```js\nlet a\n```")
        #expect(repairStreaming("~~~~\nx\n") == "~~~~\nx\n~~~~")
    }

    @Test func leavesClosedFencesAlone() {
        #expect(repairStreaming("```\na\n```\nafter") == "```\na\n```\nafter")
    }

    @Test func closesInlineCodeBoldAndItalicOnTheLastLine() {
        #expect(repairStreaming("run `npm te") == "run `npm te`")
        #expect(repairStreaming("this is **impor") == "this is **impor**")
        #expect(repairStreaming("an *aside") == "an *aside*")
        #expect(repairStreaming("`**` is fine") == "`**` is fine")
    }

    @Test func aListBulletIsNotAnOpenItalic() {
        #expect(repairStreaming("* item") == "* item")
    }

    private let fontSize = 13.0
    private let padding = 16.0

    @Test func givesOneWidthPerColumnSetByItsWidestCellInAnyRow() {
        let header = columnWidths([["a much longer cell", "b"], ["x", "y"]], fontSize: fontSize, padding: padding)
        let body = columnWidths([["a", "b"], ["a much longer cell", "y"]], fontSize: fontSize, padding: padding)
        #expect(header.count == 2)
        #expect(header[0] > header[1])
        #expect(body == header)
    }

    @Test func countsCJKCharactersAboutTwiceAsWideAsLatinOnes() {
        let widths = columnWidths([["管理员邀请码白名单用户", "abcdefghij"]], fontSize: fontSize, padding: padding)
        #expect(widths[0] - padding > (widths[1] - padding) * 1.5)
    }

    @Test func shortColumnsGetAFloorAndLongOnesACapBothGrowingWithTheTextSize() {
        let short = columnWidths([["a", "ab"]], fontSize: fontSize, padding: padding)
        #expect(short[0] == short[1])
        let long = String(repeating: "word ", count: 60)
        let capped = columnWidths([[long, long + long]], fontSize: fontSize, padding: padding)
        #expect(capped[0] == capped[1])
        #expect(capped[0] > short[0])
        let doubled = columnWidths([[long]], fontSize: fontSize * 2, padding: padding)
        #expect(doubled[0] - padding == 2 * (capped[0] - padding))
    }

    @Test func aCellsWidestLineCountsAndARaggedRowStillLinesUp() {
        let widths = columnWidths([["short\na much longer line"], ["x", "y"]], fontSize: fontSize, padding: padding)
        #expect(widths.count == 2)
        #expect(widths[0] == columnWidths([["a much longer line"]], fontSize: fontSize, padding: padding)[0])
    }
}
