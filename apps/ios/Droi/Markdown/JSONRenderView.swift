import DroiKit
import DroidProtocol
import SwiftUI

/// Draws a `<json-render>` spec: Droid's terminal components as native views.
/// Unknown types draw their children.
struct JSONRenderView: View {
    let spec: RenderSpec

    var body: some View {
        RenderNode(spec: spec, id: spec.root, seen: [])
            .frame(maxWidth: .infinity, alignment: .leading)
    }
}

private struct RenderNode: View {
    let spec: RenderSpec
    let id: String
    let seen: Set<String>
    @Environment(\.palette) private var palette

    var body: some View {
        if let element = spec.elements[id] {
            content(element)
        }
    }

    private var children: [String] { childrenOf(spec, id, seen: seen) }

    private func childViews() -> some View {
        ForEach(children, id: \.self) { child in
            RenderNode(spec: spec, id: child, seen: seen.union([id]))
        }
    }

    private func cells(_ n: Double?) -> CGFloat { CGFloat(min(8, max(0, n ?? 0))) * 4 }

    @ViewBuilder
    private func content(_ e: RenderElement) -> some View {
        let p = e.props
        switch e.type {
        case "Box":
            let gap = cells(RenderProps.num(p, "gap"))
            let pad = cells(RenderProps.num(p, "padding"))
            let border = RenderProps.str(p, "borderStyle")
            Group {
                if RenderProps.str(p, "flexDirection") == "row" {
                    HStack(alignment: .top, spacing: gap) { childViews() }
                } else {
                    VStack(alignment: .leading, spacing: gap) { childViews() }
                }
            }
            .padding(pad)
            .overlay {
                if !border.isEmpty && border != "none" {
                    RoundedRectangle(cornerRadius: Radius.md).strokeBorder(palette.border)
                }
            }
        case "Text":
            Text(RenderProps.str(p, "text"))
                .font(.sans(FontSize.sm, p["bold"]?.boolValue == true ? .semibold : .regular))
                .foregroundStyle(color(toneOf(RenderProps.str(p, "color"))))
                .textSelection(.enabled)
        case "Heading":
            Text(RenderProps.str(p, "text"))
                .font(.sans(RenderProps.num(p, "level") == 1 ? FontSize.base : FontSize.sm, .semibold))
                .foregroundStyle(palette.foreground)
                .accessibilityAddTraits(.isHeader)
        case "Divider":
            HStack(spacing: Space.sm) {
                Rectangle().fill(palette.border).frame(height: 0.5)
                let title = RenderProps.str(p, "title")
                if !title.isEmpty {
                    Text(title).font(.sans(FontSize.xs)).foregroundStyle(palette.mutedForeground)
                    Rectangle().fill(palette.border).frame(height: 0.5)
                }
            }
        case "Newline", "Spacer":
            Spacer().frame(height: Space.sm)
        case "Table":
            RenderTable(props: p)
        case "List":
            let ordered = p["ordered"]?.boolValue == true
            VStack(alignment: .leading, spacing: 2) {
                ForEach(Array(RenderProps.strings(p, "items").enumerated()), id: \.offset) { i, item in
                    HStack(alignment: .firstTextBaseline, spacing: Space.sm) {
                        Text(ordered ? "\(i + 1)." : "•").foregroundStyle(palette.mutedForeground)
                        Text(item).foregroundStyle(palette.foreground)
                    }
                    .font(.sans(FontSize.sm))
                }
            }
        case "Card":
            VStack(alignment: .leading, spacing: Space.sm) {
                let title = RenderProps.str(p, "title")
                if !title.isEmpty { Text(title).font(.sans(FontSize.sm, .semibold)) }
                childViews()
            }
            .padding(Space.md)
            .background(palette.card, in: RoundedRectangle(cornerRadius: Radius.lg))
        case "StatusLine":
            let tone = toneOf(RenderProps.str(p, "status"))
            HStack(spacing: Space.sm) {
                Image(systemName: icon(tone)).foregroundStyle(color(tone))
                Text(RenderProps.str(p, "text")).foregroundStyle(palette.foreground)
            }
            .font(.sans(FontSize.sm))
        case "KeyValue":
            HStack(alignment: .firstTextBaseline, spacing: Space.sm) {
                Text(RenderProps.str(p, "label")).foregroundStyle(palette.mutedForeground).frame(width: 112, alignment: .leading)
                Text(RenderProps.str(p, "value")).foregroundStyle(palette.foreground)
            }
            .font(.sans(FontSize.sm))
        case "Badge":
            let tone = toneOf(RenderProps.str(p, "variant"))
            Text(RenderProps.str(p, "label"))
                .font(.sans(FontSize.xs, .medium))
                .foregroundStyle(color(tone))
                .padding(.horizontal, 6).padding(.vertical, 2)
                .background(color(tone).opacity(0.12), in: Capsule())
        case "ProgressBar":
            let value = fraction(RenderProps.num(p, "progress") ?? 0, max: 1)
            VStack(alignment: .leading, spacing: 2) {
                let label = RenderProps.str(p, "label")
                HStack {
                    if !label.isEmpty { Text(label) }
                    Spacer()
                    Text("\(Int((value * 100).rounded()))%")
                }
                .font(.sans(FontSize.xs)).foregroundStyle(palette.mutedForeground)
                GeometryReader { geo in
                    ZStack(alignment: .leading) {
                        Capsule().fill(palette.muted)
                        Capsule().fill(palette.foreground).frame(width: geo.size.width * value)
                    }
                }
                .frame(height: 6)
            }
            .accessibilityElement(children: .ignore)
            .accessibilityValue("\(Int((value * 100).rounded())) percent")
        case "Metric":
            VStack(alignment: .leading, spacing: 2) {
                Text(RenderProps.str(p, "label")).font(.sans(FontSize.xs)).foregroundStyle(palette.mutedForeground)
                HStack(spacing: Space.xs) {
                    Text(RenderProps.str(p, "value")).font(.sans(FontSize.lg, .semibold))
                    switch RenderProps.str(p, "trend") {
                    case "up": Image(systemName: "chart.line.uptrend.xyaxis").foregroundStyle(palette.success)
                    case "down": Image(systemName: "chart.line.downtrend.xyaxis").foregroundStyle(palette.destructiveForeground)
                    default: EmptyView()
                    }
                }
            }
        case "Callout":
            let tip = RenderProps.str(p, "type") == "tip"
            let tone = toneOf(RenderProps.str(p, "type"))
            HStack(alignment: .top, spacing: Space.sm) {
                Image(systemName: tip ? "lightbulb" : icon(tone)).foregroundStyle(color(tone))
                VStack(alignment: .leading, spacing: 2) {
                    let title = RenderProps.str(p, "title")
                    if !title.isEmpty { Text(title).font(.sans(FontSize.sm, .semibold)) }
                    Text(RenderProps.str(p, "content")).font(.sans(FontSize.sm))
                }
            }
            .padding(Space.md)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(palette.card, in: RoundedRectangle(cornerRadius: Radius.lg))
        case "Timeline":
            VStack(alignment: .leading, spacing: Space.sm) {
                ForEach(Array(RenderProps.records(p, "items").enumerated()), id: \.offset) { _, item in
                    let tone = toneOf(RenderProps.str(item, "status"))
                    HStack(alignment: .top, spacing: Space.sm) {
                        Circle().fill(color(tone)).frame(width: 7, height: 7).padding(.top, 5)
                        VStack(alignment: .leading, spacing: 1) {
                            Text(RenderProps.str(item, "title")).font(.sans(FontSize.sm, .medium))
                            let d = RenderProps.str(item, "description")
                            if !d.isEmpty { Text(d).font(.sans(FontSize.xs)).foregroundStyle(palette.mutedForeground) }
                        }
                    }
                }
            }
        case "BarChart":
            let rows = RenderProps.records(p, "data")
            let values = rows.map { RenderProps.num($0, "value") ?? 0 }
            let total = values.reduce(0, +)
            let maxValue = values.max() ?? 1
            let percent = p["showPercentage"]?.boolValue == true
            VStack(alignment: .leading, spacing: 4) {
                ForEach(Array(rows.enumerated()), id: \.offset) { i, row in
                    HStack(spacing: Space.sm) {
                        Text(RenderProps.str(row, "label")).frame(width: 96, alignment: .leading).lineLimit(1)
                        GeometryReader { geo in
                            Capsule().fill(color(toneOf(RenderProps.str(row, "color"))).opacity(0.8))
                                .frame(width: max(2, geo.size.width * fraction(values[i], max: maxValue)))
                        }
                        .frame(height: 8)
                        Text(percent && total > 0 ? "\(Int((values[i] / total * 100).rounded()))%" : JSONText.number(values[i]))
                            .foregroundStyle(palette.mutedForeground)
                    }
                    .font(.sans(FontSize.xs))
                }
            }
        case "Sparkline":
            let data = RenderProps.numbers(p, "data")
            if data.count >= 2 {
                let lo = data.min()!, hi = data.max()!
                Path { path in
                    for (i, v) in data.enumerated() {
                        let x = CGFloat(i) / CGFloat(data.count - 1) * 120
                        let y = 24 - CGFloat(hi == lo ? 0.5 : (v - lo) / (hi - lo)) * 24
                        if i == 0 { path.move(to: CGPoint(x: x, y: y)) } else { path.addLine(to: CGPoint(x: x, y: y)) }
                    }
                }
                .stroke(color(toneOf(RenderProps.str(p, "color"))), lineWidth: 1.5)
                .frame(width: 120, height: 24)
            }
        default:
            VStack(alignment: .leading, spacing: Space.xs) { childViews() }
        }
    }

    private func color(_ tone: Tone) -> Color {
        switch tone {
        case .success: palette.success
        case .warning: palette.attention
        case .error: palette.destructiveForeground
        case .info: palette.working
        case .muted: palette.mutedForeground
        default: palette.foreground
        }
    }

    private func icon(_ tone: Tone) -> String {
        switch tone {
        case .success: "checkmark.circle"
        case .warning: "exclamationmark.triangle"
        case .error: "xmark.circle"
        default: "info.circle"
        }
    }
}

private struct RenderTable: View {
    let props: [String: JSONValue]
    @Environment(\.palette) private var palette

    var body: some View {
        let rows = RenderProps.records(props, "rows")
        let columns = columnList(rows)
        ScrollView(.horizontal, showsIndicators: false) {
            VStack(alignment: .leading, spacing: 0) {
                HStack(spacing: 0) {
                    ForEach(columns, id: \.key) { c in cell(c.header, bold: true) }
                }
                ForEach(Array(rows.enumerated()), id: \.offset) { _, row in
                    Rectangle().fill(palette.border).frame(height: 0.5)
                    HStack(spacing: 0) {
                        ForEach(columns, id: \.key) { c in cell(RenderProps.cell(row[c.key]), bold: false) }
                    }
                }
            }
            .hairlineBorder(palette.border, radius: Radius.md)
        }
    }

    private struct Column { let key: String; let header: String }

    private func columnList(_ rows: [[String: JSONValue]]) -> [Column] {
        let declared = RenderProps.records(props, "columns").map {
            Column(key: RenderProps.str($0, "key"), header: RenderProps.str($0, "header"))
        }
        if !declared.isEmpty { return declared }
        return (rows.first?.keys.sorted() ?? []).map { Column(key: $0, header: $0) }
    }

    private func cell(_ text: String, bold: Bool) -> some View {
        Text(text)
            .font(.sans(FontSize.xs, bold ? .semibold : .regular))
            .foregroundStyle(palette.foreground)
            .padding(.horizontal, Space.sm).padding(.vertical, Space.xs)
            .frame(width: 140, alignment: .leading)
    }
}
