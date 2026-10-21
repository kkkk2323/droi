import DroiKit
import SwiftUI

/// A Script run: what it calls, how it went, and when opened its source,
/// inputs and output; the calls it made are listed under it.
struct ScriptGroupView: View {
    let call: ToolCall
    @State private var open = false
    @Environment(\.palette) private var palette

    var body: some View {
        let run = readScriptRun(call)
        VStack(alignment: .leading, spacing: Space.xs) {
            Button {
                withAnimation(.snappy(duration: 0.2)) { open.toggle() }
            } label: {
                HStack(spacing: Space.sm) {
                    Image(systemName: "curlybraces").font(.system(size: 12)).foregroundStyle(palette.mutedForeground)
                        .frame(width: 16)
                    Text(call.use.name == waitForScriptTool ? "WaitForScript" : "Script")
                        .font(.sans(FontSize.sm, .medium)).foregroundStyle(palette.foreground)
                    Text(scriptSummary(call)).font(.mono(FontSize.xs)).foregroundStyle(palette.mutedForeground)
                        .lineLimit(1)
                    Spacer(minLength: 0)
                    RunStatusView(pending: call.result == nil, status: run.status, failed: run.error != nil)
                }
                .frame(minHeight: 26)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityValue(open ? "Expanded" : "Collapsed")
            if open {
                ScriptDetailView(call: call, run: run).padding(.leading, 24)
            }
            if let nested = call.nested, !nested.isEmpty {
                VStack(alignment: .leading, spacing: 2) {
                    ForEach(nested, id: \.use.id) { ToolRowView(call: $0) }
                }
                .padding(.leading, Space.md)
                .overlay(alignment: .leading) {
                    Rectangle().fill(palette.border).frame(width: 1)
                        .mask(VStack(spacing: 3) { ForEach(0..<200, id: \.self) { _ in Rectangle().frame(height: 3) } })
                }
                .padding(.leading, 7)
                .accessibilityElement(children: .contain)
                .accessibilityLabel("Calls made by Script")
            }
        }
    }
}

private struct RunStatusView: View {
    let pending: Bool
    let status: ScriptStatus?
    let failed: Bool
    @Environment(\.palette) private var palette

    var body: some View {
        if pending {
            Spinner(size: 11).accessibilityLabel("Running")
        } else {
            switch status {
            case .running?:
                ThemedText("Still running", size: FontSize.xs, tone: .muted, scaled: false)
            case .stalled?:
                ThemedText("Stalled", size: FontSize.xs, tone: .attention, scaled: false)
            case .cancelled?:
                ThemedText("Cancelled", size: FontSize.xs, tone: .muted, scaled: false)
            case .failed?:
                failedIcon
            default:
                if failed { failedIcon } else {
                    Image(systemName: "checkmark").font(.system(size: 11, weight: .medium))
                        .foregroundStyle(palette.mutedForeground).accessibilityLabel("Succeeded")
                }
            }
        }
    }

    private var failedIcon: some View {
        Image(systemName: "xmark.circle").font(.system(size: 12)).foregroundStyle(palette.destructiveForeground)
            .accessibilityLabel("Failed")
    }
}

private struct ScriptDetailView: View {
    let call: ToolCall
    let run: ScriptRun
    @Environment(\.palette) private var palette

    var body: some View {
        VStack(alignment: .leading, spacing: Space.sm) {
            if let source = scriptSource(call) {
                section("Source") { SourceView(script: source.script) }
                ForEach(source.inputs, id: \.name) { input in
                    section("inputs.\(input.name)") { CodeText(input.text) }
                }
            }
            let output = [run.output, run.value ?? ""].filter { !$0.isEmpty }.joined(separator: "\n")
            if !output.isEmpty || run.error != nil || !run.images.isEmpty {
                section("Output") {
                    VStack(alignment: .leading, spacing: Space.sm) {
                        if !output.isEmpty { CodeText(truncateLines(output, max: resultPreviewLines)) }
                        if let error = run.error { CodeText(error, error: true) }
                        ForEach(Array(run.images.enumerated()), id: \.offset) { _, image in
                            TranscriptImageView(image: image, height: 220)
                        }
                    }
                }
            }
            if run.stats != nil || run.logPath != nil {
                HStack(spacing: Space.sm) {
                    if let stats = run.stats { ThemedText(stats, size: FontSize.xs, tone: .muted, scaled: false) }
                    if let log = run.logPath {
                        Text(log).font(.mono(FontSize.xs)).foregroundStyle(palette.mutedForeground)
                            .lineLimit(1).truncationMode(.head)
                    }
                }
            }
        }
    }

    private func section<Content: View>(_ title: String, @ViewBuilder content: () -> Content) -> some View {
        VStack(alignment: .leading, spacing: Space.xs) {
            ThemedText(title, size: FontSize.xs, tone: .muted, mono: title.hasPrefix("inputs."), scaled: false)
            content()
        }
    }
}

/// A Script's source with line numbers; asked-about lines are tinted.
struct SourceView: View {
    let script: String
    var highlight: Set<Int> = []
    @Environment(\.palette) private var palette

    var body: some View {
        let lines = script.split(separator: "\n", omittingEmptySubsequences: false)
        let width = String(lines.count).count
        ScrollView(.horizontal, showsIndicators: false) {
            VStack(alignment: .leading, spacing: 0) {
                ForEach(Array(lines.enumerated()), id: \.offset) { index, line in
                    let number = index + 1
                    let marked = highlight.contains(number)
                    HStack(spacing: Space.sm) {
                        Text(String(repeating: " ", count: width - String(number).count) + String(number))
                            .foregroundStyle(marked ? palette.attention : palette.mutedForeground)
                        Text(verbatim: String(line)).foregroundStyle(palette.foreground)
                    }
                    .font(.mono(FontSize.xs))
                    .padding(.horizontal, Space.sm)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background(marked ? palette.attention.opacity(0.12) : .clear)
                    .accessibilityElement(children: .ignore)
                    .accessibilityLabel(marked ? "Line \(number), asked about: \(line)" : "Line \(number): \(line)")
                }
            }
            .padding(.vertical, Space.xs)
            .fixedSize(horizontal: true, vertical: false)
        }
        .background(palette.card, in: RoundedRectangle(cornerRadius: Radius.md))
        .textSelection(.enabled)
    }
}
