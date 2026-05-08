public enum BatteryEnergy {
    // Mirrors cutoffPercent in internal/api/status.go — keep in sync on hardware changes.
    public static let cutoffPercent: Int = 5

    public static func usableKwh(soc: Double, capacityKwh: Double, cutoffPercent: Int) -> Double {
        max(0, (soc - Double(cutoffPercent)) / 100 * capacityKwh)
    }
}
