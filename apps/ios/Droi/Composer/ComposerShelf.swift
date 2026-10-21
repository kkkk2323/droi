import Droid
import DroiKit
import DroidProtocol
import SwiftUI

/// What sits on top of the composer: the Session's todo list while it has
/// work left, and the messages waiting their turn.
struct ComposerShelf: View {
    let todos: [TodoItem]
    let queued: [QueuedMessage]
    var onRemove: (QueuedMessage) -> Void
    var onTake: (QueuedMessage) -> Void
    @State private var todosOpen = false
    @State private var queuedOpen = false
    @Environment(\.palette) private var palette

    var body: some View {
        let done = todos.filter { $0.status == .completed }.count
        let showTodos = !todos.isEmpty && done < todos.count
        if showTodos || !queued.isEmpty {
            VStack(alignment: .leading, spacing: 0) {
                if showTodos { todoList(done: done) }
                if showTodos && !queued.isEmpty { Rectangle().fill(palette.border).frame(height: 0.5) }
                if !queued.isEmpty { queuedList }
            }
            .background(palette.card, in: UnevenRoundedRectangle(topLeadingRadius: Radius.xl, topTrailingRadius: Radius.xl))
            .padding(.horizontal, Space.md + ComposerView<EmptyView>.radius)
        }
    }

    private func todoList(done: Int) -> some View {
        let current = todos.first { $0.status == .inProgress } ?? todos.first { $0.status == .pending }
        return VStack(alignment: .leading, spacing: 0) {
            Button {
                withAnimation(.snappy(duration: 0.2)) { todosOpen.toggle() }
            } label: {
                HStack(spacing: Space.sm) {
                    Image(systemName: "checklist").font(.system(size: 12))
                    Text(current?.content ?? "Tasks").font(.sans(FontSize.xs)).lineLimit(1)
                    Spacer(minLength: 0)
                    Text("\(done)/\(todos.count)").font(.mono(FontSize.xs))
                    Image(systemName: "chevron.up").font(.system(size: 9, weight: .semibold))
                        .rotationEffect(.degrees(todosOpen ? 180 : 0))
                }
                .foregroundStyle(palette.mutedForeground)
                .padding(.horizontal, Space.md)
                .frame(minHeight: 32)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityLabel("Tasks, \(done) of \(todos.count) done")
            .accessibilityValue(todosOpen ? "Expanded" : "Collapsed")
            if todosOpen {
                VStack(alignment: .leading, spacing: 4) {
                    ForEach(Array(todos.enumerated()), id: \.offset) { _, todo in
                        HStack(alignment: .firstTextBaseline, spacing: Space.sm) {
                            switch todo.status {
                            case .completed: Image(systemName: "checkmark.circle").foregroundStyle(palette.success)
                            case .inProgress: Spinner(size: 11, color: palette.working)
                            default: Image(systemName: "circle").foregroundStyle(palette.mutedForeground)
                            }
                            Text(todo.content).font(.sans(FontSize.xs)).foregroundStyle(palette.foreground)
                                .strikethrough(todo.status == .completed, color: palette.mutedForeground)
                        }
                        .font(.system(size: 11))
                        .accessibilityElement(children: .ignore)
                        .accessibilityLabel(
                            "\(todo.status == .completed ? "Done" : todo.status == .inProgress ? "In progress" : "Pending"): \(todo.content)")
                    }
                }
                .padding(.horizontal, Space.md)
                .padding(.bottom, Space.sm)
            }
        }
    }

    private var queuedList: some View {
        let folded = queued.count > queuedUnfolded && !queuedOpen
        return VStack(alignment: .leading, spacing: 0) {
            if queued.count > queuedUnfolded {
                Button {
                    withAnimation(.snappy(duration: 0.2)) { queuedOpen.toggle() }
                } label: {
                    HStack(spacing: Space.sm) {
                        Image(systemName: "clock").font(.system(size: 11))
                        Text(folded ? queuedText((queued.first { isSteering($0) } ?? queued[0]).content) : "Queued messages")
                            .font(.sans(FontSize.xs)).lineLimit(1)
                        Spacer(minLength: 0)
                        Text("\(queued.count)").font(.mono(FontSize.xs))
                        Image(systemName: "chevron.up").font(.system(size: 9, weight: .semibold))
                            .rotationEffect(.degrees(queuedOpen ? 180 : 0))
                    }
                    .foregroundStyle(palette.mutedForeground)
                    .padding(.horizontal, Space.md)
                    .frame(minHeight: 32)
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityLabel("Queued messages, \(queued.count)")
                .accessibilityValue(queuedOpen ? "Expanded" : "Collapsed")
            }
            if !folded {
                ForEach(queued, id: \.requestID) { message in row(message) }
            }
        }
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Queued messages")
    }

    private func isSteering(_ m: QueuedMessage) -> Bool {
        m.kind == .daemonQueuedDiscardable || m.kind == .localDeferredAfterEsc
    }

    private func row(_ message: QueuedMessage) -> some View {
        let paused = message.kind == .localPausedAfterEsc
        return HStack(spacing: Space.sm) {
            Image(systemName: paused ? "pause" : isSteering(message) ? "arrow.turn.down.left" : "clock")
                .font(.system(size: 11))
                .foregroundStyle(palette.mutedForeground)
            Text(queuedText(message.content)).font(.sans(FontSize.xs)).foregroundStyle(palette.foreground).lineLimit(1)
            Spacer(minLength: 0)
            Text(paused ? "Paused" : isSteering(message) ? "Next" : "Queued").font(.sans(FontSize.xs))
                .foregroundStyle(palette.mutedForeground)
            if paused {
                Button { onTake(message) } label: { Image(systemName: "pencil").font(.system(size: 11)) }
                    .buttonStyle(.plain).foregroundStyle(palette.mutedForeground)
                    .frame(width: 26, height: 26)
                    .accessibilityLabel("Edit queued message")
            }
            Button { onRemove(message) } label: { Image(systemName: "xmark").font(.system(size: 10, weight: .semibold)) }
                .buttonStyle(.plain).foregroundStyle(palette.mutedForeground)
                .frame(width: 26, height: 26)
                .accessibilityLabel("Remove queued message")
        }
        .padding(.horizontal, Space.md)
        .frame(minHeight: 32)
    }
}
