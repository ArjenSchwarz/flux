import SwiftUI

/// Shared chrome for the History screen's chart cards: title + KPI on the
/// header row, optional subtitle, then the chart-shaped content.
struct HistoryCardChrome<Content: View>: View {
    let title: String
    let kpi: String
    let subtitle: String?
    @ViewBuilder let content: () -> Content

    init(
        title: String,
        kpi: String,
        subtitle: String? = nil,
        @ViewBuilder content: @escaping () -> Content
    ) {
        self.title = title
        self.kpi = kpi
        self.subtitle = subtitle
        self.content = content
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(alignment: .firstTextBaseline) {
                Text(title)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                Spacer()
                Text(kpi)
                    .font(.headline)
                    .monospacedDigit()
            }
            if let subtitle {
                Text(subtitle)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            content()
        }
        .padding()
        .background(.thinMaterial, in: RoundedRectangle(cornerRadius: 16, style: .continuous))
    }
}
