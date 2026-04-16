import SwiftData

@Model
final class CachedDayEnergy {
    @Attribute(.unique) var date: String
    var epv: Double
    var eInput: Double
    var eOutput: Double
    var eCharge: Double
    var eDischarge: Double

    init(from dayEnergy: DayEnergy) {
        date = dayEnergy.date
        epv = dayEnergy.epv
        eInput = dayEnergy.eInput
        eOutput = dayEnergy.eOutput
        eCharge = dayEnergy.eCharge
        eDischarge = dayEnergy.eDischarge
    }

    var asDayEnergy: DayEnergy {
        DayEnergy(
            date: date,
            epv: epv,
            eInput: eInput,
            eOutput: eOutput,
            eCharge: eCharge,
            eDischarge: eDischarge
        )
    }
}
