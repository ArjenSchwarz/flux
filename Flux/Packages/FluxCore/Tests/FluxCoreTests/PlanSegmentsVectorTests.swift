import Foundation
import Testing
@testable import FluxCore

/// Segmentation exists in both Go (`plan.Segments`) and Swift. The shared
/// vectors in `internal/api/testdata/pricing_segments.json` are the pin that
/// keeps the two identical — the `note_lengths.json` pattern.
@Suite
struct PlanSegmentsVectorTests {
    @Test
    func segmentationMatchesTheSharedVectors() throws {
        let vectors = try Self.loadVectors()
        #expect(!vectors.isEmpty)

        for vector in vectors {
            let segments = vector.plan.asPlan().segments
            #expect(
                segments.count == vector.segments.count,
                "\(vector.name): expected \(vector.segments.count) segments, got \(segments.count)"
            )
            guard segments.count == vector.segments.count else { continue }
            for (index, want) in vector.segments.enumerated() {
                let got = segments[index]
                #expect(got.start == want.start, "\(vector.name) segment \(index) start")
                #expect(got.end == want.end, "\(vector.name) segment \(index) end")
                #expect(got.free == want.free, "\(vector.name) segment \(index) free")
                #expect(got.rate == want.rate, "\(vector.name) segment \(index) rate")
            }
        }
    }

    @Test
    func segmentsAlwaysTileTheWholeDay() throws {
        for vector in try Self.loadVectors() {
            let segments = vector.plan.asPlan().segments
            #expect(segments.first?.start == "00:00", "\(vector.name) starts at midnight")
            #expect(segments.last?.end == "24:00", "\(vector.name) ends at end of day")
            for index in 1 ..< segments.count {
                #expect(
                    segments[index - 1].end == segments[index].start,
                    "\(vector.name) segment \(index) abuts its predecessor"
                )
            }
        }
    }

    @Test
    func ratedSegmentsDropTheFreeBandOnly() throws {
        for vector in try Self.loadVectors() {
            let plan = vector.plan.asPlan()
            #expect(plan.ratedSegments == plan.segments.filter { !$0.free }, "\(vector.name)")
        }
    }

    // MARK: - Vector fixtures

    private struct Vector: Decodable {
        let name: String
        let plan: VectorPlan
        let segments: [VectorSegment]
    }

    private struct VectorPlan: Decodable {
        let defaultRate: Double
        let windows: [PlanWindow]
        let feedInRate: Double
        let savingsReferenceRate: Double?

        func asPlan() -> PricingPlan {
            PricingPlan(
                id: "vector",
                startDate: "2026-01-01",
                endDate: nil,
                defaultRate: defaultRate,
                windows: windows,
                feedInRate: feedInRate,
                savingsReferenceRate: savingsReferenceRate,
                createdAt: Date(timeIntervalSince1970: 0),
                updatedAt: Date(timeIntervalSince1970: 0)
            )
        }
    }

    private struct VectorSegment: Decodable {
        let start: String
        let end: String
        let free: Bool
        let rate: Double
    }

    private static func loadVectors(file: String = #filePath) throws -> [Vector] {
        try JSONDecoder().decode([Vector].self, from: Data(contentsOf: vectorURL(named: "pricing_segments.json", file: file)))
    }
}

/// `#filePath` is `<repo>/Flux/Packages/FluxCore/Tests/FluxCoreTests/<file>.swift`;
/// walk up to the repo root so both Go and Swift read the same fixture file.
func vectorURL(named name: String, file: String) -> URL {
    URL(fileURLWithPath: file)
        .deletingLastPathComponent()  // FluxCoreTests/
        .deletingLastPathComponent()  // Tests/
        .deletingLastPathComponent()  // FluxCore/
        .deletingLastPathComponent()  // Packages/
        .deletingLastPathComponent()  // Flux/
        .deletingLastPathComponent()  // repo root
        .appendingPathComponent("internal/api/testdata/\(name)")
}
