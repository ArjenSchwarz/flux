import Foundation
import Testing
@testable import FluxCore

/// Three-tier cost resolution exists in both Go (`plan.DayCosts`) and Swift.
/// `internal/api/testdata/pricing_costs.json` pins the two to identical
/// numbers, and its tier-2 rows are also the migration tool's golden formula —
/// so these vectors are the AC 5.2 proof on the client side.
@Suite
struct DayCostsVectorTests {
    @Test
    func costResolutionMatchesTheSharedVectors() throws {
        let vectors = try Self.loadVectors()
        #expect(!vectors.isEmpty)

        for vector in vectors {
            let costs = DayCosts.resolve(plan: vector.plan.asPlan(), day: vector.day.asInputs())
            #expect(costs.tier.rawValue == vector.expected.tier, "\(vector.name) tier")
            #expect(Self.close(costs.peakImportsCost, vector.expected.importCost), "\(vector.name) importCost")
            #expect(Self.close(costs.solarFeedInIncome, vector.expected.feedInIncome), "\(vector.name) feedInIncome")
            #expect(Self.close(costs.net, vector.expected.net), "\(vector.name) net")
            #expect(Self.close(costs.offPeakSavings, vector.expected.savings), "\(vector.name) savings")
        }
    }

    @Test
    func netIsAlwaysImportMinusFeedInAcrossEveryTier() throws {
        for vector in try Self.loadVectors() {
            let costs = DayCosts.resolve(plan: vector.plan.asPlan(), day: vector.day.asInputs())
            #expect(
                Self.close(costs.net, costs.peakImportsCost - costs.solarFeedInIncome),
                "\(vector.name) net invariant"
            )
        }
    }

    @Test
    func everyTierIsExercisedByTheVectors() throws {
        let tiers = Set(try Self.loadVectors().map(\.expected.tier))
        #expect(tiers == [1, 2, 3])
    }

    // MARK: - Vector fixtures

    private struct Vector: Decodable {
        let name: String
        let plan: VectorPlan
        let day: VectorDay
        let expected: VectorExpected
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

    private struct VectorOffpeak: Decodable {
        let gridImportKwh: Double
        let windowStart: String?
        let windowEnd: String?
        let integratedAt: String?
        let sampleCount: Int
    }

    private struct VectorDay: Decodable {
        let eInput: Double?
        let eOutput: Double?
        let peakGridImportKwh: Double?
        let offpeak: VectorOffpeak?
        let bandImports: [BandImport]?

        func asInputs() -> DayCostInputs {
            DayCostInputs(
                eInput: eInput,
                eOutput: eOutput,
                peakGridImportKwh: peakGridImportKwh,
                offpeak: offpeak.map {
                    OffpeakImport(
                        gridImportKwh: $0.gridImportKwh,
                        windowStart: $0.windowStart,
                        windowEnd: $0.windowEnd,
                        integratedAt: $0.integratedAt,
                        sampleCount: $0.sampleCount
                    )
                },
                bandImports: bandImports
            )
        }
    }

    private struct VectorExpected: Decodable {
        let tier: Int
        let importCost: Double
        let feedInIncome: Double
        let net: Double
        let savings: Double
    }

    private static func loadVectors(file: String = #filePath) throws -> [Vector] {
        try JSONDecoder().decode([Vector].self, from: Data(contentsOf: vectorURL(named: "pricing_costs.json", file: file)))
    }

    private static func close(_ lhs: Double, _ rhs: Double) -> Bool {
        abs(lhs - rhs) < 1e-9
    }
}
