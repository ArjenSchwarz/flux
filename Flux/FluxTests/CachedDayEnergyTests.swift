import FluxCore
import Foundation
import SwiftData
import Testing
@testable import Flux

// Covers AC 5.4, 5.5, 6.8 for the CachedDayEnergy round-trip portion of
// daily-derived-stats. Lives in the host-app target because @Model types
// need to be exercised inside the app module.
@MainActor @Suite(.serialized)
struct CachedDayEnergyTests {
    // AC 6.8: round-trip with all three new sections — DayEnergy → CachedDayEnergy
    // → DayEnergy preserves every field including the new optional ones.
    @Test
    // swiftlint:disable:next function_body_length
    func roundTripWithAllDerivedSections() throws {
        let original = DayEnergy(
            date: "2026-04-14",
            epv: 14.3,
            eInput: 0.25,
            eOutput: 5.94,
            eCharge: 5.7,
            eDischarge: 6.8,
            offpeakGridImportKwh: 0.5,
            offpeakGridExportKwh: 0.1,
            note: "sunny day",
            dailyUsage: DailyUsage(blocks: [
                DailyUsageBlock(
                    kind: .night,
                    start: "2026-04-13T14:00:00Z",
                    end: "2026-04-13T20:30:00Z",
                    totalKwh: 1.8,
                    averageKwhPerHour: 0.28,
                    percentOfDay: 12,
                    status: .complete,
                    boundarySource: .readings
                ),
                DailyUsageBlock(
                    kind: .evening,
                    start: "2026-04-14T08:42:00Z",
                    end: "2026-04-14T14:00:00Z",
                    totalKwh: 2.2,
                    averageKwhPerHour: 0.41,
                    percentOfDay: 13,
                    status: .complete,
                    boundarySource: .readings
                )
            ]),
            socLow: 18.0,
            socLowTime: "2026-04-14T19:45:00Z",
            peakPeriods: [
                PeakPeriod(
                    start: "2026-04-14T07:30:00Z",
                    end: "2026-04-14T08:15:00Z",
                    avgLoadW: 3500,
                    energyWh: 2625
                )
            ]
        )

        let cached = CachedDayEnergy(from: original)
        let roundTripped = cached.asDayEnergy

        #expect(roundTripped.date == original.date)
        #expect(roundTripped.epv == original.epv)
        #expect(roundTripped.note == original.note)
        #expect(roundTripped.socLow == 18.0)
        #expect(roundTripped.socLowTime == "2026-04-14T19:45:00Z")

        let dailyUsage = try #require(roundTripped.dailyUsage)
        #expect(dailyUsage.blocks.count == 2)
        #expect(dailyUsage.blocks[0].kind == .night)
        #expect(dailyUsage.blocks[0].totalKwh == 1.8)
        #expect(dailyUsage.blocks[0].boundarySource == .readings)
        #expect(dailyUsage.blocks[1].kind == .evening)
        #expect(dailyUsage.blocks[1].percentOfDay == 13)

        let peakPeriods = try #require(roundTripped.peakPeriods)
        #expect(peakPeriods.count == 1)
        #expect(peakPeriods[0].avgLoadW == 3500)
        #expect(peakPeriods[0].energyWh == 2625)
    }

    // AC 6.8: round-trip with no derived sections — historic rows that lacked
    // the new fields still cycle through cleanly with nil values preserved.
    @Test
    func roundTripWithoutDerivedSections() {
        let original = DayEnergy(
            date: "2026-04-14",
            epv: 14.3,
            eInput: 0.25,
            eOutput: 5.94,
            eCharge: 5.7,
            eDischarge: 6.8
        )

        let cached = CachedDayEnergy(from: original)
        let roundTripped = cached.asDayEnergy

        #expect(roundTripped.date == "2026-04-14")
        #expect(roundTripped.dailyUsage == nil)
        #expect(roundTripped.socLow == nil)
        #expect(roundTripped.socLowTime == nil)
        #expect(roundTripped.peakPeriods == nil)
    }

    // AC 5.4: persisting and re-fetching through SwiftData preserves the
    // derived sections (covers the on-disk Codable storage path on the @Model).
    @Test
    func swiftDataPersistsDerivedSections() throws {
        let context = try makeModelContext()
        let original = DayEnergy(
            date: "2026-04-14",
            epv: 14.3,
            eInput: 0.25,
            eOutput: 5.94,
            eCharge: 5.7,
            eDischarge: 6.8,
            dailyUsage: DailyUsage(blocks: [
                DailyUsageBlock(
                    kind: .morningPeak,
                    start: "2026-04-13T20:30:00Z",
                    end: "2026-04-14T01:00:00Z",
                    totalKwh: 2.1,
                    averageKwhPerHour: 0.47,
                    percentOfDay: 12,
                    status: .complete,
                    boundarySource: .readings
                )
            ]),
            socLow: 22.5,
            socLowTime: "2026-04-14T05:00:00Z",
            peakPeriods: [
                PeakPeriod(
                    start: "2026-04-14T07:30:00Z",
                    end: "2026-04-14T08:15:00Z",
                    avgLoadW: 3500,
                    energyWh: 2625
                )
            ]
        )

        context.insert(CachedDayEnergy(from: original))
        try context.save()

        let fetched = try context.fetch(FetchDescriptor<CachedDayEnergy>())
        #expect(fetched.count == 1)
        let row = try #require(fetched.first)
        let restored = row.asDayEnergy
        #expect(restored.socLow == 22.5)
        #expect(restored.socLowTime == "2026-04-14T05:00:00Z")
        #expect(restored.dailyUsage?.blocks.first?.kind == .morningPeak)
        #expect(restored.peakPeriods?.first?.avgLoadW == 3500)
    }

    // AC 5.5: a row that was written without the new derived sections (the
    // pre-feature shape) must load cleanly with the new properties as nil.
    // This exercises the schema-evolution path without needing an external
    // pre-feature on-disk fixture: the test inserts a row that omits the new
    // derived sections, then re-fetches it. SwiftData treats new optional
    // properties as a lightweight, no-op migration; a regression in the model
    // shape that breaks this path will surface here.
    //
    // The full empirical AC 5.5 verification (booting a pre-feature build's
    // on-disk store with the new app binary) still requires a developer-side
    // simulator run before merge — this in-memory check is the unit-test
    // approximation of that contract.
    @Test
    func preFeatureShapeRowLoadsWithNilDerivedSections() throws {
        let context = try makeModelContext()
        let preFeature = DayEnergy(
            date: "2026-04-14",
            epv: 14.3,
            eInput: 0.25,
            eOutput: 5.94,
            eCharge: 5.7,
            eDischarge: 6.8
        )

        context.insert(CachedDayEnergy(from: preFeature))
        try context.save()

        let fetched = try context.fetch(FetchDescriptor<CachedDayEnergy>())
        let row = try #require(fetched.first)
        let restored = row.asDayEnergy

        #expect(restored.dailyUsage == nil)
        #expect(restored.socLow == nil)
        #expect(restored.socLowTime == nil)
        #expect(restored.peakPeriods == nil)
        // Pre-feature properties still decode unchanged.
        #expect(restored.date == "2026-04-14")
        #expect(restored.eInput == 0.25)
    }

    private func makeModelContext() throws -> ModelContext {
        let configuration = ModelConfiguration(isStoredInMemoryOnly: true)
        let container = try ModelContainer(for: CachedDayEnergy.self, configurations: configuration)
        return ModelContext(container)
    }
}
