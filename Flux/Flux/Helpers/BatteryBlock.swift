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
    /// When true, the "Charged during off-peak" row is always rendered —
    /// showing "—" if `offpeakBatteryDeltaPercent` is nil. Matches the V4
    /// off-peak block's behaviour so the row stays visible before today's
    /// off-peak window has produced data.
    var showsOffpeakDelta: Bool = false
    /// Live SOC, pack capacity, and cutoff threshold. When all three are
    /// supplied (Dashboard only), a top "Energy left" row is rendered
    /// showing usable kWh: `(soc − cutoff) / 100 × capacity`, clamped at 0.
    /// Day Detail / History callsites omit these and the row is hidden.
    var currentSOC: Double?
    var capacityKwh: Double?
    var cutoffPercent: Int?

    var body: some View {
        FluxPanel {
            VStack(spacing: 0) {
                if let title {
                    FluxPanelHeader(label: title, right: trailing)
                }
                if let energyLeft = energyLeftKwh {
                    FluxStatRow(label: "Energy left", value: EnergyFormatting.format(energyLeft))
                }
                FluxStatRow(label: "Battery cycle", value: cycleText)
                FluxStatRow(
                    label: "Lowest",
                    value: lowestValue,
                    sub: lowestSubtitle,
                    last: !rendersOffpeakDelta
                )
                if rendersOffpeakDelta {
                    FluxStatRow(
                        label: "Charged during off-peak",
                        value: offpeakDeltaText,
                        last: true
                    )
                }
            }
        }
    }

    private var rendersOffpeakDelta: Bool {
        showsOffpeakDelta || offpeakBatteryDeltaPercent != nil
    }

    private var energyLeftKwh: Double? {
        guard let soc = currentSOC,
              let capacity = capacityKwh,
              let cutoff = cutoffPercent,
              capacity > 0 else { return nil }
        return max(0, (soc - Double(cutoff)) / 100 * capacity)
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
