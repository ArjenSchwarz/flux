import FluxCore
import SwiftUI

/// Universal summary block. Used on Dashboard ("Today so far") and Today
/// ("Power"). The order, labels, and accent colours are fixed. The Day
/// Detail / History "Power" block hides the battery cycle row — that row
/// moves to the dedicated `BatteryBlock` on those screens.
struct SummaryBlock: View {
    var title: String?
    var trailing: String?
    let solar: Double?
    let gridImport: Double?
    let gridExport: Double?
    let offpeakGridImport: Double?
    let batteryCharge: Double?
    let batteryDischarge: Double?
    var showsBatteryCycle: Bool = true
    var avgLoadWatts: Double?

    var body: some View {
        FluxPanel {
            VStack(spacing: 0) {
                if let title {
                    FluxPanelHeader(label: title, right: trailing)
                }

                if let avgLoadWatts {
                    FluxStatRow(label: "15m avg load", value: PowerFormatting.format(avgLoadWatts))
                }

                FluxStatRow(
                    label: "Solar produced",
                    value: kwh(solar),
                    accent: FluxTheme.Palette.amber
                )
                FluxStatRow(label: "House used", value: kwh(houseUsed))
                if offpeakGridImport != nil {
                    FluxStatRow(
                        label: "Grid in (peak)",
                        value: kwh(peakGridImport),
                        sub: "paid",
                        accent: FluxTheme.Palette.grid
                    )
                    FluxStatRow(
                        label: "Grid in (off-peak)",
                        value: kwh(offpeakGridImport),
                        sub: "free",
                        accent: FluxTheme.Palette.offpeak
                    )
                } else {
                    // Without a peak/off-peak split (DaySummary currently
                    // doesn't carry one) showing it all under "peak" would be
                    // misleading. Render a single combined row instead.
                    FluxStatRow(
                        label: "Grid in",
                        value: kwh(gridImport),
                        accent: FluxTheme.Palette.grid
                    )
                }
                FluxStatRow(
                    label: "Grid out",
                    value: kwh(gridExport),
                    accent: FluxTheme.Palette.gridExport,
                    last: !showsBatteryCycle
                )
                if showsBatteryCycle {
                    FluxStatRow(label: "Battery cycle", value: batteryCycleText, last: true)
                }
            }
        }
    }

    private var peakGridImport: Double? {
        guard let total = gridImport else { return nil }
        guard let offpeak = offpeakGridImport else { return total }
        return max(0, total - offpeak)
    }

    private var houseUsed: Double? {
        HouseholdLoad.kwh(
            solar: solar,
            gridImport: gridImport,
            gridExport: gridExport,
            batteryCharge: batteryCharge,
            batteryDischarge: batteryDischarge
        )
    }

    private var batteryCycleText: String {
        "\(kwh(batteryCharge)) / \(kwh(batteryDischarge))"
    }

    private func kwh(_ value: Double?) -> String {
        EnergyFormatting.format(value)
    }
}

extension SummaryBlock {
    init(
        title: String? = nil,
        trailing: String? = nil,
        todayEnergy: TodayEnergy?,
        offpeakGridImport: Double?,
        showsBatteryCycle: Bool = true,
        avgLoadWatts: Double? = nil
    ) {
        self.init(
            title: title,
            trailing: trailing,
            solar: todayEnergy?.epv,
            gridImport: todayEnergy?.eInput,
            gridExport: todayEnergy?.eOutput,
            offpeakGridImport: offpeakGridImport,
            batteryCharge: todayEnergy?.eCharge,
            batteryDischarge: todayEnergy?.eDischarge,
            showsBatteryCycle: showsBatteryCycle,
            avgLoadWatts: avgLoadWatts
        )
    }

    init(
        title: String? = nil,
        trailing: String? = nil,
        summary: DaySummary?,
        offpeakGridImport: Double?,
        showsBatteryCycle: Bool = true
    ) {
        self.init(
            title: title,
            trailing: trailing,
            solar: summary?.epv,
            gridImport: summary?.eInput,
            gridExport: summary?.eOutput,
            offpeakGridImport: offpeakGridImport,
            batteryCharge: summary?.eCharge,
            batteryDischarge: summary?.eDischarge,
            showsBatteryCycle: showsBatteryCycle
        )
    }

    init(
        title: String? = nil,
        trailing: String? = nil,
        day: DayEnergy,
        showsBatteryCycle: Bool = true
    ) {
        self.init(
            title: title,
            trailing: trailing,
            solar: day.epv,
            gridImport: day.eInput,
            gridExport: day.eOutput,
            offpeakGridImport: day.offpeakGridImportKwh,
            batteryCharge: day.eCharge,
            batteryDischarge: day.eDischarge,
            showsBatteryCycle: showsBatteryCycle
        )
    }
}

#if DEBUG
#Preview {
    ZStack {
        FluxTheme.Palette.background.ignoresSafeArea()
        SummaryBlock(
            title: "Today so far",
            trailing: "17:38",
            solar: 14.82,
            gridImport: 4.26,
            gridExport: 1.10,
            offpeakGridImport: 3.42,
            batteryCharge: 6.20,
            batteryDischarge: 5.40
        )
        .padding()
    }
}
#endif
