import DroiKit
import SwiftUI

/// Droid's reasoning, folded to a line that says how long it thought.
struct ThinkingView: View {
    let text: String
    let durationMs: Double?
    let streaming: Bool
    @State private var open = false
    @Environment(\.palette) private var palette

    var body: some View {
        VStack(alignment: .leading, spacing: Space.xs) {
            Button {
                withAnimation(.snappy(duration: 0.2)) { open.toggle() }
            } label: {
                HStack(spacing: Space.sm) {
                    Image(systemName: "brain").font(.system(size: 12)).foregroundStyle(palette.mutedForeground)
                    ThemedText(title, size: FontSize.sm, tone: .muted)
                    Image(systemName: "chevron.right")
                        .font(.system(size: 10, weight: .semibold))
                        .foregroundStyle(palette.mutedForeground)
                        .rotationEffect(.degrees(open ? 90 : 0))
                    Spacer(minLength: 0)
                }
                .frame(minHeight: 28)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityValue(open ? "Expanded" : "Collapsed")
            if open {
                MarkdownView(blocks: MarkdownCache.blocks(for: text, streaming: streaming), muted: true)
                    .padding(.leading, Space.md)
                    .overlay(alignment: .leading) { Rectangle().fill(palette.border).frame(width: 2) }
            }
        }
    }

    private var title: String {
        if streaming && durationMs == nil { return "Thinking" }
        if let durationMs { return "Thought for \(formatDuration(durationMs))" }
        return "Thought"
    }
}

/// A reply: Markdown, with `<json-render>` blocks drawn in place.
struct ReplyView: View {
    let text: String
    let streaming: Bool

    var body: some View {
        if hasRenderTag(text) {
            VStack(alignment: .leading, spacing: Space.sm) {
                ForEach(Array(splitReply(text).enumerated()), id: \.offset) { _, segment in
                    switch segment {
                    case .markdown(let markdown):
                        MarkdownView(blocks: MarkdownCache.blocks(for: prepared(markdown), streaming: streaming))
                    case .render(let spec):
                        JSONRenderView(spec: spec)
                    case .pending(let raw):
                        if streaming {
                            PendingRenderView()
                        } else {
                            MarkdownView(blocks: MarkdownCache.blocks(for: "```json\n\(raw)\n```", streaming: false))
                        }
                    }
                }
            }
        } else {
            MarkdownView(blocks: MarkdownCache.blocks(for: prepared(text), streaming: streaming))
        }
    }

    private func prepared(_ text: String) -> String {
        streaming ? repairStreaming(text) : text
    }
}

private struct PendingRenderView: View {
    @Environment(\.palette) private var palette

    var body: some View {
        RoundedRectangle(cornerRadius: Radius.lg)
            .fill(palette.muted)
            .frame(height: 64)
            .overlay(Spinner(size: 14))
            .accessibilityLabel("Drawing output")
    }
}
