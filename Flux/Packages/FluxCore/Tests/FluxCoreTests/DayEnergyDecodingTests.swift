import Foundation
import Testing
@testable import FluxCore

// Covers AC 5.1, 5.2, 5.3, 6.8 for the DayEnergy decoding side of
// daily-derived-stats. The CachedDayEnergy round-trip lives in the
// host-app FluxTests target because @Model classes need the app's
// SwiftData model container.
@Suite
struct DayEnergyDecodingTests {
    private let decoder = JSONDecoder()

    // AC 6.8: a /history row carrying all three derivedStats sections decodes
    // with non-nil properties.
    @Test
    // swiftlint:disable:next function_body_length
    func decodeDayEnergyWithAllThreeDerivedSections() throws {
        let json = """
        {
          "date": "2026-04-14",
          "epv": 14.3,
          "eInput": 0.25,
          "eOutput": 5.94,
          "eCharge": 5.7,
          "eDischarge": 6.8,
          "socLow": 18.0,
          "socLowTime": "2026-04-14T19:45:00Z",
          "dailyUsage": {
            "blocks": [
              {
                "kind": "night",
                "start": "2026-04-13T14:00:00Z",
                "end": "2026-04-13T20:30:00Z",
                "totalKwh": 1.8,
                "averageKwhPerHour": 0.28,
                "percentOfDay": 12,
                "status": "complete",
                "boundarySource": "readings"
              },
              {
                "kind": "evening",
                "start": "2026-04-14T08:42:00Z",
                "end": "2026-04-14T14:00:00Z",
                "totalKwh": 2.2,
                "averageKwhPerHour": 0.41,
                "percentOfDay": 13,
                "status": "complete",
                "boundarySource": "readings"
              }
            ]
          },
          "peakPeriods": [
            {
              "start": "2026-04-14T07:30:00Z",
              "end": "2026-04-14T08:15:00Z",
              "avgLoadW": 3500,
              "energyWh": 2625
            }
          ]
        }
        """

        let day = try decoder.decode(DayEnergy.self, from: Data(json.utf8))

        #expect(day.socLow == 18.0)
        #expect(day.socLowTime == "2026-04-14T19:45:00Z")

        let dailyUsage = try #require(day.dailyUsage)
        #expect(dailyUsage.blocks.count == 2)
        #expect(dailyUsage.blocks[0].kind == .night)
        #expect(dailyUsage.blocks[1].kind == .evening)

        let peakPeriods = try #require(day.peakPeriods)
        #expect(peakPeriods.count == 1)
        #expect(peakPeriods[0].avgLoadW == 3500)
        #expect(peakPeriods[0].energyWh == 2625)
    }

    // AC 5.2 / 6.8: a historic /history row that lacks the new fields decodes
    // cleanly with the new properties as nil.
    @Test
    func decodeDayEnergyWithoutAnyDerivedSections() throws {
        let json = """
        {
          "date": "2026-04-14",
          "epv": 14.3,
          "eInput": 0.25,
          "eOutput": 5.94,
          "eCharge": 5.7,
          "eDischarge": 6.8
        }
        """

        let day = try decoder.decode(DayEnergy.self, from: Data(json.utf8))

        #expect(day.dailyUsage == nil)
        #expect(day.socLow == nil)
        #expect(day.socLowTime == nil)
        #expect(day.peakPeriods == nil)
        // Existing fields still decode correctly (AC 5.3).
        #expect(day.date == "2026-04-14")
        #expect(day.eInput == 0.25)
    }

    // AC 5.2 / 5.3: explicit nulls in the JSON decode to nil without rejecting
    // the row.
    @Test
    func decodeDayEnergyWithExplicitNullDerivedSections() throws {
        let json = """
        {
          "date": "2026-04-14",
          "epv": 14.3,
          "eInput": 0.25,
          "eOutput": 5.94,
          "eCharge": 5.7,
          "eDischarge": 6.8,
          "socLow": null,
          "socLowTime": null,
          "dailyUsage": null,
          "peakPeriods": null
        }
        """

        let day = try decoder.decode(DayEnergy.self, from: Data(json.utf8))

        #expect(day.dailyUsage == nil)
        #expect(day.socLow == nil)
        #expect(day.socLowTime == nil)
        #expect(day.peakPeriods == nil)
    }

    // Existing init must still accept callers that omit the new params (AC 5.3
    // — the existing properties and init shape continue to work).
    @Test
    func dayEnergyInitWithoutDerivedSectionsLeavesThemNil() {
        let day = DayEnergy(
            date: "2026-04-14",
            epv: 1, eInput: 2, eOutput: 3, eCharge: 4, eDischarge: 5
        )

        #expect(day.dailyUsage == nil)
        #expect(day.socLow == nil)
        #expect(day.socLowTime == nil)
        #expect(day.peakPeriods == nil)
    }
}
