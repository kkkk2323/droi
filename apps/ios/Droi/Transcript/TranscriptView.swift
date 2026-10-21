import DroiKit
import DroidProtocol
import SwiftUI

/// The transcript: opens on the latest message, follows streamed output while
/// the reader is at the bottom, stays put once they scroll up, and offers the
/// way back down. A turn is a row per block, so the block being read keeps
/// its place while a later one grows.
struct TranscriptView<Lead: View>: View {
    let entries: [TranscriptEntry]
    /// Where each continued Session starts, below the one it continues.
    var boundaryIds: Set<String> = []
    let workingState: DroidWorkingState
    /// Bumped on send: the list goes to the end wherever it was.
    var scrollToEndKey = 0
    @ViewBuilder var lead: () -> Lead

    @State private var position = ScrollPosition(edge: .bottom)
    @State private var atLatest = true
    @Environment(\.palette) private var palette

    /// How far above the bottom, in points, still counts as reading the latest output.
    private let followThreshold: CGFloat = 120

    var body: some View {
        let running = workingState != .idle
        let streamingId = streamingEntryId(entries, workingState: workingState)
        let ends = turnEnds(entries, running: running)
        let rows = Array(transcriptRows(entries).reversed())
        let activity = workingLabel(workingState)
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 0) {
                lead().frame(maxWidth: .infinity, alignment: .leading).padding(.top, Space.md)
                ForEach(rows) { row in
                    VStack(spacing: 0) {
                        if row.first && boundaryIds.contains(row.entry.id) { BoundaryView() }
                        if let block = row.block {
                            AssistantBlockView(
                                block: block, streaming: row.entry.id == streamingId, first: row.first,
                                last: row.last, isError: row.last && row.entry.isError,
                                turnEnd: row.last ? ends[row.entry.id] : nil
                            ).equatable()
                        } else {
                            UserMessageView(entry: row.entry).equatable()
                        }
                    }
                    .id(row.id)
                }
                HStack(spacing: Space.sm) {
                    if !activity.isEmpty {
                        BouncingDots(color: palette.mutedForeground)
                        ThemedText(activity, size: FontSize.sm, tone: .muted)
                    }
                }
                .frame(minHeight: 28, alignment: .leading)
                .padding(.bottom, Space.lg)
                .accessibilityElement(children: .combine)
                .accessibilityLabel("Session activity")
                .accessibilityValue(activity)
                .id("activity")
            }
            .padding(.horizontal, Space.lg)
            .scrollTargetLayout()
        }
        .scrollPosition($position)
        .defaultScrollAnchor(.bottom)
        .scrollDismissesKeyboard(.interactively)
        .onScrollGeometryChange(for: Bool.self) { geometry in
            geometry.contentOffset.y + geometry.containerSize.height >= geometry.contentSize.height - followThreshold
        } action: { _, near in
            atLatest = near
        }
        .onScrollGeometryChange(for: CGFloat.self) { geometry in
            geometry.contentSize.height
        } action: { old, new in
            // Output that grows below a reader at the bottom is followed; a
            // reader who scrolled up is left where they are.
            if new > old, atLatest { position.scrollTo(edge: .bottom) }
        }
        .onChange(of: scrollToEndKey) {
            withAnimation(.snappy(duration: 0.25)) { position.scrollTo(edge: .bottom) }
        }
        .overlay(alignment: .bottom) {
            if !atLatest {
                Button {
                    withAnimation(.snappy(duration: 0.25)) { position.scrollTo(edge: .bottom) }
                } label: {
                    Image(systemName: "arrow.down")
                        .font(.system(size: 15, weight: .medium))
                        .foregroundStyle(palette.foreground)
                        .frame(width: 36, height: 36)
                        .background(palette.background, in: Circle())
                        .overlay(Circle().strokeBorder(palette.border, lineWidth: 0.5))
                }
                .buttonStyle(.plain)
                .padding(.bottom, Space.md)
                .accessibilityLabel("Scroll to latest")
                .transition(.opacity)
            }
        }
        .accessibilityLabel("Transcript")
    }
}

private struct BoundaryView: View {
    @Environment(\.palette) private var palette

    var body: some View {
        HStack(spacing: Space.sm) {
            Rectangle().fill(palette.border).frame(height: 0.5)
            ThemedText(
                "Context compacted here; the conversation continues in a new session", size: FontSize.xs,
                tone: .muted, scaled: false
            )
            .multilineTextAlignment(.center)
            .fixedSize(horizontal: false, vertical: true)
            Rectangle().fill(palette.border).frame(height: 0.5)
        }
        .padding(.vertical, Space.md)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("Context compacted here")
    }
}
