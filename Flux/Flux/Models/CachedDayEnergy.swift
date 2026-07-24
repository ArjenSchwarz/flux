import FluxCore
import SwiftData

@Model
final class CachedDayEnergy {
    @Attribute(.unique) var date: String
    var epv: Double
    var eInput: Double
    var eOutput: Double
    var eCharge: Double
    var eDischarge: Double
    var offpeakGridImportKwh: Double?
    var offpeakGridExportKwh: Double?
    var peakGridImportKwh: Double?
    var note: String?

    // The rated-band split and the off-peak row's geometry and provenance.
    // Cached alongside the energy values because History serves cached days
    // when a fetch fails: without them the same day would reprice at the
    // fallback tier offline and at the banded tier online, and the two screens
    // would disagree (Data Consistency).
    var bandImports: [BandImport]?
    var offpeakWindowStart: String?
    var offpeakWindowEnd: String?
    var offpeakIntegratedAt: String?
    var offpeakSampleCount: Int?

    // Derived stats persisted as optional Codable values (per
    // daily-derived-stats AC 5.4). SwiftData stores Codable structs as
    // transformable blobs without needing an extra @Relationship — keeping
    // these flat means schema evolution stays trivial: adding optional
    // properties is a SwiftData lightweight migration / no-op in practice.
    var dailyUsage: DailyUsage?
    var socLow: Double?
    var socLowTime: String?
    var peakPeriods: [PeakPeriod]?

    init(from dayEnergy: DayEnergy) {
        date = dayEnergy.date
        epv = dayEnergy.epv
        eInput = dayEnergy.eInput
        eOutput = dayEnergy.eOutput
        eCharge = dayEnergy.eCharge
        eDischarge = dayEnergy.eDischarge
        offpeakGridImportKwh = dayEnergy.offpeakGridImportKwh
        offpeakGridExportKwh = dayEnergy.offpeakGridExportKwh
        peakGridImportKwh = dayEnergy.peakGridImportKwh
        bandImports = dayEnergy.bandImports
        offpeakWindowStart = dayEnergy.offpeakWindowStart
        offpeakWindowEnd = dayEnergy.offpeakWindowEnd
        offpeakIntegratedAt = dayEnergy.offpeakIntegratedAt
        offpeakSampleCount = dayEnergy.offpeakSampleCount
        note = dayEnergy.note
        dailyUsage = dayEnergy.dailyUsage
        socLow = dayEnergy.socLow
        socLowTime = dayEnergy.socLowTime
        peakPeriods = dayEnergy.peakPeriods
    }

    var asDayEnergy: DayEnergy {
        DayEnergy(
            date: date,
            epv: epv,
            eInput: eInput,
            eOutput: eOutput,
            eCharge: eCharge,
            eDischarge: eDischarge,
            offpeakGridImportKwh: offpeakGridImportKwh,
            offpeakGridExportKwh: offpeakGridExportKwh,
            peakGridImportKwh: peakGridImportKwh,
            bandImports: bandImports,
            offpeakWindowStart: offpeakWindowStart,
            offpeakWindowEnd: offpeakWindowEnd,
            offpeakIntegratedAt: offpeakIntegratedAt,
            offpeakSampleCount: offpeakSampleCount,
            note: note,
            dailyUsage: dailyUsage,
            socLow: socLow,
            socLowTime: socLowTime,
            peakPeriods: peakPeriods
        )
    }
}
