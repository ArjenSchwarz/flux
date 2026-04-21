public enum BatteryColor {
    public static func forSOC(_ soc: Double) -> ColorTier {
        if soc > 60 {
            return .green
        }
        if soc >= 30 {
            return .normal
        }
        if soc >= 15 {
            return .orange
        }
        return .red
    }
}
