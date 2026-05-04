import FluxCore
import SwiftUI

/// Hero battery numeral on the Dashboard. The numeral is rendered in the
/// user-selected hero font (Settings → Hero font); body and subline stay on
/// San Francisco.
struct DashboardHeroPanel: View {
    let live: LiveData?
    let rolling15min: RollingAvg?
    let heroFont: HeroFontChoice

    var body: some View {
        FluxPanel(padding: FluxTheme.Metrics.panelHeroPadding) {
            VStack(alignment: .leading, spacing: 0) {
                HStack(alignment: .firstTextBaseline, spacing: 6) {
                    Text(heroNumber)
                        .font(heroFont.font(size: 108, weight: .light))
                        .tracking(-4)
                        .foregroundStyle(FluxTheme.Palette.amber)
                        .monospacedDigit()
                        .accessibilityLabel(accessibilityValue)
                    Text("%")
                        .font(heroFont.font(size: 32, weight: .light))
                        .foregroundStyle(FluxTheme.Palette.tertiaryText)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                // Trim the giant glyph's intrinsic leading so the panel's
                // visible whitespace matches the standard 18pt panel padding
                // used by the off-peak and summary panels below.
                .padding(.top, -10)
                .padding(.bottom, -10)

                statusLine
                    .padding(.vertical, FluxTheme.Metrics.statRowVerticalPadding)
            }
        }
    }

    private var heroNumber: String {
        guard let soc = live?.soc else { return "—" }
        return String(format: "%.1f", soc)
    }

    private var accessibilityValue: String {
        guard let soc = live?.soc else { return "Battery percentage unavailable" }
        return String(format: "%.1f percent", soc)
    }

    @ViewBuilder
    private var statusLine: some View {
        switch mode {
        case .discharging(let watts, let cutoff):
            HStack(spacing: 4) {
                Text("Discharging · \(PowerFormatting.format(watts)) · ")
                    .foregroundStyle(FluxTheme.Palette.secondaryText)
                if let cutoff {
                    Text("empty by ")
                        .foregroundStyle(FluxTheme.Palette.secondaryText)
                    Text(DateFormatting.clockTime(from: cutoff))
                        .foregroundStyle(FluxTheme.Palette.amber)
                        .monospacedDigit()
                } else {
                    Text("empty by —")
                        .foregroundStyle(FluxTheme.Palette.secondaryText)
                }
            }
            .font(FluxTheme.Typography.heroSubline)
        case .charging(let watts):
            Text("Charging · \(PowerFormatting.format(watts))")
                .font(FluxTheme.Typography.heroSubline)
                .foregroundStyle(FluxTheme.Palette.secondaryText)
        case .idle:
            Text("Idle · battery holding")
                .font(FluxTheme.Typography.heroSubline)
                .foregroundStyle(FluxTheme.Palette.secondaryText)
        case .unknown:
            Text("Awaiting live data")
                .font(FluxTheme.Typography.heroSubline)
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
#Preview {
    ZStack {
        FluxTheme.Palette.background.ignoresSafeArea()
        DashboardHeroPanel(
            live: MockFluxAPIClient.statusResponse.live,
            rolling15min: MockFluxAPIClient.statusResponse.rolling15min,
            heroFont: .default
        )
        .padding()
    }
}
#endif
