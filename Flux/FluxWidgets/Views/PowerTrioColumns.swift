import FluxCore
import SwiftUI

struct PowerTrioColumns: View {
    let entry: StatusEntry

    private var offpeakStart: String {
        entry.offpeak?.windowStart ?? OffpeakData.defaultWindowStart
    }

    private var offpeakEnd: String {
        entry.offpeak?.windowEnd ?? OffpeakData.defaultWindowEnd
    }

    var body: some View {
        let offline = entry.staleness == .offline
        let solarColor: Color = offline
            ? .secondary
            : (entry.ppv > 0 ? .green : .secondary)
        let loadColor: Color = offline
            ? .secondary
            : (entry.pload > UserDefaults.fluxAppGroup.loadAlertThreshold ? .red : .primary)
        let gridColor: Color = offline
            ? .secondary
            : GridColor.forGrid(
                pgrid: entry.pgrid,
                pgridSustained: entry.live?.pgridSustained ?? false,
                offpeakWindowStart: offpeakStart,
                offpeakWindowEnd: offpeakEnd,
                now: entry.date
            ).color

        VStack(alignment: .leading, spacing: 4) {
            row(title: "Solar", value: entry.ppv, color: solarColor)
            row(title: "Load", value: entry.pload, color: loadColor)
            row(title: entry.gridTitle, value: entry.pgrid, color: gridColor)
        }
    }

    private func row(title: String, value: Double, color: Color) -> some View {
        HStack(spacing: 8) {
            Text(title)
                .foregroundStyle(.secondary)
                .lineLimit(1)
            Spacer(minLength: 4)
            Text(PowerFormatting.format(value))
                .monospacedDigit()
                .foregroundStyle(color)
                .lineLimit(1)
                .padding(.horizontal, 8)
                .padding(.vertical, 2)
                .background(color.opacity(0.15), in: Capsule())
                .redacted(reason: entry.isPlaceholder ? .placeholder : [])
        }
        .font(.subheadline)
    }
}
