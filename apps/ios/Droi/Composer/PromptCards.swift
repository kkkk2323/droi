import DroiKit
import DroidProtocol
import SwiftUI

/// A permission request: which tools Droid wants to run, with what, and the
/// options the Daemon offers.
struct PermissionCard: View {
    let toolUses: [ToolConfirmationInfo]
    let options: [ToolConfirmationListItem]
    let onChoose: (ToolConfirmationOutcome) async throws -> Void
    @State private var busy: ToolConfirmationOutcome?
    @State private var error: String?
    @Environment(\.palette) private var palette

    var body: some View {
        let names = toolUses.map { toolDisplayName($0.toolUse.name).tool }.joined(separator: ", ")
        VStack(alignment: .leading, spacing: Space.md) {
            HStack(spacing: Space.xs) {
                Image(systemName: "exclamationmark.shield").font(.system(size: 13))
                ThemedText("Permission", size: FontSize.xs, weight: .medium, tone: .attention, scaled: false)
            }
            .foregroundStyle(palette.attention)
            ThemedText("Droid wants to run \(names)", size: FontSize.base, weight: .medium)
            ForEach(Array(toolUses.enumerated()), id: \.offset) { _, info in
                if let script = scriptPermission(info.details), let source = scriptSource(ToolCall(use: info.toolUse)) {
                    ScriptPermissionDetail(permission: script, script: source.script)
                } else {
                    Text(verbatim: permissionDetail(info.details, input: info.toolUse.input))
                        .font(.mono(FontSize.xs))
                        .foregroundStyle(palette.foreground)
                        .textSelection(.enabled)
                        .lineLimit(12)
                        .padding(Space.sm)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .background(palette.card, in: RoundedRectangle(cornerRadius: Radius.md))
                }
            }
            VStack(spacing: Space.xs) {
                ForEach(options, id: \.value) { option in
                    Button {
                        choose(option.value)
                    } label: {
                        HStack(spacing: Space.sm) {
                            if busy == option.value {
                                Spinner(size: 13)
                            } else {
                                Image(systemName: option.value == .cancel ? "xmark" : "checkmark")
                                    .font(.system(size: 12, weight: .medium))
                            }
                            Text(option.label).font(.sans(FontSize.sm, .medium)).multilineTextAlignment(.leading)
                            Spacer(minLength: 0)
                        }
                        .foregroundStyle(option.value == .cancel ? palette.mutedForeground : palette.foreground)
                        .padding(.horizontal, Space.md)
                        .frame(minHeight: 40)
                        .background(palette.secondary, in: RoundedRectangle(cornerRadius: Radius.lg))
                        .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .disabled(busy != nil)
                    .accessibilityLabel(option.label)
                }
            }
            if let error { ThemedText(error, size: FontSize.sm, tone: .destructive, scaled: false) }
        }
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Permission request: \(names)")
    }

    private func choose(_ outcome: ToolConfirmationOutcome) {
        busy = outcome
        error = nil
        Task {
            do {
                try await onChoose(outcome)
            } catch {
                // Another Client answered first: the prompt is gone, not failed.
                if !isAlreadyResolved(String(describing: error)) { self.error = String(describing: error) }
            }
            busy = nil
        }
    }
}

/// The calls a Script asks for at once, by line, with the source on demand.
private struct ScriptPermissionDetail: View {
    let permission: ScriptPermission
    let script: String
    @State private var showSource = false
    @Environment(\.palette) private var palette

    var body: some View {
        VStack(alignment: .leading, spacing: Space.xs) {
            ForEach(Array(permission.calls.enumerated()), id: \.offset) { _, call in
                HStack(alignment: .firstTextBaseline, spacing: Space.sm) {
                    Text("L\(call.line)").font(.mono(FontSize.xs)).foregroundStyle(palette.mutedForeground)
                    Text(call.tool).font(.sans(FontSize.xs, .medium)).foregroundStyle(palette.foreground)
                    Text(call.detail).font(.mono(FontSize.xs)).foregroundStyle(palette.foreground).lineLimit(2)
                    Spacer(minLength: 0)
                    if let impact = call.impact {
                        Text(impact).font(.sans(FontSize.xs)).foregroundStyle(
                            impact == "high" ? palette.destructiveForeground : impact == "medium" ? palette.attention : palette.mutedForeground)
                    }
                }
                .accessibilityElement(children: .ignore)
                .accessibilityLabel("Line \(call.line), \(call.tool): \(call.detail)" + (call.impact.map { ", \($0)" } ?? ""))
            }
            Button(showSource ? "Hide source" : "Show source") {
                withAnimation(.snappy(duration: 0.2)) { showSource.toggle() }
            }
            .font(.sans(FontSize.xs, .medium))
            .foregroundStyle(palette.mutedForeground)
            .buttonStyle(.plain)
            if showSource {
                SourceView(script: script, highlight: Set(permission.calls.map(\.line)))
            }
        }
        .padding(Space.sm)
        .background(palette.card, in: RoundedRectangle(cornerRadius: Radius.md))
    }
}

/// Droid's questions, one at a time, with its options and a field for an
/// answer of one's own.
struct AskUserCard: View {
    let questions: [AskUserQuestion]
    let onAnswer: ([AskUserAnswer]) async throws -> Void
    let onCancel: () async throws -> Void
    @State private var step = 0
    @State private var chosen: [Int: [String]] = [:]
    @State private var typed: [Int: String] = [:]
    @State private var busy = false
    @State private var error: String?
    @Environment(\.palette) private var palette

    var body: some View {
        let question = questions[min(step, questions.count - 1)]
        let picked = chosen[step] ?? []
        let own = typed[step] ?? ""
        let canGo = !picked.isEmpty || !own.trimmingCharacters(in: .whitespaces).isEmpty
        VStack(alignment: .leading, spacing: Space.md) {
            HStack(spacing: Space.xs) {
                Image(systemName: "questionmark.bubble").font(.system(size: 13))
                ThemedText(question.topic.isEmpty ? "Question" : question.topic, size: FontSize.xs, weight: .medium, tone: .working, scaled: false)
                if questions.count > 1 {
                    ThemedText("\(step + 1) / \(questions.count)", size: FontSize.xs, tone: .muted, scaled: false)
                        .padding(.horizontal, 6).background(palette.secondary, in: Capsule())
                }
            }
            .foregroundStyle(palette.working)
            ThemedText(question.question, size: FontSize.base, weight: .medium).fixedSize(horizontal: false, vertical: true)
            VStack(spacing: Space.xs) {
                ForEach(question.options, id: \.self) { option in
                    let on = picked.contains(option)
                    Button {
                        var next = question.multiSelect == true ? picked : []
                        if on { next.removeAll { $0 == option } } else { next.append(option) }
                        chosen[step] = next
                        typed[step] = ""
                    } label: {
                        HStack(spacing: Space.sm) {
                            Image(systemName: question.multiSelect == true
                                ? (on ? "checkmark.square.fill" : "square")
                                : (on ? "largecircle.fill.circle" : "circle"))
                                .font(.system(size: 15))
                            Text(option).font(.sans(FontSize.sm)).multilineTextAlignment(.leading)
                            Spacer(minLength: 0)
                        }
                        .foregroundStyle(palette.foreground)
                        .padding(.horizontal, Space.md)
                        .frame(minHeight: 40)
                        .background(on ? palette.accent : palette.secondary, in: RoundedRectangle(cornerRadius: Radius.lg))
                        .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel(option)
                    .accessibilityAddTraits(on ? .isSelected : [])
                }
            }
            DroiTextField(
                label: "Other answer for: \(question.question)",
                text: Binding(get: { own }, set: { typed[step] = $0; if !$0.isEmpty { chosen[step] = [] } }),
                placeholder: "Other answer")
            if let error { ThemedText(error, size: FontSize.sm, tone: .destructive, scaled: false) }
            HStack(spacing: Space.sm) {
                if step > 0 {
                    DroiButton(title: "Back", variant: .ghost) { step -= 1 }
                }
                DroiButton(title: "Cancel", variant: .secondary, disabled: busy) { run { try await onCancel() } }
                DroiButton(title: step + 1 < questions.count ? "Next" : "Answer", busy: busy, disabled: !canGo) {
                    if step + 1 < questions.count {
                        step += 1
                    } else {
                        let answers = questions.indices.map { i in
                            askUserAnswer(
                                index: Int(questions[i].index), question: questions[i].question,
                                chosen: chosen[i] ?? [], typed: typed[i] ?? "")
                        }
                        run { try await onAnswer(answers) }
                    }
                }
            }
        }
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Droid has a question")
    }

    private func run(_ body: @escaping () async throws -> Void) {
        busy = true
        error = nil
        Task {
            do { try await body() } catch {
                if !isAlreadyResolved(String(describing: error)) { self.error = String(describing: error) }
            }
            busy = false
        }
    }
}
