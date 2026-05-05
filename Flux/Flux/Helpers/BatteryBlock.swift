import FluxCore
import SwiftUI

/// Battery-focused stats panel used on Day Detail and History day cards.
/// Shows the day's charge/discharge cycle and the lowest SoC reached.
struct BatteryBlock: View {
    var title: String? = "Battery"
    var trailing: String?
    let batteryCharge: Double?
    let batteryDischarge: Double?
    let lowestSOC: Double?
    let lowestSOCTimestamp: Date?
    var offpeakBatteryDeltaPercent: Double?

    var body: some View {
        FluxPanel {
            VStack(spacing: 0) {
                if let title {
                    FluxPanelHeader(label: title, right: trailing)
                }
                FluxStatRow(label: "Battery cycle", value: cycleText)
                FluxStatRow(
                    label: "Lowest",
                    value: lowestValue,
                    sub: lowestSubtitle,
                    last: offpeakBatteryDeltaPercent == nil
                )
                if offpeakBatteryDeltaPercent != nil {
                    FluxStatRow(label: "Charged during off-peak", value: offpeakDeltaText, last: true)
                }
            }
        }
    }

    private var offpeakDeltaText: String {
        guard let value = offpeakBatteryDeltaPercent else { return "—" }
        return String(format: "%+.0f%%", value)
    }

    private var cycleText: String {
        "\(EnergyFormatting.format(batteryCharge)) / \(EnergyFormatting.format(batteryDischarge))"
    }

    private var lowestValue: String {
        guard let lowestSOC else { return "—" }
        return SOCFormatting.format(lowestSOC)
    }

    private var lowestSubtitle: String? {
        guard let lowestSOCTimestamp else { return nil }
        return "SOC at \(DateFormatting.clockTime(from: lowestSOCTimestamp))"
    }
}

extension BatteryBlock {
    init(title: String? = "Battery", trailing: String? = nil, day: DayEnergy) {
        self.init(
            title: title,
            trailing: trailing,
            batteryCharge: day.eCharge,
            batteryDischarge: day.eDischarge,
            lowestSOC: day.socLow,
            lowestSOCTimestamp: day.socLowTime.flatMap(DateFormatting.parseTimestamp)
        )
    }
}

#if DEBUG
#Preview {
    ZStack {
        FluxTheme.Palette.background.ignoresSafeArea()
        BatteryBlock(
            batteryCharge: 6.20,
            batteryDischarge: 5.40,
            lowestSOC: 38,
            lowestSOCTimestamp: DateFormatting.parseTimestamp("2026-05-04T01:14:00Z")
        )
        .padding()
    }
}
#endif
