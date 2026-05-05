import FluxCore
import Foundation

/// Derived stats for the V5 off-peak block. Keeps the computation off the
/// view layer so the same logic is reused on Dashboard and Today.
struct OffpeakReadingStats: Equatable {
    let lowestSOC: Double?
    let lowestSOCTimestamp: Date?
    let avgLoadWatts: Double?
    let gridImportKwh: Double?

    static let empty = OffpeakReadingStats(
        lowestSOC: nil,
        lowestSOCTimestamp: nil,
        avgLoadWatts: nil,
        gridImportKwh: nil
    )

    /// Computes the lowest SOC and its timestamp across the entire day so
    /// the Day Detail "Lowest" row matches the Dashboard's lowest-of-day
    /// signal. Average load (final 15 minutes of the off-peak window) and
    /// grid import energy stay scoped to the off-peak window — those remain
    /// window-specific stats. The API's `OffpeakData.gridUsageKwh` is the
    /// source of truth when available; the integrated value here is a
    /// fallback for the Day Detail summary split.
    static func compute(date: String, readings: [ParsedReading]) -> OffpeakReadingStats {
        guard !readings.isEmpty else { return .empty }

        let lowest = readings.min { $0.point.soc < $1.point.soc }

        guard let range = DayChartDomain.offpeakRange(for: date) else {
            return OffpeakReadingStats(
                lowestSOC: lowest?.point.soc,
                lowestSOCTimestamp: lowest?.date,
                avgLoadWatts: nil,
                gridImportKwh: nil
            )
        }

        let inWindow = readings.filter { $0.date >= range.start && $0.date < range.end }
        let avgWindow = max(range.start, range.end.addingTimeInterval(-15 * 60))
        let avgPoints = readings.filter { $0.date >= avgWindow && $0.date <= range.end }
        let avgLoad = avgPoints.isEmpty ? nil : avgPoints.map(\.point.pload).reduce(0, +) / Double(avgPoints.count)

        let gridImportKwh = integratedGridImport(inWindow)

        return OffpeakReadingStats(
            lowestSOC: lowest?.point.soc,
            lowestSOCTimestamp: lowest?.date,
            avgLoadWatts: avgLoad,
            gridImportKwh: gridImportKwh
        )
    }

    private static func integratedGridImport(_ readings: [ParsedReading]) -> Double? {
        guard readings.count >= 2 else { return nil }
        var watthours = 0.0
        for index in 1 ..< readings.count {
            let prev = readings[index - 1]
            let curr = readings[index]
            let prevImport = max(0, prev.point.pgrid)
            let currImport = max(0, curr.point.pgrid)
            let hours = curr.date.timeIntervalSince(prev.date) / 3600
            watthours += ((prevImport + currImport) / 2) * hours
        }
        return watthours / 1000
    }
}
