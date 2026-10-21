import DroiKit
import DroidProtocol
import SwiftUI

/// The settings pills under the composer: model, reasoning effort, tool calls
/// (a new Session only) and autonomy; each opens a sheet.
struct SessionSettingsBar: View {
    let models: [ModelChoice]
    let modelId: String?
    let reasoningEffort: String?
    let autonomyLevel: String?
    var toolMode: ToolMode?
    var onModel: (String) -> Void
    var onEffort: (String) -> Void
    var onAutonomy: (String) -> Void
    var onToolMode: ((ToolMode) -> Void)?

    @State private var sheet: Sheet?
    @Environment(\.palette) private var palette

    enum Sheet: String, Identifiable {
        case model, effort, autonomy, tools
        var id: String { rawValue }
    }

    var body: some View {
        let model = models.first { $0.id == modelId }
        HStack(spacing: Space.xs) {
            pill(model?.label ?? "Model", icon: "sparkles", label: "Model") { sheet = .model }
            if let model, !model.reasoningEfforts.isEmpty {
                pill(effortLabel(reasoningEffort ?? model.reasoningEfforts[0]), icon: nil, label: "Reasoning effort") {
                    sheet = .effort
                }
            }
            if let toolMode, onToolMode != nil {
                pill(toolMode.label, icon: "curlybraces", label: "Tool calls") { sheet = .tools }
            }
            pill(autonomyLabel(autonomyLevel ?? "off"), icon: "checkmark.shield", label: "Autonomy") { sheet = .autonomy }
        }
        .sheet(item: $sheet) { which in
            Group {
                switch which {
                case .model:
                    ModelPicker(models: models, selected: modelId) { id in
                        onModel(id)
                        sheet = nil
                    }
                case .effort:
                    RadioSheet(
                        title: "Reasoning effort",
                        choices: (model?.reasoningEfforts ?? []).map { ($0, effortLabel($0), nil) },
                        selected: reasoningEffort ?? model?.reasoningEfforts.first
                    ) { onEffort($0); sheet = nil }
                case .autonomy:
                    RadioSheet(
                        title: "Autonomy",
                        choices: autonomyLevels.map { ($0, autonomyLabel($0), autonomyDescriptions[$0]) },
                        selected: autonomyLevel ?? "off"
                    ) { onAutonomy($0); sheet = nil }
                case .tools:
                    RadioSheet(
                        title: "Tool calls",
                        choices: ToolMode.allCases.map { ($0.rawValue, $0.label, nil) },
                        selected: toolMode?.rawValue
                    ) { value in
                        if let mode = ToolMode(rawValue: value) { onToolMode?(mode) }
                        sheet = nil
                    }
                }
            }
            .environment(\.palette, palette)
            .presentationDetents(which == .model ? [.large] : [.medium])
            .presentationDragIndicator(.visible)
        }
    }

    private func pill(_ text: String, icon: String?, label: String, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            HStack(spacing: 4) {
                if let icon { Image(systemName: icon).font(.system(size: 11)) }
                Text(text).font(.sans(FontSize.xs, .medium)).lineLimit(1)
                Image(systemName: "chevron.down").font(.system(size: 8, weight: .bold))
            }
            .foregroundStyle(palette.mutedForeground)
            .padding(.horizontal, 8)
            .frame(height: 28)
            .background(palette.secondary, in: Capsule())
        }
        .buttonStyle(.plain)
        .accessibilityLabel(label)
        .accessibilityValue(text)
    }
}

/// A sheet of radio rows.
struct RadioSheet: View {
    let title: String
    let choices: [(value: String, label: String, description: String?)]
    let selected: String?
    let onPick: (String) -> Void
    @Environment(\.palette) private var palette

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(spacing: 0) {
                    ForEach(choices, id: \.value) { choice in
                        Button { onPick(choice.value) } label: {
                            HStack(alignment: .top, spacing: Space.md) {
                                Image(systemName: choice.value == selected ? "largecircle.fill.circle" : "circle")
                                    .font(.system(size: 17))
                                    .foregroundStyle(choice.value == selected ? palette.foreground : palette.mutedForeground)
                                VStack(alignment: .leading, spacing: 2) {
                                    Text(choice.label).font(.sans(FontSize.base)).foregroundStyle(palette.foreground)
                                    if let d = choice.description {
                                        Text(d).font(.sans(FontSize.sm)).foregroundStyle(palette.mutedForeground)
                                            .fixedSize(horizontal: false, vertical: true)
                                    }
                                }
                                Spacer()
                            }
                            .padding(.horizontal, Space.lg)
                            .padding(.vertical, Space.md)
                            .contentShape(Rectangle())
                        }
                        .buttonStyle(.plain)
                        .accessibilityAddTraits(choice.value == selected ? .isSelected : [])
                    }
                }
            }
            .navigationTitle(title)
            .navigationBarTitleDisplayMode(.inline)
        }
    }
}

/// Choosing a model: search, filter by brand or favourites, star to keep
/// favourites on this phone.
struct ModelPicker: View {
    let models: [ModelChoice]
    let selected: String?
    let onPick: (String) -> Void
    @Environment(AppModel.self) private var app
    @Environment(\.palette) private var palette
    @State private var query = ""
    @State private var filter: PickerFilter = .all

    var body: some View {
        let favorites = app.preferences.favoriteModels
        let shown = visibleModels(models, favorites: favorites, filter: filter, query: query)
        NavigationStack {
            VStack(spacing: Space.sm) {
                ScrollView(.horizontal, showsIndicators: false) {
                    HStack(spacing: Space.xs) {
                        chip("Favorites", active: filter == .favorites) { filter = filter == .favorites ? .all : .favorites }
                        ForEach(brandsOf(models), id: \.self) { brand in
                            chip(brand.label, active: filter == .brand(brand)) {
                                filter = filter == .brand(brand) ? .all : .brand(brand)
                            }
                        }
                    }
                    .padding(.horizontal, Space.lg)
                }
                .accessibilityLabel("Filter models")
                List(shown) { model in
                    HStack(spacing: Space.sm) {
                        Button { onPick(model.id) } label: {
                            HStack(spacing: Space.sm) {
                                Image(systemName: model.id == selected ? "largecircle.fill.circle" : "circle")
                                    .foregroundStyle(model.id == selected ? palette.foreground : palette.mutedForeground)
                                VStack(alignment: .leading, spacing: 1) {
                                    Text(model.label).font(.sans(FontSize.base)).foregroundStyle(palette.foreground)
                                    Text(model.provider == nil ? "Router" : model.brand.label)
                                        .font(.sans(FontSize.xs)).foregroundStyle(palette.mutedForeground)
                                }
                                Spacer()
                                if let m = model.multiplier {
                                    Text(formatMultiplier(m)).font(.mono(FontSize.xs)).foregroundStyle(palette.mutedForeground)
                                }
                            }
                            .contentShape(Rectangle())
                        }
                        .buttonStyle(.plain)
                        .disabled(model.disabled)
                        .accessibilityAddTraits(model.id == selected ? .isSelected : [])
                        let starred = favorites.contains(model.id)
                        Button {
                            app.preferences.toggle(model.id, in: \.favoriteModels)
                        } label: {
                            Image(systemName: starred ? "star.fill" : "star")
                                .foregroundStyle(starred ? palette.attention : palette.mutedForeground)
                                .frame(width: 32, height: 32)
                        }
                        .buttonStyle(.plain)
                        .accessibilityLabel("\(starred ? "Unstar" : "Star") \(model.label)")
                    }
                    .listRowBackground(palette.background)
                }
                .listStyle(.plain)
            }
            .searchable(text: $query, placement: .navigationBarDrawer(displayMode: .always), prompt: "Search models")
            .navigationTitle("Choose a model")
            .navigationBarTitleDisplayMode(.inline)
        }
    }

    private func chip(_ title: String, active: Bool, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Text(title).font(.sans(FontSize.xs, .medium))
                .foregroundStyle(active ? palette.primaryForeground : palette.foreground)
                .padding(.horizontal, 10).frame(height: 28)
                .background(active ? palette.primary : palette.secondary, in: Capsule())
        }
        .buttonStyle(.plain)
        .accessibilityAddTraits(active ? .isSelected : [])
    }
}
