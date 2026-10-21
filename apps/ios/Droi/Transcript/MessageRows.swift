import DroiKit
import DroidProtocol
import SwiftUI

/// What transcript rows need from the screen around them: opening another
/// Session (a subagent's), and how a subagent run is going.
struct TranscriptLinks {
    var open: (String) -> Void = { _ in }
    var subagentSession: (ToolCall) -> String? = { _ in nil }
    var subagentRun: (String) -> SubagentRun? = { _ in nil }
}

private struct TranscriptLinksKey: EnvironmentKey {
    nonisolated(unsafe) static let defaultValue = TranscriptLinks()
}

extension EnvironmentValues {
    var transcriptLinks: TranscriptLinks {
        get { self[TranscriptLinksKey.self] }
        set { self[TranscriptLinksKey.self] = newValue }
    }
}

/// The user's message, as a bubble on the right with its images above it.
struct UserMessageView: View, Equatable {
    let entry: TranscriptEntry
    @Environment(\.palette) private var palette
    @Environment(\.textScale) private var scale

    nonisolated static func == (a: Self, b: Self) -> Bool { a.entry == b.entry }

    var body: some View {
        let text = entry.blocks.compactMap { block -> String? in
            if case .text(_, let text) = block { return text }
            return nil
        }.joined(separator: "\n")
        VStack(alignment: .trailing, spacing: Space.xs) {
            ForEach(entry.blocks) { block in
                if case .image(_, let image) = block {
                    TranscriptImageView(image: image, height: 180, label: "Attached image")
                        .frame(width: 180, height: 180)
                }
            }
            if !text.isEmpty {
                Text(verbatim: text)
                    .font(.sans(FontSize.base * scale))
                    .foregroundStyle(palette.foreground)
                    .lineSpacing(22 * scale - FontSize.base * scale * 1.2)
                    .textSelection(.enabled)
                    .padding(.horizontal, Space.lg)
                    .padding(.vertical, 10)
                    .background(palette.secondary, in: RoundedRectangle(cornerRadius: 18))
                    .containerRelativeFrame(.horizontal, alignment: .trailing) { width, _ in width * 0.85 }
            }
        }
        .frame(maxWidth: .infinity, alignment: .trailing)
        .padding(.vertical, Space.sm)
        .accessibilityElement(children: .contain)
        .accessibilityLabel("You")
    }
}

/// One block of an assistant turn, with how the turn ended under its last.
struct AssistantBlockView: View, Equatable {
    let block: TranscriptBlock
    let streaming: Bool
    let first: Bool
    let last: Bool
    let isError: Bool
    let turnEnd: TurnEnd?
    @Environment(\.palette) private var palette

    nonisolated static func == (a: Self, b: Self) -> Bool {
        a.block == b.block && a.streaming == b.streaming && a.first == b.first && a.last == b.last
            && a.isError == b.isError && a.turnEnd == b.turnEnd
    }

    var body: some View {
        VStack(alignment: .leading, spacing: Space.sm) {
            content
            if last && isError {
                ThemedText("The turn ended with an error.", size: FontSize.sm, tone: .destructive)
            }
            if last, let turnEnd {
                ThemedText(formatTurnEnd(turnEnd), size: FontSize.xs, tone: .muted, scaled: false)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.top, first ? Space.sm : 0)
        .padding(.bottom, Space.sm)
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Assistant")
    }

    @ViewBuilder
    private var content: some View {
        switch block {
        case .text(_, let text):
            ReplyView(text: text, streaming: streaming && last)
        case .image(_, let image):
            TranscriptImageView(image: image)
        case .thinking(_, let text, let durationMs):
            ThinkingView(text: text, durationMs: durationMs, streaming: streaming)
        case .tools(_, let calls):
            ToolClusterView(calls: calls)
        case .subagent(_, let call):
            SubagentCardView(call: call)
        }
    }
}

/// A Task call: the subagent's kind and description, how its run is going,
/// and a way into its Session.
struct SubagentCardView: View {
    let call: ToolCall
    @State private var open = false
    @Environment(\.palette) private var palette
    @Environment(\.transcriptLinks) private var links

    var body: some View {
        let request = taskRequest(call)
        let sessionId = links.subagentSession(call)
        let run = sessionId.flatMap(links.subagentRun)
        let state = taskState(call, run: run)
        VStack(alignment: .leading, spacing: Space.sm) {
            HStack(spacing: Space.sm) {
                statusIcon(state)
                VStack(alignment: .leading, spacing: 2) {
                    ThemedText(request.subagentType.isEmpty ? "Subagent" : request.subagentType, size: FontSize.xs, tone: .muted, scaled: false)
                    ThemedText(request.description.isEmpty ? "Task" : request.description, size: FontSize.sm, weight: .medium)
                        .lineLimit(2)
                }
                Spacer(minLength: 0)
                if let sessionId {
                    Button("Open") { links.open(sessionId) }
                        .font(.sans(FontSize.sm, .medium))
                        .buttonStyle(.plain)
                        .foregroundStyle(palette.foreground)
                        .padding(.horizontal, Space.md).frame(height: 30)
                        .background(palette.secondary, in: Capsule())
                        .accessibilityLabel("Open \(request.description.isEmpty ? "subagent" : request.description)")
                }
            }
            if let run, let count = run.toolUseCount {
                ThemedText(
                    "\(count) tool \(count == 1 ? "call" : "calls")" + (run.durationMs.map { " · " + formatRunDuration($0) } ?? ""),
                    size: FontSize.xs, tone: .muted, scaled: false)
            }
            let report = taskReport(call)
            if !report.isEmpty {
                Button {
                    withAnimation(.snappy(duration: 0.2)) { open.toggle() }
                } label: {
                    HStack(spacing: Space.xs) {
                        ThemedText("Report", size: FontSize.xs, tone: .muted, scaled: false)
                        Image(systemName: "chevron.right").font(.system(size: 9, weight: .semibold))
                            .foregroundStyle(palette.mutedForeground)
                            .rotationEffect(.degrees(open ? 90 : 0))
                    }
                }
                .buttonStyle(.plain)
                .accessibilityValue(open ? "Expanded" : "Collapsed")
                if open {
                    MarkdownView(blocks: MarkdownCache.blocks(for: report, streaming: false), muted: true)
                }
            }
        }
        .padding(Space.md)
        .background(palette.card, in: RoundedRectangle(cornerRadius: Radius.lg))
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Subagent")
    }

    @ViewBuilder
    private func statusIcon(_ state: SubagentStatus) -> some View {
        switch state {
        case .running, .pending:
            Spinner(size: 13, color: palette.working).accessibilityLabel("Running")
        case .failed:
            Image(systemName: "xmark.circle").foregroundStyle(palette.destructiveForeground).accessibilityLabel("Failed")
        case .cancelled:
            Image(systemName: "stop.circle").foregroundStyle(palette.mutedForeground).accessibilityLabel("Cancelled")
        default:
            Image(systemName: "checkmark.circle").foregroundStyle(palette.success).accessibilityLabel("Done")
        }
    }
}
