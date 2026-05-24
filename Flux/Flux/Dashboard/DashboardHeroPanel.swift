import FluxCore
import SwiftUI

/// Hero battery numeral on the Dashboard. The numeral and the rest of the
/// panel are rendered in the user-selected app font (Settings → App font),
/// resolved via `\.appFontFamily` in the environment.
struct DashboardHeroPanel: View {
    let live: LiveData?
    let rolling15min: RollingAvg?
    let battery: BatteryInfo?
    let offpeakWindowStart: String?

    init(
        live: LiveData?,
        rolling15min: RollingAvg?,
        battery: BatteryInfo? = nil,
        offpeakWindowStart: String? = nil
    ) {
        self.live = live
        self.rolling15min = rolling15min
        self.battery = battery
        self.offpeakWindowStart = offpeakWindowStart
    }

    var body: some View {
        FluxPanel(padding: FluxTheme.Metrics.panelHeroPadding) {
            VStack(alignment: .leading, spacing: 0) {
                HStack(alignment: .firstTextBaseline, spacing: 6) {
                    Text(heroNumber)
                        .appFont(FluxTheme.Typography.heroNumber)
                        .tracking(-4)
                        .foregroundStyle(FluxTheme.Palette.amber)
                        .monospacedDigit()
                        .accessibilityLabel(accessibilityValue)
                    Text("%")
                        .appFont(FluxTheme.Typography.heroUnit)
                        .foregroundStyle(FluxTheme.Palette.tertiaryText)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                // Trim the giant glyph's intrinsic leading so the panel's
                // visible whitespace matches the standard 18pt panel padding
                // used by the off-peak and summary panels below.
                .padding(.top, -10)
                .padding(.bottom, -10)

                subline
                    .padding(.vertical, FluxTheme.Metrics.statRowVerticalPadding)
            }
        }
    }

    /// When the server flags the battery as unable to reach the cutoff before
    /// the next off-peak window starts, we hide the standard status line and
    /// show the indicator instead — only one is visible at a time.
    @ViewBuilder
    private var subline: some View {
        if battery?.cantEmptyBeforeOffpeak == true, let offpeakWindowStart {
            cantEmptyBeforeOffpeakIndicator(offpeakWindowStart: offpeakWindowStart)
        } else {
            statusLine
        }
    }

    private func cantEmptyBeforeOffpeakIndicator(offpeakWindowStart: String) -> some View {
        Text(Self.cantEmptyBeforeOffpeakVisibleText(offpeakWindowStart: offpeakWindowStart))
            .appFont(FluxTheme.Typography.heroSubline)
            .foregroundStyle(FluxTheme.Palette.secondaryText)
            .accessibilityLabel(
                Self.cantEmptyBeforeOffpeakAccessibilityLabel(offpeakWindowStart: offpeakWindowStart)
            )
    }

    /// Visible indicator text. `offpeakWindowStart` is already an `HH:MM`
    /// string from the wire — substituted verbatim.
    static func cantEmptyBeforeOffpeakVisibleText(offpeakWindowStart: String) -> String {
        "Won't empty before \(offpeakWindowStart)"
    }

    /// VoiceOver announcement for the indicator. The exact string is the
    /// requirement contract under test (see requirements.md §3.5).
    static func cantEmptyBeforeOffpeakAccessibilityLabel(offpeakWindowStart: String) -> String {
        "Battery won't empty before off-peak at \(offpeakWindowStart)"
    }

    private var heroNumber: String {
        guard let soc = live?.soc else { return "—" }
        if soc >= 99.95 { return "100" }
        return String(format: "%.1f", soc)
    }

    private var accessibilityValue: String {
        guard let soc = live?.soc else { return "Battery percentage unavailable" }
        if soc >= 99.95 { return "100 percent" }
        return String(format: "%.1f percent", soc)
    }

    @ViewBuilder
    private var statusLine: some View {
        switch mode {
        case .discharging(let watts, let cutoff):
            HStack(spacing: 4) {
                if let cutoff {
                    Text("Discharging · \(PowerFormatting.format(watts)) · ")
                        .foregroundStyle(FluxTheme.Palette.secondaryText)
                    Text("empty by ")
                        .foregroundStyle(FluxTheme.Palette.secondaryText)
                    Text(DateFormatting.clockTime(from: cutoff))
                        .foregroundStyle(FluxTheme.Palette.amber)
                        .monospacedDigit()
                } else {
                    Text("Discharging · \(PowerFormatting.format(watts))")
                        .foregroundStyle(FluxTheme.Palette.secondaryText)
                }
            }
            .appFont(FluxTheme.Typography.heroSubline)
        case .charging(let watts):
            Text("Charging · \(PowerFormatting.format(watts))")
                .appFont(FluxTheme.Typography.heroSubline)
                .foregroundStyle(FluxTheme.Palette.secondaryText)
        case .idle:
            Text("Idle · battery holding")
                .appFont(FluxTheme.Typography.heroSubline)
                .foregroundStyle(FluxTheme.Palette.secondaryText)
        case .unknown:
            Text("Awaiting live data")
                .appFont(FluxTheme.Typography.heroSubline)
                .foregroundStyle(FluxTheme.Palette.secondaryText)
        }
    }

    private enum Mode {
        case discharging(watts: Double, cutoff: Date?)
        case charging(watts: Double)
        case idle
        case unknown
    }

    private var mode: Mode {
        guard let live else { return .unknown }
        if live.pbat > 50 {
            let cutoff = rolling15min?.estimatedCutoffTime.flatMap(DateFormatting.parseTimestamp)
            return .discharging(watts: live.pbat, cutoff: cutoff)
        }
        if live.pbat < -50 {
            return .charging(watts: abs(live.pbat))
        }
        return .idle
    }
}

#if DEBUG
#Preview("Default + can't-empty indicator") {
    ZStack {
        FluxTheme.Palette.background.ignoresSafeArea()
        VStack(spacing: 16) {
            DashboardHeroPanel(
                live: MockFluxAPIClient.statusResponse.live,
                rolling15min: MockFluxAPIClient.statusResponse.rolling15min,
                battery: MockFluxAPIClient.statusResponse.battery,
                offpeakWindowStart: MockFluxAPIClient.statusResponse.offpeak?.windowStart
            )
            DashboardHeroPanel(
                live: MockFluxAPIClient.statusResponseCantEmpty.live,
                rolling15min: MockFluxAPIClient.statusResponseCantEmpty.rolling15min,
                battery: MockFluxAPIClient.statusResponseCantEmpty.battery,
                offpeakWindowStart: MockFluxAPIClient.statusResponseCantEmpty.offpeak?.windowStart
            )
        }
        .padding()
    }
}
#endif
