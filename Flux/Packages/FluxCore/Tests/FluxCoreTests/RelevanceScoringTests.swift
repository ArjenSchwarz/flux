import Foundation
import Testing
import WidgetKit
@testable import FluxCore

@Suite
struct RelevanceScoringTests {
    private func live(soc: Double, pbat: Double) -> LiveData {
        LiveData(
            ppv: 0,
            pload: 0,
            pbat: pbat,
            pgrid: 0,
            pgridSustained: false,
            soc: soc,
            timestamp: "2026-04-20T10:00:00Z"
        )
    }

    private func battery(cutoffPercent: Int) -> BatteryInfo {
        BatteryInfo(
            capacityKwh: 10,
            cutoffPercent: cutoffPercent,
            estimatedCutoffTime: nil,
            low24h: nil
        )
    }

    @Test
    func freshDischargingNearCutoffScoresHighest() {
        let result = RelevanceScoring.score(
            staleness: .fresh,
            live: live(soc: 14, pbat: 500),
            battery: battery(cutoffPercent: 10)
        )

        #expect(result.score == 0.9)
    }

    @Test
    func freshDischargingAtExactCutoffPlusFiveScoresHighest() {
        let result = RelevanceScoring.score(
            staleness: .fresh,
            live: live(soc: 15, pbat: 500),
            battery: battery(cutoffPercent: 10)
        )

        #expect(result.score == 0.9)
    }

    @Test
    func freshDischargingWithinTwentyPercentOfCutoffScoresElevated() {
        let result = RelevanceScoring.score(
            staleness: .fresh,
            live: live(soc: 25, pbat: 500),
            battery: battery(cutoffPercent: 10)
        )

        #expect(result.score == 0.7)
    }

    @Test
    func freshDischargingFarFromCutoffScoresBaseline() {
        let result = RelevanceScoring.score(
            staleness: .fresh,
            live: live(soc: 60, pbat: 500),
            battery: battery(cutoffPercent: 10)
        )

        #expect(result.score == 0.5)
    }

    @Test
    func freshChargingScoresBaseline() {
        let result = RelevanceScoring.score(
            staleness: .fresh,
            live: live(soc: 20, pbat: -500),
            battery: battery(cutoffPercent: 10)
        )

        #expect(result.score == 0.5)
    }

    @Test
    func freshIdleScoresBaseline() {
        let result = RelevanceScoring.score(
            staleness: .fresh,
            live: live(soc: 20, pbat: 0),
            battery: battery(cutoffPercent: 10)
        )

        #expect(result.score == 0.5)
    }

    @Test
    func freshWithoutBatteryInfoScoresBaseline() {
        let result = RelevanceScoring.score(
            staleness: .fresh,
            live: live(soc: 20, pbat: 500),
            battery: nil
        )

        #expect(result.score == 0.5)
    }

    @Test
    func staleScoresLow() {
        let result = RelevanceScoring.score(
            staleness: .stale,
            live: live(soc: 14, pbat: 500),
            battery: battery(cutoffPercent: 10)
        )

        #expect(result.score == 0.3)
    }

    @Test
    func offlineScoresLowest() {
        let result = RelevanceScoring.score(
            staleness: .offline,
            live: live(soc: 14, pbat: 500),
            battery: battery(cutoffPercent: 10)
        )

        #expect(result.score == 0.1)
    }

    @Test
    func placeholderNoLiveScoresLowest() {
        let result = RelevanceScoring.score(
            staleness: .fresh,
            live: nil,
            battery: nil
        )

        #expect(result.score == 0.1)
    }
}
