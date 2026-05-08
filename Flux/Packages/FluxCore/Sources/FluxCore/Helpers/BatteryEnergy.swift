/// Shared battery-energy helpers. The iOS backend (`internal/api/status.go`)
/// holds the canonical cutoff value; this constant mirrors it for views that
/// can't read it from the API response (Day Detail charts get no
/// `BatteryInfo`). Keep the two in sync when the AlphaESS minimum-discharge
/// setting changes.
public enum BatteryEnergy {
    /// Battery cutoff percent — the SOC at which the AlphaESS battery stops
    /// discharging. Mirrors `cutoffPercent` in `internal/api/status.go`.
    public static let cutoffPercent: Int = 5

    /// Usable kWh remaining at the given SOC, clamped at 0.
    public static func usableKwh(soc: Double, capacityKwh: Double, cutoffPercent: Int) -> Double {
        guard capacityKwh > 0 else { return 0 }
        return max(0, (soc - Double(cutoffPercent)) / 100 * capacityKwh)
    }
}
