import Foundation
import Testing
@testable import FluxCore

@Suite
struct SimulationPresetTests {
    // MARK: - Decode / encode

    @Test
    func decodeFromBackendShape() throws {
        let json = """
        {
          "id": "preset-uuid",
          "label": "Charge car",
          "watts": 1700,
          "createdAt": "2026-06-09T08:00:00Z",
          "updatedAt": "2026-06-09T08:00:00Z"
        }
        """.data(using: .utf8)!

        let preset = try jsonDecoder().decode(SimulationPreset.self, from: json)
        #expect(preset.id == "preset-uuid")
        #expect(preset.label == "Charge car")
        #expect(preset.watts == 1700)
    }

    @Test
    func encodeRoundTrip() throws {
        let preset = SimulationPreset(
            id: "p1",
            label: "Heat pump",
            watts: 3200,
            createdAt: Date(timeIntervalSince1970: 1_715_000_000),
            updatedAt: Date(timeIntervalSince1970: 1_715_100_000)
        )
        let data = try jsonEncoder().encode(preset)
        let back = try jsonDecoder().decode(SimulationPreset.self, from: data)
        #expect(back.id == preset.id)
        #expect(back.label == preset.label)
        #expect(back.watts == preset.watts)
    }

    // MARK: - Draft validation (boundary cases — AC 1.3)

    @Test
    func draftDefaultStartsInvalid() {
        // Empty label + watts 0 so the editor forces a deliberate entry. The
        // empty-label check is reported first.
        let draft = SimulationPresetDraft()
        #expect(draft.validate() != nil)
        #expect(draft.validate() == .emptyLabel)
    }

    @Test
    func draftWithLabelButZeroWattsStartsInvalid() {
        // watts defaults to 0 so even with a label the draft is invalid until
        // a deliberate watt value is entered.
        let draft = SimulationPresetDraft(label: "Charge car")
        #expect(draft.validate() == .wattsOutOfRange)
    }

    @Test
    func draftValidWithLabelAndWatts() {
        let draft = SimulationPresetDraft(label: "Charge car", watts: 1700)
        #expect(draft.validate() == nil)
    }

    @Test
    func draftRejectsEmptyLabel() {
        let draft = SimulationPresetDraft(label: "", watts: 1700)
        #expect(draft.validate() == .emptyLabel)
    }

    @Test
    func draftRejectsLabelOf41Chars() {
        let draft = SimulationPresetDraft(label: String(repeating: "a", count: 41), watts: 1700)
        #expect(draft.validate() == .labelTooLong)
    }

    @Test
    func draftAcceptsLabelOf40Chars() {
        let draft = SimulationPresetDraft(label: String(repeating: "a", count: 40), watts: 1700)
        #expect(draft.validate() == nil)
    }

    @Test
    func draftRejectsZeroWatts() {
        let draft = SimulationPresetDraft(label: "Charge car", watts: 0)
        #expect(draft.validate() == .wattsOutOfRange)
    }

    @Test
    func draftRejectsWattsOf20001() {
        let draft = SimulationPresetDraft(label: "Charge car", watts: 20001)
        #expect(draft.validate() == .wattsOutOfRange)
    }

    @Test
    func draftAcceptsWattsOf20000() {
        let draft = SimulationPresetDraft(label: "Charge car", watts: 20000)
        #expect(draft.validate() == nil)
    }

    @Test
    func draftAcceptsWattsOf1() {
        let draft = SimulationPresetDraft(label: "Charge car", watts: 1)
        #expect(draft.validate() == nil)
    }

    // MARK: - Draft from preset

    @Test
    func draftSeededFromPresetCarriesWritableFields() {
        let preset = SimulationPreset(
            id: "p1",
            label: "Oven",
            watts: 2400,
            createdAt: Date(),
            updatedAt: Date()
        )
        let draft = SimulationPresetDraft(preset: preset)
        #expect(draft.label == "Oven")
        #expect(draft.watts == 2400)
    }

    // MARK: - helpers

    private func jsonEncoder() -> JSONEncoder {
        let enc = JSONEncoder()
        enc.dateEncodingStrategy = .iso8601
        return enc
    }

    private func jsonDecoder() -> JSONDecoder {
        let dec = JSONDecoder()
        dec.dateDecodingStrategy = .iso8601
        return dec
    }
}
