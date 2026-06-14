import FluxCore
import SwiftUI

/// Hero battery numeral on the Dashboard. The numeral and the rest of the
/// panel are rendered in the user-selected app font (Settings → App font),
/// resolved via `\.appFontFamily` in the environment.
struct DashboardHeroPanel<Accessory: View>: View {
    let live: LiveData?
    let rolling15min: RollingAvg?
    let battery: BatteryInfo?
    let offpeakWindowStart: String?
    /// Server-computed projected SoC (percent) at the off-peak window end, from
    /// the same `/status` response. The server only returns it during the
    /// window on fresh live data; the panel renders it on the charging subline
    /// when present (AC 4.1) and shows nothing when nil (AC 4.3).
    let projectedOffpeakEndSoc: Double?
    /// Off-peak window end label (e.g. "14:00") used to time-stamp the
    /// projection, so the labelled time matches the projected time (AC 4.2).
    let offpeakWindowEnd: String?
    /// When true the discharge rate and "empty by" reflect a simulated added
    /// load, so they render in the simulation accent and are announced as
    /// simulated (Req 5.3/5.4).
    let isSimulating: Bool
    /// Control pinned to the top-trailing of the hero, beside the SoC numeral
    /// (the Dashboard passes the Simulate menu). Laid out as a sibling of the
    /// numeral — not an overlay — so the numeral shrinks slightly on a narrow
    /// device rather than running under the control.
    @ViewBuilder let accessory: () -> Accessory

    init(
        live: LiveData?,
        rolling15min: RollingAvg?,
        battery: BatteryInfo? = nil,
        offpeakWindowStart: String? = nil,
        projectedOffpeakEndSoc: Double? = nil,
        offpeakWindowEnd: String? = nil,
        isSimulating: Bool = false,
        @ViewBuilder accessory: @escaping () -> Accessory = { EmptyView() }
    ) {
        self.live = live
        self.rolling15min = rolling15min
        self.battery = battery
        self.offpeakWindowStart = offpeakWindowStart
        self.projectedOffpeakEndSoc = projectedOffpeakEndSoc
        self.offpeakWindowEnd = offpeakWindowEnd
        self.isSimulating = isSimulating
        self.accessory = accessory
    }

    /// Colour for the discharge/charge rate and "empty by" time in the
    /// subline. The empty-by time is normally amber; while simulating the
    /// whole simulated subline takes the simulation accent.
    private var sublineAccent: Color {
        isSimulating ? FluxTheme.Palette.simulation : FluxTheme.Palette.secondaryText
    }

    private var cutoffAccent: Color {
        isSimulating ? FluxTheme.Palette.simulation : FluxTheme.Palette.amber
    }

    var body: some View {
        FluxPanel(padding: FluxTheme.Metrics.panelHeroPadding) {
            VStack(alignment: .leading, spacing: 0) {
                HStack(alignment: .top, spacing: 8) {
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
                    // Scale the numeral and its unit together so a wide reading
                    // (e.g. "84.3%") shrinks as one on a narrow device rather
                    // than the numeral alone shrinking while the % stays full
                    // size.
                    .lineLimit(1)
                    .minimumScaleFactor(0.6)
                    Spacer(minLength: 8)
                    accessory()
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

    /// Leads with the live power-flow rate (when discharging or charging) so
    /// the indicator carries the same context as the "empty by" line, then
    /// states the off-peak floor. Rendered entirely in `secondaryText` — the
    /// time is deliberately not amber (Decision 9: this is an informational
    /// state, not a cutoff warning).
    private func cantEmptyBeforeOffpeakIndicator(offpeakWindowStart: String) -> some View {
        Text(
            Self.cantEmptyBeforeOffpeakVisibleText(
                prefix: powerFlowPrefix,
                offpeakWindowStart: offpeakWindowStart
            )
        )
        .appFont(FluxTheme.Typography.heroSubline)
        .foregroundStyle(FluxTheme.Palette.secondaryText)
        .accessibilityLabel(
            Self.cantEmptyBeforeOffpeakAccessibilityLabel(offpeakWindowStart: offpeakWindowStart)
        )
    }

    /// Visible indicator text. When the battery is discharging or charging the
    /// line leads with the rate prefix; otherwise it falls back to the bare
    /// form. `offpeakWindowStart` is already an `HH:MM` string from the wire —
    /// substituted verbatim.
    static func cantEmptyBeforeOffpeakVisibleText(prefix: String?, offpeakWindowStart: String) -> String {
        if let prefix {
            return "\(prefix) · won't empty before \(offpeakWindowStart)"
        }
        return "Won't empty before \(offpeakWindowStart)"
    }

    /// The leading `Discharging · 2.40 kW` / `Charging · 1.20 kW` segment shared
    /// with the status line. Nil when idle or awaiting data — there is no
    /// meaningful rate to show, so the indicator falls back to bare text.
    private var powerFlowPrefix: String? {
        switch mode {
        case .discharging(let watts, _):
            return Self.dischargingLabel(watts)
        case .charging(let watts):
            return Self.chargingLabel(watts)
        case .idle, .unknown:
            return nil
        }
    }

    // Single source for the power-flow labels — consumed by both
    // `powerFlowPrefix` and the status line so the two cannot drift on wording
    // or formatting.
    static func dischargingLabel(_ watts: Double) -> String {
        "Discharging · \(PowerFormatting.format(watts))"
    }

    static func chargingLabel(_ watts: Double) -> String {
        "Charging · \(PowerFormatting.format(watts))"
    }

    /// The projected-SoC suffix for the charging subline, or nil when the
    /// server returned no projection (outside the window / no live data). The
    /// panel never re-derives the value — it only formats what `/status`
    /// provides (AC 4.3). Internal so the present/absent selection (AC 4.1/4.3)
    /// is testable without hosting the view body.
    var projectedCharge: (text: String, accessibility: String)? {
        guard let soc = projectedOffpeakEndSoc else { return nil }
        return (
            Self.projectedChargeLabel(soc: soc, windowEnd: offpeakWindowEnd),
            Self.projectedChargeAccessibilityLabel(soc: soc, windowEnd: offpeakWindowEnd)
        )
    }

    /// Visible projection suffix, e.g. "~99% by 14:00". The percent is rounded
    /// and prefixed with "~" because it is an idealised best-case figure (4.5
    /// kW to 95%, then trickle), not a measured value. Falls back to the bare
    /// percent when the window end is unknown.
    static func projectedChargeLabel(soc: Double, windowEnd: String?) -> String {
        let pct = String(format: "~%.0f%%", soc)
        guard let windowEnd else { return pct }
        return "\(pct) by \(windowEnd)"
    }

    /// VoiceOver reading for the projection suffix — spells out "about N
    /// percent" so the "~" is not announced as "tilde".
    static func projectedChargeAccessibilityLabel(soc: Double, windowEnd: String?) -> String {
        let pct = "about \(Int(soc.rounded())) percent"
        guard let windowEnd else { return pct }
        return "\(pct) by \(windowEnd)"
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
                    Text("\(Self.dischargingLabel(watts)) · ")
                        .foregroundStyle(sublineAccent)
                    Text("empty by ")
                        .foregroundStyle(sublineAccent)
                    Text(DateFormatting.clockTime(from: cutoff))
                        .foregroundStyle(cutoffAccent)
                        .monospacedDigit()
                } else {
                    Text(Self.dischargingLabel(watts))
                        .foregroundStyle(sublineAccent)
                }
            }
            .appFont(FluxTheme.Typography.heroSubline)
            .modifier(SimulatedSublineAccessibility(isSimulating: isSimulating))
        case .charging(let watts):
            if let projectedCharge {
                // Mirror the discharge "empty by" line: the rate in the
                // standard accent, then the idealised projected SoC at the
                // window end in amber (Decision 10).
                HStack(spacing: 4) {
                    Text("\(Self.chargingLabel(watts)) · ")
                        .foregroundStyle(sublineAccent)
                    Text(projectedCharge.text)
                        .foregroundStyle(cutoffAccent)
                        .monospacedDigit()
                        .accessibilityLabel(projectedCharge.accessibility)
                }
                .appFont(FluxTheme.Typography.heroSubline)
                .modifier(SimulatedSublineAccessibility(isSimulating: isSimulating))
            } else {
                Text(Self.chargingLabel(watts))
                    .appFont(FluxTheme.Typography.heroSubline)
                    .foregroundStyle(sublineAccent)
                    .modifier(SimulatedSublineAccessibility(isSimulating: isSimulating))
            }
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

/// Appends a ", simulated" suffix to the subline's VoiceOver announcement
/// while a simulation is active (Req 5.4), leaving the visible text untouched.
/// A no-op when not simulating so the default reading is unchanged.
private struct SimulatedSublineAccessibility: ViewModifier {
    let isSimulating: Bool

    func body(content: Content) -> some View {
        if isSimulating {
            content.accessibilityLabel("Simulated battery flow")
        } else {
            content
        }
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
            // Off-peak charging with the projected end-of-window SoC appended.
            DashboardHeroPanel(
                live: LiveData(
                    ppv: 0,
                    pload: 600,
                    pbat: -4500,
                    pgrid: 5100,
                    pgridSustained: true,
                    soc: 84.3,
                    timestamp: "2026-06-14T12:34:00Z"
                ),
                rolling15min: nil,
                projectedOffpeakEndSoc: 99.1,
                offpeakWindowEnd: "14:00"
            )
        }
        .padding()
    }
}
#endif
