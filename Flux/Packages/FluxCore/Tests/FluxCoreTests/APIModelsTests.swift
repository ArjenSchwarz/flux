import Foundation
import Testing
@testable import FluxCore

// swiftlint:disable type_body_length
@Suite
struct APIModelsTests {
    private let decoder = JSONDecoder()

    // MARK: - /status response

    @Test
    // swiftlint:disable:next function_body_length
    func decodeFullStatusResponse() throws {
        let json = """
        {
          "live": {
            "ppv": 2400,
            "pload": 207,
            "pbat": 500,
            "pgrid": -9,
            "pgridSustained": false,
            "soc": 62.4,
            "timestamp": "2026-04-15T10:00:00Z"
          },
          "battery": {
            "capacityKwh": 10.0,
            "cutoffPercent": 10,
            "estimatedCutoffTime": "2026-04-15T16:03:00Z",
            "low24h": {
              "soc": 38.2,
              "timestamp": "2026-04-14T18:45:00Z"
            }
          },
          "rolling15min": {
            "avgLoad": 243,
            "avgPbat": 150,
            "estimatedCutoffTime": "2026-04-16T03:00:00Z"
          },
          "offpeak": {
            "windowStart": "11:00",
            "windowEnd": "14:00",
            "gridUsageKwh": 6.1,
            "solarKwh": 3.2,
            "batteryChargeKwh": 2.5,
            "batteryDischargeKwh": 0.1,
            "gridExportKwh": 0.3,
            "batteryDeltaPercent": 42.3
          },
          "todayEnergy": {
            "epv": 14.3,
            "eInput": 0.25,
            "eOutput": 5.94,
            "eCharge": 5.7,
            "eDischarge": 6.8
          }
        }
        """

        let status = try decoder.decode(StatusResponse.self, from: Data(json.utf8))

        #expect(status.live?.ppv == 2400)
        #expect(status.live?.soc == 62.4)
        #expect(status.live?.pgridSustained == false)
        #expect(status.battery?.capacityKwh == 10.0)
        #expect(status.battery?.low24h?.soc == 38.2)
        #expect(status.battery?.estimatedCutoffTime == "2026-04-15T16:03:00Z")
        #expect(status.rolling15min?.avgLoad == 243)
        #expect(status.offpeak?.windowStart == "11:00")
        #expect(status.offpeak?.gridUsageKwh == 6.1)
        #expect(status.offpeak?.batteryDeltaPercent == 42.3)
        #expect(status.todayEnergy?.epv == 14.3)
        #expect(status.todayEnergy?.eOutput == 5.94)
    }

    @Test
    func decodeStatusWithNullOptionalFields() throws {
        let json = """
        {
          "live": null,
          "battery": null,
          "rolling15min": null,
          "offpeak": null,
          "todayEnergy": null
        }
        """

        let status = try decoder.decode(StatusResponse.self, from: Data(json.utf8))

        #expect(status.live == nil)
        #expect(status.battery == nil)
        #expect(status.rolling15min == nil)
        #expect(status.offpeak == nil)
        #expect(status.todayEnergy == nil)
    }

    @Test
    func decodeStatusWithMissingOptionalFields() throws {
        let json = "{}"

        let status = try decoder.decode(StatusResponse.self, from: Data(json.utf8))

        #expect(status.live == nil)
        #expect(status.battery == nil)
        #expect(status.rolling15min == nil)
        #expect(status.offpeak == nil)
        #expect(status.todayEnergy == nil)
    }

    @Test
    func decodeBatteryInfoWithNullOptionals() throws {
        let json = """
        {
          "capacityKwh": 10.0,
          "cutoffPercent": 10,
          "estimatedCutoffTime": null,
          "low24h": null
        }
        """

        let battery = try decoder.decode(BatteryInfo.self, from: Data(json.utf8))

        #expect(battery.capacityKwh == 10.0)
        #expect(battery.cutoffPercent == 10)
        #expect(battery.estimatedCutoffTime == nil)
        #expect(battery.low24h == nil)
    }

    @Test
    func decodeRollingAvgWithNullCutoff() throws {
        let json = """
        {
          "avgLoad": 500,
          "avgPbat": 200,
          "estimatedCutoffTime": null
        }
        """

        let rolling = try decoder.decode(RollingAvg.self, from: Data(json.utf8))

        #expect(rolling.avgLoad == 500)
        #expect(rolling.estimatedCutoffTime == nil)
    }

    @Test
    func decodeOffpeakWithNullDeltaFields() throws {
        let json = """
        {
          "windowStart": "11:00",
          "windowEnd": "14:00",
          "gridUsageKwh": null,
          "solarKwh": null,
          "batteryChargeKwh": null,
          "batteryDischargeKwh": null,
          "gridExportKwh": null,
          "batteryDeltaPercent": null
        }
        """

        let offpeak = try decoder.decode(OffpeakData.self, from: Data(json.utf8))

        #expect(offpeak.windowStart == "11:00")
        #expect(offpeak.windowEnd == "14:00")
        #expect(offpeak.gridUsageKwh == nil)
        #expect(offpeak.batteryDeltaPercent == nil)
    }

    // MARK: - /history response

    @Test
    func decodeHistoryResponse() throws {
        let json = """
        {
          "days": [
            {
              "date": "2026-04-14",
              "epv": 14.3,
              "eInput": 0.25,
              "eOutput": 5.94,
              "eCharge": 5.7,
              "eDischarge": 6.8
            },
            {
              "date": "2026-04-13",
              "epv": 12.1,
              "eInput": 1.5,
              "eOutput": 3.2,
              "eCharge": 4.0,
              "eDischarge": 5.5
            }
          ]
        }
        """

        let history = try decoder.decode(HistoryResponse.self, from: Data(json.utf8))

        #expect(history.days.count == 2)
        #expect(history.days[0].date == "2026-04-14")
        #expect(history.days[0].epv == 14.3)
        #expect(history.days[1].eInput == 1.5)
    }

    @Test
    func decodeHistoryWithEmptyDays() throws {
        let json = """
        {
          "days": []
        }
        """

        let history = try decoder.decode(HistoryResponse.self, from: Data(json.utf8))

        #expect(history.days.isEmpty)
    }

    @Test
    func dayEnergyIdentifiable() throws {
        let json = """
        {
          "date": "2026-04-14",
          "epv": 1, "eInput": 2, "eOutput": 3, "eCharge": 4, "eDischarge": 5
        }
        """

        let day = try decoder.decode(DayEnergy.self, from: Data(json.utf8))

        #expect(day.id == "2026-04-14")
    }

    // MARK: - /day response

    @Test
    func decodeDayDetailResponse() throws {
        let json = """
        {
          "date": "2026-04-14",
          "readings": [
            {
              "timestamp": "2026-04-14T00:00:00Z",
              "ppv": 0,
              "pload": 150,
              "pbat": 200,
              "pgrid": -50,
              "soc": 85
            }
          ],
          "summary": {
            "epv": 14.3,
            "eInput": 0.25,
            "eOutput": 5.94,
            "eCharge": 5.7,
            "eDischarge": 6.8,
            "socLow": 38.2,
            "socLowTime": "2026-04-14T18:45:00Z"
          }
        }
        """

        let detail = try decoder.decode(DayDetailResponse.self, from: Data(json.utf8))

        #expect(detail.date == "2026-04-14")
        #expect(detail.readings.count == 1)
        #expect(detail.readings[0].soc == 85)
        #expect(detail.readings[0].pgrid == -50)
        #expect(detail.summary?.socLow == 38.2)
        #expect(detail.summary?.socLowTime == "2026-04-14T18:45:00Z")
    }

    @Test
    func decodeDayDetailWithNullSummary() throws {
        let json = """
        {
          "date": "2026-04-14",
          "readings": [],
          "summary": null
        }
        """

        let detail = try decoder.decode(DayDetailResponse.self, from: Data(json.utf8))

        #expect(detail.readings.isEmpty)
        #expect(detail.summary == nil)
    }

    @Test
    func decodeDaySummaryWithPartialNulls() throws {
        let json = """
        {
          "epv": null,
          "eInput": null,
          "eOutput": null,
          "eCharge": null,
          "eDischarge": null,
          "socLow": 38.2,
          "socLowTime": "2026-04-14T18:45:00Z"
        }
        """

        let summary = try decoder.decode(DaySummary.self, from: Data(json.utf8))

        #expect(summary.epv == nil)
        #expect(summary.eCharge == nil)
        #expect(summary.socLow == 38.2)
        #expect(summary.socLowTime == "2026-04-14T18:45:00Z")
    }

    @Test
    func timeSeriesPointIdentifiable() throws {
        let json = """
        {
          "timestamp": "2026-04-14T10:30:00Z",
          "ppv": 100, "pload": 200, "pbat": 50, "pgrid": -50, "soc": 70
        }
        """

        let point = try decoder.decode(TimeSeriesPoint.self, from: Data(json.utf8))

        #expect(point.id == "2026-04-14T10:30:00Z")
    }

    // MARK: - Error response

    @Test
    func decodeErrorResponse() throws {
        let json = """
        {
          "error": "Unauthorized"
        }
        """

        let errorResponse = try decoder.decode(APIErrorResponse.self, from: Data(json.utf8))

        #expect(errorResponse.error == "Unauthorized")
    }
}
// swiftlint:enable type_body_length
