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
    var note: String?

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
            note: note,
            dailyUsage: dailyUsage,
            socLow: socLow,
            socLowTime: socLowTime,
            peakPeriods: peakPeriods
        )
    }
}
