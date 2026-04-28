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
            note: note
        )
    }
}
