import FluxCore
import SwiftUI

struct PowerTrioColumns: View {
    let entry: StatusEntry
    var font: Font = .subheadline
    var spacing: CGFloat = 4
    var tight: Bool = false

    /// A day with no free window has none — never substitute a default, which
    /// would falsely render the legacy 11:00–14:00 band (Q35).
    private var offpeakStart: String? {
        entry.offpeak?.windowStart
    }

    private var offpeakEnd: String? {
        entry.offpeak?.windowEnd
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

        VStack(alignment: .leading, spacing: spacing) {
            PillRow(
                title: "Solar",
                value: PowerFormatting.format(entry.ppv),
                color: solarColor,
                valueFont: font,
                redacted: entry.isPlaceholder,
                tight: tight
            )
            PillRow(
                title: "Load",
                value: PowerFormatting.format(entry.pload),
                color: loadColor,
                valueFont: font,
                redacted: entry.isPlaceholder,
                tight: tight
            )
            PillRow(
                title: entry.gridTitle,
                value: PowerFormatting.format(entry.pgrid),
                color: gridColor,
                valueFont: font,
                redacted: entry.isPlaceholder,
                tight: tight
            )
        }
    }
}
