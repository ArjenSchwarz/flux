import Foundation

/// Pure formatting helpers for the Compare feature. The rounding
/// behaviour is pinned by `DeltaFormatterTests`; if you need to change
/// the format, update the tests in lockstep.
enum DeltaFormatter {
    /// Returns `.text("▲ 1.2 kWh")` / `.text("▼ 0.4 kWh")` / `.text("— kWh")`
    /// when both inputs are present, or `.reserved` when either is nil.
    /// The chevron / em-dash is selected from the rounded one-decimal
    /// display so a 0.04 kWh raw difference still reads as "—".
    static func sublineContent(current: Double?, comparison: Double?) -> SublineContent {
        guard let current, let comparison else { return .reserved }
        let rounded = roundedOneDecimal(current - comparison)
        return .text("\(indicator(forRounded: rounded)) \(formatMagnitude(rounded))")
    }

    // swiftlint:disable function_parameter_count
    /// Composes the combined row VoiceOver label per AC 7.1, e.g.
    /// "Solar produced: 14.8 kilowatt-hours, up 1.2 kilowatt-hours versus yesterday".
    /// When `current` or `comparison` is nil the comparison clause is
    /// omitted (per-row fallback per AC 7.2).
    static func voiceOverLabel(
        rowLabel: String,
        labelSub: String?,
        primaryValue: String,
        current: Double?,
        comparison: Double?,
        period: ComparePeriod
    ) -> String {
        let prefix = composeLabelPrefix(rowLabel: rowLabel, labelSub: labelSub, primaryValue: primaryValue)
        return prefix + voiceOverComparisonClause(current: current, comparison: comparison, period: period)
    }
    // swiftlint:enable function_parameter_count

    /// Used when Compare is loading or unavailable, or when we want to
    /// surface only the row label and primary value with no comparison
    /// clause. Per AC 7.2.
    static func voiceOverFallbackLabel(
        rowLabel: String,
        labelSub: String?,
        primaryValue: String
    ) -> String {
        composeLabelPrefix(rowLabel: rowLabel, labelSub: labelSub, primaryValue: primaryValue)
    }

    /// Returns the trailing comparison clause used inside a larger
    /// VoiceOver label, e.g. ", up 0.6 kilowatt-hours versus yesterday".
    /// Returns "" when either input is nil so callers can append
    /// unconditionally — the clause is the only text that differs between
    /// the present and fallback paths. Used by `voiceOverLabel` and by
    /// `DayInFiveBlocksPanel`'s composed daylight-row label, which needs
    /// to splice two clauses into one sentence.
    static func voiceOverComparisonClause(
        current: Double?,
        comparison: Double?,
        period: ComparePeriod
    ) -> String {
        guard let current, let comparison else { return "" }
        let rounded = roundedOneDecimal(current - comparison)
        let direction = directionWord(forRounded: rounded)
        let periodName = period.displayName.lowercased()
        if rounded == 0 {
            return ", \(direction) versus \(periodName)"
        }
        let magnitude = String(format: "%.1f", abs(rounded))
        return ", \(direction) \(magnitude) kilowatt-hours versus \(periodName)"
    }

    // MARK: - Helpers

    /// Format the magnitude using %.1f and strip the sign — the indicator
    /// glyph carries the direction.
    private static func formatMagnitude(_ rounded: Double) -> String {
        if rounded == 0 { return "kWh" }
        return String(format: "%.1f kWh", abs(rounded))
    }

    private static func indicator(forRounded rounded: Double) -> String {
        if rounded > 0 { return "▲" }
        if rounded < 0 { return "▼" }
        return "—"
    }

    private static func directionWord(forRounded rounded: Double) -> String {
        if rounded > 0 { return "up" }
        if rounded < 0 { return "down" }
        return "unchanged"
    }

    /// Round to one decimal place via String(format:) so the test cases
    /// reading "current=10.05, comparison=10.0 → 0.1" stay pinned to the
    /// printf rounding rule (banker's rounding does not apply here).
    private static func roundedOneDecimal(_ value: Double) -> Double {
        Double(String(format: "%.1f", value)) ?? 0
    }

    private static func composeLabelPrefix(
        rowLabel: String,
        labelSub: String?,
        primaryValue: String
    ) -> String {
        if let labelSub, !labelSub.isEmpty {
            return "\(rowLabel), \(labelSub): \(primaryValue)"
        }
        return "\(rowLabel): \(primaryValue)"
    }
}
