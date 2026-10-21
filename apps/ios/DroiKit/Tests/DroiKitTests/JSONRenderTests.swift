import DroidProtocol
import Testing

@testable import DroiKit

private let table =
    "{\"root\":\"t\",\"elements\":{\"t\":{\"type\":\"Table\",\"props\":{\"columns\":[{\"header\":\"A\",\"key\":\"a\"}],\"rows\":[{\"a\":\"1\"}]},\"children\":[]}}}"

private func kinds(_ segments: [ReplySegment]) -> [String] {
    segments.map { segment in
        switch segment {
        case .markdown: "markdown"
        case .render: "render"
        case .pending: "pending"
        }
    }
}

@Suite struct JSONRenderTests {
    @Test func aReplyWithoutTheTagIsOneMarkdownSegment() {
        #expect(splitReply("Just **text**") == [.markdown("Just **text**")])
    }

    @Test func theTagOnItsOwnLineSplitsTheReplyAroundADrawnTree() {
        let segments = splitReply("Before\n\n<json-render>\(table)</json-render>\n\nAfter")
        #expect(kinds(segments) == ["markdown", "render", "markdown"])
        #expect(segments[0] == .markdown("Before\n"))
        #expect(segments[2] == .markdown("\nAfter"))
        guard case .render(let spec) = segments[1] else {
            Issue.record("expected a render segment")
            return
        }
        #expect(spec.root == "t")
        #expect(spec.elements["t"]?.type == "Table")
    }

    @Test func aTagMidLineAndOneSpanningLinesBothSplit() {
        #expect(kinds(splitReply("See <json-render>\(table)</json-render> below")) == ["markdown", "render", "markdown"])
        #expect(kinds(splitReply("<json-render>\n\(table)\n</json-render>")) == ["render"])
    }

    @Test func aTagInsideACodeFenceIsText() {
        let reply = "```html\n<json-render>{}</json-render>\n```"
        #expect(splitReply(reply) == [.markdown(reply)])
    }

    @Test func aTagInsideAnInlineCodeSpanIsText() {
        let reply = "Script 显示和 `<json-render>` 渲染都做完了。\n\nMore"
        #expect(splitReply(reply) == [.markdown(reply)])
        #expect(kinds(splitReply("``<json-render>`` then <json-render>\(table)</json-render>")) == ["markdown", "render"])
    }

    @Test func aTagStillOpenIsPendingAndBadJSONIsShownAsCode() {
        #expect(splitReply("Here:\n<json-render>{\"root\":").last == .pending(raw: "{\"root\":"))
        #expect(splitReply("<json-render>{oops}</json-render>") == [.markdown("```json\n{oops}\n```")])
    }

    @Test func fillsInMissingPropsAndChildrenDropsElementsWithoutAType() {
        #expect(
            parseRenderSpec("{\"root\":\"a\",\"elements\":{\"a\":{\"type\":\"Text\"},\"b\":{\"props\":{}}}}")
                == RenderSpec(root: "a", elements: ["a": RenderElement(type: "Text")]))
    }

    @Test func aRootThatIsNotAmongTheElementsDrawsNothing() {
        #expect(parseRenderSpec("{\"root\":\"x\",\"elements\":{}}") == nil)
        #expect(parseRenderSpec("[]") == nil)
    }

    @Test func childrenSkipMissingOnesAndOnesAlreadyDrawn() {
        let spec = parseRenderSpec(
            "{\"root\":\"a\",\"elements\":{\"a\":{\"type\":\"Box\",\"children\":[\"b\",\"zz\",\"a\"]},\"b\":{\"type\":\"Text\"}}}")!
        #expect(childrenOf(spec, "a", seen: ["a"]) == ["b"])
    }

    @Test func colourAndStatusNamesMapToWhatTheyMean() {
        #expect(toneOf("green") == .success)
        #expect(toneOf("Warning") == .warning)
        #expect(toneOf("red") == .error)
        #expect(toneOf("magenta") == .default)
    }

    @Test func propReadersTakeWhatTheyCan() {
        let props: [String: JSONValue] = [
            "s": "x", "n": 2.5, "b": true, "ns": " 7 ", "bad": "x7", "list": ["a", 1, true, nil],
            "nums": [1, "2", 3.5], "rows": [["a": 1], "no"],
        ]
        #expect(RenderProps.str(props, "s") == "x")
        #expect(RenderProps.str(props, "n") == "2.5")
        #expect(RenderProps.str(props, "b") == "true")
        #expect(RenderProps.str(props, "list") == "")
        #expect(RenderProps.num(props, "ns") == 7)
        #expect(RenderProps.num(props, "bad") == nil)
        #expect(RenderProps.strings(props, "list") == ["a", "1"])
        #expect(RenderProps.numbers(props, "nums") == [1, 3.5])
        #expect(RenderProps.records(props, "rows").count == 1)
        #expect(RenderProps.cell(["a": 1]) == "{\"a\":1}")
        #expect(RenderProps.cell(nil) == "")
        #expect(fraction(5, max: 4) == 1)
        #expect(fraction(1, max: 0) == 0)
        #expect(fraction(1, max: 4) == 0.25)
    }
}
