import FluxCore
import Foundation

/// Client-only projection of the comparison day's `DayDetailResponse`.
/// Only the energy fields needed by the SummaryBlock and DayInFiveBlocksPanel
/// are pulled across; the rest of the response (readings, peakPeriods, note)
/// is dropped because Compare does not surface them.
struct ComparisonSnapshot: Sendable, Equatable {
    let date: String
    let solar: Double?
    let gridImport: Double?
    let gridExport: Double?
    let batteryCharge: Double?
    let batteryDischarge: Double?
    let offpeakGridImport: Double?
    /// Server-computed peak grid import for the comparison day (past days).
    /// nil for today or gate-failed days → the computed `peakGridImport` falls
    /// back to the residual. `var` so the memberwise initialiser treats it as
    /// optional (omittable) for existing call sites.
    var peakGridImportServer: Double?
    let dailyUsage: DailyUsage?

    var houseUsed: Double? {
        HouseholdLoad.kwh(
            solar: solar,
            gridImport: gridImport,
            gridExport: gridExport,
            batteryCharge: batteryCharge,
            batteryDischarge: batteryDischarge
        )
    }

    /// Prefers the server-computed peak (peak-from-readings Decision 8) for the
    /// comparison day. Production data always carries the off-peak split
    /// (Decision 10), but the wire type is optional, so the residual guard is
    /// defensive: when both are unexpectedly nil this returns nil and the
    /// per-row fallback handles the Grid in (peak) row.
    var peakGridImport: Double? {
        if let peakGridImportServer { return peakGridImportServer }
        guard let gridImport, let offpeakGridImport else { return nil }
        return max(0, gridImport - offpeakGridImport)
    }

    /// Returns `nil` when the response carries neither a `summary` nor a
    /// `dailyUsage` — the empty-day shape used by `/day` to signal "no data
    /// for this date" without an HTTP error. A response with summary
    /// present but `dailyUsage` nil (or vice versa) stays `.ready`; the
    /// per-row fallback in `DeltaFormatter` and the per-block fallback in
    /// the panels handle the missing fields.
    static func from(date: String, response: DayDetailResponse) -> ComparisonSnapshot? {
        guard response.summary != nil || response.dailyUsage != nil else {
            return nil
        }
        return ComparisonSnapshot(
            date: date,
            solar: response.summary?.epv,
            gridImport: response.summary?.eInput,
            gridExport: response.summary?.eOutput,
            batteryCharge: response.summary?.eCharge,
            batteryDischarge: response.summary?.eDischarge,
            offpeakGridImport: response.summary?.offpeakGridImportKwh,
            peakGridImportServer: response.summary?.peakGridImportKwh,
            dailyUsage: response.dailyUsage
        )
    }
}
