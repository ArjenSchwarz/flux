import Foundation
import Testing
@testable import FluxCore

@Suite
struct StalenessClassifierTests {
    private let epoch = Date(timeIntervalSince1970: 0)

    private func now(minutesAfter minutes: Double) -> Date {
        epoch.addingTimeInterval(minutes * 60)
    }

    @Test(arguments: [
        (0.0, Staleness.fresh),
        (44.0, Staleness.fresh),
        (45.0, Staleness.stale),
        (179.0, Staleness.stale),
        (180.0, Staleness.offline),
        (1000.0, Staleness.offline)
    ])
    func classifyProducesExpectedBucket(ageMinutes: Double, expected: Staleness) {
        let result = StalenessClassifier.classify(
            fetchedAt: epoch,
            now: now(minutesAfter: ageMinutes)
        )

        #expect(result == expected)
    }

    @Test
    func classifyBoundariesUseGreaterOrEqual() {
        let atFreshBoundary = epoch.addingTimeInterval(StalenessClassifier.freshThreshold)
        #expect(StalenessClassifier.classify(fetchedAt: epoch, now: atFreshBoundary) == .stale)

        let atOfflineBoundary = epoch.addingTimeInterval(StalenessClassifier.offlineThreshold)
        #expect(StalenessClassifier.classify(fetchedAt: epoch, now: atOfflineBoundary) == .offline)
    }

    @Test
    func nextTransitionBeforeFreshReturnsFreshBoundary() {
        let current = now(minutesAfter: 10)
        let expected = epoch.addingTimeInterval(StalenessClassifier.freshThreshold)

        let result = StalenessClassifier.nextTransition(fetchedAt: epoch, now: current)

        #expect(result == expected)
    }

    @Test
    func nextTransitionBetweenFreshAndOfflineReturnsOfflineBoundary() {
        let current = now(minutesAfter: 60)
        let expected = epoch.addingTimeInterval(StalenessClassifier.offlineThreshold)

        let result = StalenessClassifier.nextTransition(fetchedAt: epoch, now: current)

        #expect(result == expected)
    }

    @Test
    func nextTransitionAfterOfflineReturnsNil() {
        let current = now(minutesAfter: 500)

        let result = StalenessClassifier.nextTransition(fetchedAt: epoch, now: current)

        #expect(result == nil)
    }
}
