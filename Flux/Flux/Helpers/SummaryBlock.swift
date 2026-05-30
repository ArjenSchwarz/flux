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
    /// Server-computed peak grid import (peak-from-readings). When non-nil the
    /// "Grid in (peak)" row uses it directly; when nil the row falls back to
    /// the `gridImport − offpeak` residual. Today / Dashboard has no server
    /// value and stays on the residual.
    var serverPeakGridImport: Double?
    let batteryCharge: Double?
    let batteryDischarge: Double?
    var showsBatteryCycle: Bool = true
    var avgLoadWatts: Double?
    var compare: ComparisonState = .off

    var body: some View {
        FluxPanel {
            VStack(spacing: 0) {
                if let title {
                    FluxPanelHeader(label: title, right: trailing)
                }
                if let avgLoadWatts {
                    FluxStatRow(label: "15m avg load", value: PowerFormatting.format(avgLoadWatts))
                }
                solarRow
                houseUsedRow
                gridInRows
                gridOutRow
                if showsBatteryCycle {
                    FluxStatRow(label: "Battery cycle", value: batteryCycleText, last: true)
                }
            }
        }
    }

    private var solarRow: some View {
        compareRow(
            label: "Solar produced",
            value: kwh(solar),
            sub: nil,
            accent: FluxTheme.Palette.amber,
            last: false,
            current: solar,
            comparison: snapshot?.solar
        )
    }

    private var houseUsedRow: some View {
        compareRow(
            label: "House used",
            value: kwh(houseUsed),
            sub: nil,
            accent: nil,
            last: false,
            current: houseUsed,
            comparison: snapshot?.houseUsed
        )
    }

    @ViewBuilder
    private var gridInRows: some View {
        if showsGridSplit, let peak = peakGridImport {
            // Off-peak is shown as 0 (not "missing") when a peak value exists
            // but the off-peak window hasn't opened yet — today before 11:00
            // (T-1420). `showsGridSplit` keeps this branch off when neither a
            // server peak nor an off-peak value is available, so we never imply
            // a misleading "off-peak 0" on a day with genuinely unknown split.
            let offpeak = offpeakGridImport ?? 0
            compareRow(
                label: "Grid in (peak)",
                value: kwh(peak),
                sub: "paid",
                accent: FluxTheme.Palette.grid,
                last: false,
                current: peak,
                comparison: snapshot?.peakGridImport
            )
            compareRow(
                label: "Grid in (off-peak)",
                value: kwh(offpeak),
                sub: "free",
                accent: FluxTheme.Palette.offpeak,
                last: false,
                current: offpeak,
                comparison: snapshot?.offpeakGridImport
            )
        } else {
            // No split information at all (no server peak, no off-peak value):
            // showing it all under "peak" would be misleading, so render a
            // single combined row instead.
            FluxStatRow(
                label: "Grid in",
                value: kwh(gridImport),
                accent: FluxTheme.Palette.grid
            )
        }
    }

    /// Whether to render the peak/off-peak split. True when the server gives an
    /// authoritative peak (today's live value or a stored past day) or when an
    /// off-peak value is present to derive the residual. False leaves only the
    /// combined "Grid in" row, so a day with a genuinely unknown split never
    /// implies an off-peak of 0.
    private var showsGridSplit: Bool {
        serverPeakGridImport != nil || offpeakGridImport != nil
    }

    private var gridOutRow: some View {
        compareRow(
            label: "Grid out",
            value: kwh(gridExport),
            sub: nil,
            accent: FluxTheme.Palette.gridExport,
            last: !showsBatteryCycle,
            current: gridExport,
            comparison: snapshot?.gridExport
        )
    }

    // swiftlint:disable function_parameter_count
    private func compareRow(
        label: String,
        value: String,
        sub: String?,
        accent: Color?,
        last: Bool,
        current: Double?,
        comparison: Double?
    ) -> FluxStatRow {
        FluxStatRow(
            label: label,
            value: value,
            sub: sub,
            accent: accent,
            last: last,
            valueSub: valueSub(current: current, comparison: comparison),
            accessibilityOverride: accessibilityOverride(
                rowLabel: label,
                labelSub: sub,
                // Spoken primary value uses "kilowatt-hours" rather than
                // the displayed "kWh" so VoiceOver doesn't read it as the
                // letters k-W-h. AC 7.1.
                primary: EnergyFormatting.formatSpoken(current),
                current: current,
                comparison: comparison
            )
        )
    }
    // swiftlint:enable function_parameter_count

    private var snapshot: ComparisonSnapshot? {
        if case .ready(let snapshot, _) = compare { return snapshot }
        return nil
    }

    private func valueSub(current: Double?, comparison: Double?) -> SublineContent {
        SummaryBlockCompareMapping.valueSub(current: current, comparison: comparison, compare: compare)
    }

    private func accessibilityOverride(
        rowLabel: String,
        labelSub: String?,
        primary: String,
        current: Double?,
        comparison: Double?
    ) -> String? {
        SummaryBlockCompareMapping.accessibilityOverride(
            rowLabel: rowLabel,
            labelSub: labelSub,
            primary: primary,
            current: current,
            comparison: comparison,
            compare: compare
        )
    }

    private var peakGridImport: Double? {
        if let serverPeakGridImport { return serverPeakGridImport }
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

/// Per-row compare-state mapping used by `SummaryBlock`. Factored out so
/// the per-row logic is unit-testable without rendering the SwiftUI view.
enum SummaryBlockCompareMapping {
    static func valueSub(current: Double?, comparison: Double?, compare: ComparisonState) -> SublineContent {
        compare.subline(current: current, comparison: comparison)
    }

    // swiftlint:disable function_parameter_count
    static func accessibilityOverride(
        rowLabel: String,
        labelSub: String?,
        primary: String,
        current: Double?,
        comparison: Double?,
        compare: ComparisonState
    ) -> String? {
        switch compare {
        case .off:
            return nil
        case .loading, .unavailable:
            return DeltaFormatter.voiceOverFallbackLabel(
                rowLabel: rowLabel,
                labelSub: labelSub,
                primaryValue: primary
            )
        case .ready(_, let period):
            return DeltaFormatter.voiceOverLabel(
                rowLabel: rowLabel,
                labelSub: labelSub,
                primaryValue: primary,
                current: current,
                comparison: comparison,
                period: period
            )
        }
    }
    // swiftlint:enable function_parameter_count
}

extension SummaryBlock {
    init(
        title: String? = nil,
        trailing: String? = nil,
        todayEnergy: TodayEnergy?,
        offpeakGridImport: Double?,
        serverPeakGridImport: Double? = nil,
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
            serverPeakGridImport: serverPeakGridImport,
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
        showsBatteryCycle: Bool = true,
        compare: ComparisonState = .off
    ) {
        self.init(
            title: title,
            trailing: trailing,
            solar: summary?.epv,
            gridImport: summary?.eInput,
            gridExport: summary?.eOutput,
            offpeakGridImport: offpeakGridImport,
            serverPeakGridImport: summary?.peakGridImportKwh,
            batteryCharge: summary?.eCharge,
            batteryDischarge: summary?.eDischarge,
            showsBatteryCycle: showsBatteryCycle,
            compare: compare
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
            serverPeakGridImport: day.peakGridImportKwh,
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
