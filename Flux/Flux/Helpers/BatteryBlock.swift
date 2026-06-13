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
    /// When non-nil, renders an "Energy left" row above "Battery cycle".
    var energyLeftKwh: Double?
    /// Server-computed projected SoC (percent) at the off-peak window end.
    /// When present, the projection row replaces the "Charged during off-peak"
    /// delta row (Decision 9 — they are mutually exclusive). Live-only, so it
    /// is nil on Day Detail / History.
    var projectedOffpeakEndSoc: Double?
    /// Off-peak window end label (e.g. "14:00") from the same `/status`
    /// response, used to label the projection row so the labelled time matches
    /// the time the projection targets (AC 4.2).
    var offpeakWindowEnd: String?

    var body: some View {
        FluxPanel {
            VStack(spacing: 0) {
                if let title {
                    FluxPanelHeader(label: title, right: trailing)
                }
                if let energyLeftKwh {
                    FluxStatRow(label: "Energy left", value: EnergyFormatting.format(energyLeftKwh))
                }
                FluxStatRow(label: "Battery cycle", value: cycleText)
                FluxStatRow(
                    label: "Lowest",
                    value: lowestValue,
                    sub: lowestSubtitle,
                    last: !(projectedOffpeakEndSoc != nil || rendersOffpeakDelta)
                )
                if let offpeakRow {
                    FluxStatRow(
                        label: offpeakRow.label,
                        value: offpeakRow.value,
                        last: true
                    )
                }
            }
        }
    }

    /// The single off-peak row to render, or nil when none applies. The
    /// projection row takes precedence over the delta row (Decision 9); the
    /// two are mutually exclusive. Internal so the selection contract is
    /// testable without view-rendering infrastructure.
    var offpeakRow: (label: String, value: String)? {
        if let projected = projectedOffpeakEndSoc {
            return (projectedLabel, SOCFormatting.format(projected))
        }
        if rendersOffpeakDelta {
            return ("Charged during off-peak", offpeakDeltaText)
        }
        return nil
    }

    var projectedLabel: String {
        "Projected at \(offpeakWindowEnd ?? "off-peak end")"
    }

    var rendersOffpeakDelta: Bool {
        showsOffpeakDelta || offpeakBatteryDeltaPercent != nil
    }

    var offpeakDeltaText: String {
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
