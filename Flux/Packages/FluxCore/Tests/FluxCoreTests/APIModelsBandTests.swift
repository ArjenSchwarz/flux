import Foundation
import Testing
@testable import FluxCore

/// Wire-shape coverage for the band split and the nullable off-peak window
/// (`time-of-use-pricing`).
@Suite
struct APIModelsBandTests {
    // MARK: - bandImports and off-peak provenance

    @Test
    func daySummaryDecodesBandImportsAndOffpeakGeometry() throws {
        let json = """
        {
          "epv": 12.0, "eInput": 23.0, "eOutput": 15.0,
          "eCharge": 8.0, "eDischarge": 7.0,
          "socLow": 20.0, "socLowTime": "2026-08-15T06:00:00+10:00",
          "offpeakGridImportKwh": 3.0,
          "offpeakWindowStart": "10:00",
          "offpeakWindowEnd": "15:00",
          "offpeakIntegratedAt": "2026-08-16T05:00:00Z",
          "offpeakSampleCount": 1500,
          "bandImports": [
            { "start": "00:00", "end": "01:00", "kwh": 1.0 },
            { "start": "01:00", "end": "06:00", "kwh": 4.0 },
            { "start": "06:00", "end": "10:00", "kwh": 2.0 },
            { "start": "15:00", "end": "24:00", "kwh": 8.0 }
          ]
        }
        """
        let summary = try JSONDecoder().decode(DaySummary.self, from: Data(json.utf8))
        #expect(summary.bandImports?.count == 4)
        #expect(summary.bandImports?[1] == BandImport(start: "01:00", end: "06:00", kwh: 4.0))
        #expect(summary.offpeakWindowStart == "10:00")
        #expect(summary.offpeakWindowEnd == "15:00")
        #expect(summary.offpeakIntegratedAt == "2026-08-16T05:00:00Z")
        #expect(summary.offpeakSampleCount == 1500)
    }

    @Test
    func daySummaryWithoutABandSplitDecodesToNil() throws {
        let json = """
        {
          "epv": 12.0, "eInput": 23.0, "eOutput": 15.0,
          "eCharge": 8.0, "eDischarge": 7.0,
          "socLow": null, "socLowTime": null
        }
        """
        let summary = try JSONDecoder().decode(DaySummary.self, from: Data(json.utf8))
        // Absent, not an empty array — an empty array would read as "zero
        // import in every band".
        #expect(summary.bandImports == nil)
        #expect(summary.offpeakWindowStart == nil)
        #expect(summary.offpeakSampleCount == nil)
    }

    @Test
    func dayEnergyDecodesBandImportsAndOffpeakGeometry() throws {
        let json = """
        {
          "date": "2026-08-15",
          "epv": 12.0, "eInput": 23.0, "eOutput": 15.0,
          "eCharge": 8.0, "eDischarge": 7.0,
          "offpeakGridImportKwh": 3.0,
          "offpeakWindowStart": "10:00",
          "offpeakWindowEnd": "15:00",
          "bandImports": [
            { "start": "00:00", "end": "10:00", "kwh": 6.0 },
            { "start": "15:00", "end": "24:00", "kwh": 8.0 }
          ],
          "note": null
        }
        """
        let day = try JSONDecoder().decode(DayEnergy.self, from: Data(json.utf8))
        #expect(day.bandImports?.count == 2)
        #expect(day.offpeakWindowStart == "10:00")
        #expect(day.offpeakWindowEnd == "15:00")
        #expect(day.offpeakIntegratedAt == nil)
    }

    // MARK: - Off-peak row reconstruction

    @Test
    func costInputsBuildTheOffpeakRowFromTheFlatWireFields() {
        let summary = DaySummary(
            epv: nil, eInput: 23, eOutput: 15,
            eCharge: nil, eDischarge: nil,
            socLow: nil, socLowTime: nil,
            offpeakGridImportKwh: 3,
            offpeakWindowStart: "10:00",
            offpeakWindowEnd: "15:00",
            offpeakIntegratedAt: "2026-08-16T05:00:00Z",
            offpeakSampleCount: 1500
        )
        let offpeak = summary.costInputs.offpeak
        #expect(offpeak?.gridImportKwh == 3)
        #expect(offpeak?.geometry.start == "10:00")
        #expect(offpeak?.isUsable == true)
    }

    @Test
    func aDayWithNoOffpeakImportHasNoOffpeakRowAtAll() {
        let summary = DaySummary(
            epv: nil, eInput: 23, eOutput: 15,
            eCharge: nil, eDischarge: nil,
            socLow: nil, socLowTime: nil,
            offpeakGridImportKwh: nil
        )
        #expect(summary.costInputs.offpeak == nil)
    }

    @Test
    func aRowWithoutGeometryFallsBackToTheOnlyWindowItCanHaveHad() {
        let row = OffpeakImport(gridImportKwh: 6)
        #expect(row.geometry.start == "11:00")
        #expect(row.geometry.end == "14:00")
        #expect(row.isUsable)
    }

    @Test
    func aSparseCompleteRowIsNotAMeasurement() {
        let row = OffpeakImport(
            gridImportKwh: 0,
            windowStart: "10:00",
            windowEnd: "15:00",
            integratedAt: "2026-08-16T05:00:00Z",
            sampleCount: 0
        )
        #expect(!row.isUsable)
    }

    // MARK: - Nullable off-peak window (Q35)

    @Test
    func offpeakDataDecodesWithoutWindowStrings() throws {
        let json = """
        {
          "windowStart": null,
          "windowEnd": null,
          "gridUsageKwh": 1.0,
          "solarKwh": null,
          "batteryChargeKwh": null,
          "batteryDischargeKwh": null,
          "gridExportKwh": null,
          "batteryDeltaPercent": null,
          "projectedEndSoc": null
        }
        """
        let offpeak = try JSONDecoder().decode(OffpeakData.self, from: Data(json.utf8))
        #expect(offpeak.windowStart == nil)
        #expect(offpeak.windowEnd == nil)
        #expect(offpeak.gridUsageKwh == 1.0)
    }

    @Test
    func aNoFreeBandDayServesANullOffpeakObject() throws {
        let json = """
        {
          "live": null, "battery": null, "rolling15min": null,
          "offpeak": null, "todayEnergy": null, "note": null
        }
        """
        let status = try JSONDecoder().decode(StatusResponse.self, from: Data(json.utf8))
        #expect(status.offpeak == nil)
    }

    // MARK: - No default-window substitution

    @Test
    func gridTintTreatsAnAbsentWindowAsOutsideTheWindow() {
        let now = Date(timeIntervalSince1970: 1_776_000_000)
        // Sustained import above the threshold is red when there is no free
        // window to protect it — the same outcome as being outside one.
        let tier = GridColor.forGrid(
            pgrid: 900,
            pgridSustained: true,
            offpeakWindowStart: nil,
            offpeakWindowEnd: nil,
            now: now
        )
        #expect(tier == .red)
    }

    @Test
    func cutoffTintIsNeutralWithoutAWindow() {
        let now = Date(timeIntervalSince1970: 1_776_000_000)
        // Far enough out that only the window comparison could escalate it.
        let cutoff = now.addingTimeInterval(5 * 60 * 60)
        #expect(CutoffTimeColor.forCutoff(cutoff, offpeakWindowStart: nil, now: now) == .normal)
    }
}
