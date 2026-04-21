import FluxCore
import SwiftUI

struct PowerTrioColumns: View {
    let entry: StatusEntry
    var font: Font = .subheadline

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
            PillRow(
                title: "Solar",
                value: PowerFormatting.format(entry.ppv),
                color: solarColor,
                font: font,
                redacted: entry.isPlaceholder
            )
            PillRow(
                title: "Load",
                value: PowerFormatting.format(entry.pload),
                color: loadColor,
                font: font,
                redacted: entry.isPlaceholder
            )
            PillRow(
                title: entry.gridTitle,
                value: PowerFormatting.format(entry.pgrid),
                color: gridColor,
                font: font,
                redacted: entry.isPlaceholder
            )
        }
    }
}
