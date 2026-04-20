import Foundation
import Testing
@testable import FluxCore

@Suite
struct StatusSnapshotEnvelopeTests {
    private func makeStatus() -> StatusResponse {
        StatusResponse(
            live: LiveData(
                ppv: 2400,
                pload: 207,
                pbat: 500,
                pgrid: -9,
                pgridSustained: false,
                soc: 62.4,
                timestamp: "2026-04-15T10:00:00Z"
            ),
            battery: BatteryInfo(
                capacityKwh: 10.0,
                cutoffPercent: 10,
                estimatedCutoffTime: "2026-04-15T16:03:00Z",
                low24h: Low24h(soc: 38.2, timestamp: "2026-04-14T18:45:00Z")
            ),
            rolling15min: RollingAvg(avgLoad: 243, avgPbat: 150, estimatedCutoffTime: nil),
            offpeak: nil,
            todayEnergy: nil
        )
    }

    private func makeEncoder() -> JSONEncoder {
        let encoder = JSONEncoder()
        encoder.dateEncodingStrategy = .iso8601
        return encoder
    }

    private func makeDecoder() -> JSONDecoder {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        return decoder
    }

    @Test
    func roundTripPreservesAllFields() throws {
        let fetchedAt = Date(timeIntervalSince1970: 1_767_225_600) // 2026-01-01T00:00:00Z
        let envelope = StatusSnapshotEnvelope(
            fetchedAt: fetchedAt,
            status: makeStatus()
        )

        let data = try makeEncoder().encode(envelope)
        let decoded = try makeDecoder().decode(StatusSnapshotEnvelope.self, from: data)

        #expect(decoded.schemaVersion == envelope.schemaVersion)
        #expect(decoded.fetchedAt == envelope.fetchedAt)
        #expect(decoded.status.live?.soc == 62.4)
        #expect(decoded.status.battery?.capacityKwh == 10.0)
        #expect(decoded.status.rolling15min?.avgLoad == 243)
    }

    @Test
    func schemaVersionDefaultsToCurrent() {
        let envelope = StatusSnapshotEnvelope(fetchedAt: .now, status: makeStatus())

        #expect(envelope.schemaVersion == StatusSnapshotEnvelope.currentSchemaVersion)
        #expect(StatusSnapshotEnvelope.currentSchemaVersion == 1)
    }

    @Test
    func payloadTopLevelKeys() throws {
        let envelope = StatusSnapshotEnvelope(fetchedAt: .now, status: makeStatus())

        let data = try makeEncoder().encode(envelope)
        let json = try #require(try JSONSerialization.jsonObject(with: data) as? [String: Any])

        #expect(json["schemaVersion"] != nil)
        #expect(json["fetchedAt"] != nil)
        #expect(json["status"] != nil)
    }

    @Test
    func fetchedAtSerialisesAsISO8601UTC() throws {
        let fetchedAt = Date(timeIntervalSince1970: 1_767_225_600) // 2026-01-01T00:00:00Z
        let envelope = StatusSnapshotEnvelope(fetchedAt: fetchedAt, status: makeStatus())

        let data = try makeEncoder().encode(envelope)
        let json = try #require(try JSONSerialization.jsonObject(with: data) as? [String: Any])
        let fetchedAtString = try #require(json["fetchedAt"] as? String)

        #expect(fetchedAtString == "2026-01-01T00:00:00Z")
    }

    @Test
    func decoderFailsOnMissingSchemaVersion() {
        let json = """
        {
          "fetchedAt": "2026-01-01T00:00:00Z",
          "status": {}
        }
        """

        #expect(throws: DecodingError.self) {
            try makeDecoder().decode(StatusSnapshotEnvelope.self, from: Data(json.utf8))
        }
    }
}
