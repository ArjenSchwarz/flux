import FluxCore
import SwiftUI

/// Universal off-peak block shown on Dashboard and Today.
///
/// No title row — the row shape and copy are recognisable without a header.
/// "Free grid in" was dropped because the Summary block already shows
/// "Grid in (off-peak)" on both screens. When data isn't available, every
/// value renders as an em dash to preserve the row layout.
struct OffPeakBlock: View {
    let offpeak: OffpeakData?
    let lowestSOC: Double?
    let lowestSOCTimestamp: Date?
    let avgLoadWatts: Double?
    var showsBatteryCharged: Bool = true
    var showsAvgLoad: Bool = true

    var body: some View {
        FluxPanel {
            VStack(spacing: 0) {
                if showsBatteryCharged {
                    FluxStatRow(label: "Battery charged", value: deltaText, last: !showsLowest && !showsAvgLoad)
                }
                if showsLowest {
                    FluxStatRow(label: "Lowest", value: lowestValue, sub: lowestSubtitle, last: !showsAvgLoad)
                }
                if showsAvgLoad {
                    FluxStatRow(label: "15m avg load", value: avgLoadText, last: true)
                }
            }
        }
    }

    private var showsLowest: Bool { true }

    private var deltaText: String {
        guard let value = offpeak?.batteryDeltaPercent else { return "—" }
        return String(format: "%+.0f%%", value)
    }

    private var lowestValue: String {
        guard let lowestSOC else { return "—" }
        return SOCFormatting.format(lowestSOC)
    }

    private var lowestSubtitle: String? {
        guard let lowestSOCTimestamp else { return nil }
        return "SOC at \(DateFormatting.clockTime(from: lowestSOCTimestamp))"
    }

    private var avgLoadText: String {
        guard let avgLoadWatts else { return "—" }
        return PowerFormatting.format(avgLoadWatts)
    }
}

#if DEBUG
#Preview {
    ZStack {
        FluxTheme.Palette.background.ignoresSafeArea()
        OffPeakBlock(
            offpeak: OffpeakData(
                windowStart: "11:00",
                windowEnd: "14:00",
                status: .complete,
                gridUsageKwh: 3.42,
                solarKwh: 2.1,
                batteryChargeKwh: 1.8,
                batteryDischargeKwh: 0,
                gridExportKwh: 0,
                batteryDeltaPercent: 24
            ),
            lowestSOC: 38,
            lowestSOCTimestamp: DateFormatting.parseTimestamp("2026-05-04T01:14:00Z"),
            avgLoadWatts: 1680
        )
        .padding()
    }
}
#endif
