import SwiftUI

/// Grouped rows in the iOS settings style, in Droi's colours.
struct ListSection<Content: View>: View {
    var title: String?
    var footer: String?
    @ViewBuilder var content: () -> Content
    @Environment(\.palette) private var palette

    var body: some View {
        VStack(alignment: .leading, spacing: Space.xs) {
            if let title {
                Text(title.uppercased())
                    .font(.sans(FontSize.xs, .medium))
                    .foregroundStyle(palette.mutedForeground)
                    .padding(.horizontal, Space.lg)
            }
            VStack(spacing: 0) {
                Group(subviews: content()) { rows in
                    ForEach(rows) { row in
                        row
                        if row.id != rows.last?.id {
                            Rectangle().fill(palette.border).frame(height: 0.5).padding(.leading, Space.lg)
                        }
                    }
                }
            }
            .background(palette.card, in: RoundedRectangle(cornerRadius: Radius.xl))
            if let footer {
                Text(footer)
                    .font(.sans(FontSize.xs))
                    .foregroundStyle(palette.mutedForeground)
                    .padding(.horizontal, Space.lg)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
        .accessibilityElement(children: .contain)
    }
}

/// A row with a label, an optional value, and a chevron when it navigates.
struct ListRow: View {
    let label: String
    var value: String?
    var chevron = true
    var destructive = false
    var action: () -> Void
    @Environment(\.palette) private var palette

    var body: some View {
        Button(action: action) {
            HStack(spacing: Space.sm) {
                Text(label)
                    .font(.sans(FontSize.base))
                    .foregroundStyle(destructive ? palette.destructiveForeground : palette.foreground)
                Spacer(minLength: Space.sm)
                if let value {
                    Text(value).font(.sans(FontSize.base)).foregroundStyle(palette.mutedForeground).lineLimit(1)
                }
                if chevron {
                    Image(systemName: "chevron.right").font(.system(size: 13, weight: .semibold))
                        .foregroundStyle(palette.mutedForeground.opacity(0.7))
                }
            }
            .padding(.horizontal, Space.lg)
            .frame(minHeight: 46)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }
}

/// A row with a switch.
struct ListSwitch: View {
    let label: String
    @Binding var isOn: Bool
    @Environment(\.palette) private var palette

    var body: some View {
        Toggle(isOn: $isOn) {
            Text(label).font(.sans(FontSize.base)).foregroundStyle(palette.foreground)
        }
        .tint(palette.success)
        .padding(.horizontal, Space.lg)
        .frame(minHeight: 46)
    }
}

/// A row that shows a value and offers the others in a menu.
struct ListPicker<Value: Hashable>: View {
    let label: String
    let choices: [(value: Value, label: String)]
    let selection: Value
    /// Nil makes the row display-only (an organisation manages it).
    var onChange: ((Value) -> Void)?
    @Environment(\.palette) private var palette

    var body: some View {
        let current = choices.first { $0.value == selection }?.label ?? ""
        HStack(spacing: Space.sm) {
            Text(label).font(.sans(FontSize.base)).foregroundStyle(palette.foreground)
            Spacer(minLength: Space.sm)
            if let onChange {
                Menu {
                    Picker(label, selection: Binding(get: { selection }, set: { onChange($0) })) {
                        ForEach(choices, id: \.value) { choice in Text(choice.label).tag(choice.value) }
                    }
                } label: {
                    HStack(spacing: 4) {
                        Text(current).font(.sans(FontSize.base)).lineLimit(1)
                        Image(systemName: "chevron.up.chevron.down").font(.system(size: 11, weight: .medium))
                    }
                    .foregroundStyle(palette.mutedForeground)
                }
                .accessibilityLabel(label)
                .accessibilityValue(current)
            } else {
                Text(current).font(.sans(FontSize.base)).foregroundStyle(palette.mutedForeground)
            }
        }
        .padding(.horizontal, Space.lg)
        .frame(minHeight: 46)
    }
}

/// Choices shown as rows with a checkmark on the chosen one.
struct ListChoices<Value: Hashable>: View {
    let title: String
    let choices: [(value: Value, label: String)]
    @Binding var selection: Value
    @Environment(\.palette) private var palette

    var body: some View {
        ListSection(title: title) {
            ForEach(choices, id: \.value) { choice in
                Button {
                    selection = choice.value
                } label: {
                    HStack {
                        Text(choice.label).font(.sans(FontSize.base)).foregroundStyle(palette.foreground)
                        Spacer()
                        if choice.value == selection {
                            Image(systemName: "checkmark").font(.system(size: 14, weight: .semibold))
                                .foregroundStyle(palette.foreground)
                        }
                    }
                    .padding(.horizontal, Space.lg)
                    .frame(minHeight: 46)
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityAddTraits(choice.value == selection ? [.isSelected] : [])
            }
        }
    }
}

/// A full-width button.
struct DroiButton: View {
    enum Variant { case primary, secondary, ghost, destructive }

    let title: String
    var variant: Variant = .primary
    var busy = false
    var disabled = false
    var action: () -> Void
    @Environment(\.palette) private var palette

    var body: some View {
        Button(action: action) {
            HStack(spacing: Space.sm) {
                if busy { Spinner(size: 14, color: foreground) }
                Text(title).font(.sans(FontSize.base, .medium))
            }
            .foregroundStyle(foreground)
            .frame(maxWidth: .infinity, minHeight: 44)
            .background(background, in: RoundedRectangle(cornerRadius: Radius.lg))
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .disabled(disabled || busy)
        .opacity(disabled ? 0.45 : 1)
    }

    private var foreground: Color {
        switch variant {
        case .primary: palette.primaryForeground
        case .secondary, .ghost: palette.foreground
        case .destructive: .white
        }
    }

    private var background: Color {
        switch variant {
        case .primary: palette.primary
        case .secondary: palette.secondary
        case .ghost: .clear
        case .destructive: palette.destructive
        }
    }
}

/// A text field in the theme: rounded, filled.
struct DroiTextField: View {
    let label: String
    @Binding var text: String
    var placeholder = ""
    var mono = false
    var keyboard: UIKeyboardType = .default
    var onSubmit: () -> Void = {}
    @Environment(\.palette) private var palette

    var body: some View {
        TextField(placeholder, text: $text)
            .font(mono ? .mono(FontSize.sm) : .sans(FontSize.base))
            .foregroundStyle(palette.foreground)
            .textInputAutocapitalization(.never)
            .autocorrectionDisabled()
            .keyboardType(keyboard)
            .submitLabel(.go)
            .onSubmit(onSubmit)
            .padding(.horizontal, Space.md)
            .frame(height: 44)
            .background(palette.card, in: RoundedRectangle(cornerRadius: Radius.lg))
            .accessibilityLabel(label)
    }
}
