import DroiKit
import DroidProtocol
import SwiftUI

/// A run of tool calls: a header that folds, and a row per call. Starts open.
struct ToolClusterView: View {
    let calls: [ToolCall]
    @State private var open = true
    @Environment(\.palette) private var palette

    var body: some View {
        let (label, pending) = clusterLabel(calls)
        VStack(alignment: .leading, spacing: Space.xs) {
            Button {
                withAnimation(.snappy(duration: 0.2)) { open.toggle() }
            } label: {
                HStack(spacing: Space.sm) {
                    if pending { Spinner(size: 11) }
                    ThemedText(label, size: FontSize.sm, tone: .muted)
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
                VStack(alignment: .leading, spacing: 2) {
                    ForEach(calls, id: \.use.id) { call in
                        if scriptRunOf(call.use) != nil || call.nested != nil {
                            ScriptGroupView(call: call)
                        } else {
                            ToolRowView(call: call)
                        }
                    }
                }
            }
        }
    }
}

/// The icon for a tool, by name.
func toolIcon(_ name: String) -> String {
    switch toolDisplayName(name).tool {
    case "Execute": "terminal"
    case "Read": "doc.text"
    case "Edit", "ApplyPatch", "MultiEdit": "square.and.pencil"
    case "Create": "doc.badge.plus"
    case "Grep": "magnifyingglass"
    case "Glob", "LS": "folder"
    case "FetchUrl", "WebSearch": "globe"
    case "Skill": "book"
    case "Script": "curlybraces"
    default: toolDisplayName(name).server != nil ? "powerplug" : "wrench.adjustable"
    }
}

/// One tool call: icon, name, what it was given, and how it went; tapping
/// shows the input and the result.
struct ToolRowView: View {
    let call: ToolCall
    @State private var open = false
    @Environment(\.palette) private var palette

    var body: some View {
        let name = toolDisplayName(call.use.name)
        let reading = readToolResult(call)
        let diff = reading.diff ?? (reading.isError ? nil : createdFileDiff(call))
        let parts = toolSummaryParts(call)
        VStack(alignment: .leading, spacing: Space.xs) {
            Button {
                withAnimation(.snappy(duration: 0.2)) { open.toggle() }
            } label: {
                HStack(alignment: .firstTextBaseline, spacing: Space.sm) {
                    Image(systemName: toolIcon(call.use.name))
                        .font(.system(size: 12))
                        .foregroundStyle(palette.mutedForeground)
                        .frame(width: 16)
                    summary(name: name, parts: parts, isError: reading.isError)
                        .lineLimit(1)
                        .truncationMode(.tail)
                    Spacer(minLength: 0)
                    if let diff {
                        Text("+\(diff.added)").font(.mono(FontSize.xs)).foregroundStyle(palette.success)
                        Text("−\(diff.removed)").font(.mono(FontSize.xs)).foregroundStyle(palette.destructiveForeground)
                    }
                    status(reading)
                }
                .frame(minHeight: 26)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityLabel("\(name.tool): \(toolSummary(call))")
            .accessibilityValue(open ? "Expanded" : "Collapsed")
            if open {
                ToolDetailView(call: call, reading: reading, diff: diff)
                    .padding(.leading, 24)
                    .accessibilityElement(children: .contain)
                    .accessibilityLabel("\(name.tool) details")
            }
        }
    }

    private func summary(name: ToolName, parts: [SummaryPart], isError: Bool) -> Text {
        var text = AttributedString()
        if let server = name.server {
            var s = AttributedString(server + " ")
            s.font = .sans(FontSize.sm)
            s.foregroundColor = palette.mutedForeground
            text.append(s)
        }
        var tool = AttributedString(name.tool)
        tool.font = .sans(FontSize.sm, .medium)
        tool.foregroundColor = isError ? palette.destructiveForeground : palette.foreground
        text.append(tool)
        for (index, part) in parts.enumerated() {
            var sep = AttributedString(index == 0 ? "  " : " · ")
            sep.foregroundColor = palette.mutedForeground
            sep.font = .sans(FontSize.sm)
            text.append(sep)
            if let key = part.key {
                var k = AttributedString(key + ": ")
                k.font = .sans(FontSize.sm)
                k.foregroundColor = palette.mutedForeground
                text.append(k)
            }
            var v = AttributedString(part.value)
            v.font = .mono(FontSize.xs)
            v.foregroundColor = palette.mutedForeground
            text.append(v)
        }
        return Text(text)
    }

    @ViewBuilder
    private func status(_ reading: ToolResultReading) -> some View {
        if reading.pending {
            Spinner(size: 11).accessibilityLabel("Running")
        } else if reading.isError {
            Image(systemName: "xmark.circle").font(.system(size: 12)).foregroundStyle(palette.destructiveForeground)
                .accessibilityLabel("Failed")
        } else {
            Image(systemName: "checkmark").font(.system(size: 11, weight: .medium)).foregroundStyle(palette.mutedForeground)
                .accessibilityLabel("Succeeded")
        }
    }
}

/// A tool call's input, then its diff, status line or (truncated) output.
struct ToolDetailView: View {
    let call: ToolCall
    let reading: ToolResultReading
    let diff: DiffResult?
    @Environment(\.palette) private var palette

    var body: some View {
        VStack(alignment: .leading, spacing: Space.sm) {
            CodeText(toolInputText(call))
            if let diff {
                DiffView(diff: diff)
            } else if let status = reading.status {
                HStack(spacing: Space.xs) {
                    Image(systemName: status.success ? "checkmark.circle" : "xmark.circle")
                    Text(status.message ?? (status.success ? "Succeeded" : "Failed"))
                }
                .font(.sans(FontSize.sm))
                .foregroundStyle(status.success ? palette.success : palette.destructiveForeground)
            } else if !reading.text.isEmpty {
                CodeText(truncateLines(reading.text, max: resultPreviewLines), error: reading.isError)
            }
            ForEach(Array(reading.images.enumerated()), id: \.offset) { _, image in
                TranscriptImageView(image: image, height: 220)
            }
        }
    }
}

/// Monospaced, selectable, scrolling sideways.
struct CodeText: View {
    let text: String
    var error = false
    @Environment(\.palette) private var palette

    init(_ text: String, error: Bool = false) {
        self.text = text
        self.error = error
    }

    var body: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            Text(verbatim: text)
                .font(.mono(FontSize.xs))
                .foregroundStyle(error ? palette.destructiveForeground : palette.foreground)
                .lineSpacing(3)
                .textSelection(.enabled)
                .fixedSize(horizontal: true, vertical: true)
                .padding(Space.sm)
        }
        .background(palette.card, in: RoundedRectangle(cornerRadius: Radius.md))
    }
}

/// An edit's lines with old and new numbers, added and removed tinted.
struct DiffView: View {
    let diff: DiffResult
    @Environment(\.palette) private var palette

    var body: some View {
        let oldWidth = String(diff.lines.compactMap(\.old).max() ?? 0).count
        let newWidth = String(diff.lines.compactMap(\.new).max() ?? 0).count
        ScrollView(.horizontal, showsIndicators: false) {
            VStack(alignment: .leading, spacing: 0) {
                ForEach(Array(diff.lines.prefix(400).enumerated()), id: \.offset) { _, line in
                    HStack(spacing: Space.sm) {
                        Text(pad(line.old, oldWidth) + " " + pad(line.new, newWidth))
                            .foregroundStyle(palette.mutedForeground)
                        Text(sign(line.type)).foregroundStyle(palette.mutedForeground)
                        Text(verbatim: line.content).foregroundStyle(palette.foreground)
                    }
                    .font(.mono(FontSize.xs))
                    .padding(.horizontal, Space.sm)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background(background(line.type))
                    .accessibilityElement(children: .ignore)
                    .accessibilityLabel("\(label(line.type)): \(line.content)")
                }
            }
            .padding(.vertical, Space.xs)
            .fixedSize(horizontal: true, vertical: false)
        }
        .background(palette.card, in: RoundedRectangle(cornerRadius: Radius.md))
    }

    private func pad(_ n: Int?, _ width: Int) -> String {
        let s = n.map(String.init) ?? ""
        return String(repeating: " ", count: max(0, width - s.count)) + s
    }

    private func sign(_ kind: DiffLine.Kind) -> String {
        switch kind {
        case .added: "+"
        case .removed: "−"
        default: " "
        }
    }

    private func label(_ kind: DiffLine.Kind) -> String {
        switch kind {
        case .added: "Added"
        case .removed: "Removed"
        default: "Unchanged"
        }
    }

    private func background(_ kind: DiffLine.Kind) -> Color {
        switch kind {
        case .added: palette.addedBackground
        case .removed: palette.removedBackground
        default: .clear
        }
    }
}

/// An image from the transcript, decoded once and kept while memory allows.
struct TranscriptImageView: View {
    let image: TranscriptImage
    var height: CGFloat = 240
    var label = "Image from Droid"
    @Environment(\.palette) private var palette

    var body: some View {
        let key = image.mediaType + String(image.base64.count) + String(image.base64.prefix(48)) + String(image.base64.suffix(48))
        if let ui = ImageCache.image(key: key, data: { image.data }) {
            Image(uiImage: ui)
                .resizable()
                .scaledToFit()
                .frame(maxWidth: .infinity, maxHeight: height)
                .clipShape(RoundedRectangle(cornerRadius: Radius.xl))
                .hairlineBorder(palette.border, radius: Radius.xl)
                .accessibilityLabel(label)
        }
    }
}
