import Foundation
import Testing
@testable import FluxCore

@Suite
struct WhatsNewCoordinatorTests {
    private let referenceDate = Date(timeIntervalSince1970: 0)

    private func release(
        _ version: String,
        highlights: [Highlight] = [.init(category: .new, title: "x")]
    ) -> WhatsNewRelease {
        WhatsNewRelease(version: version, date: referenceDate, highlights: highlights)
    }

    private func installed(_ string: String) -> WhatsNewVersion {
        WhatsNewVersion(string)!
    }

    // MARK: - autoDecision decision table

    @Test
    func freshInstallReturnsSilentSetInstalled() {
        let coord = WhatsNewCoordinator(
            catalogue: [release("1.1")],
            installed: installed("1.1"),
            lastSeen: nil,
            hasAnyFluxPref: false
        )
        #expect(coord.autoDecision() == .silentSet(version: "1.1"))
    }

    @Test
    func preFeatureUpgradeWithEntryPresents() {
        let r11 = release("1.1")
        let coord = WhatsNewCoordinator(
            catalogue: [r11],
            installed: installed("1.1"),
            lastSeen: nil,
            hasAnyFluxPref: true
        )
        #expect(coord.autoDecision() == .present(releases: [r11]))
    }

    @Test
    func preFeatureUpgradeWithOnlyFutureEntrySilentSets() {
        let coord = WhatsNewCoordinator(
            catalogue: [release("2.0")],
            installed: installed("1.1"),
            lastSeen: nil,
            hasAnyFluxPref: true
        )
        #expect(coord.autoDecision() == .silentSet(version: "1.1"))
    }

    @Test
    func preFeatureUpgradeAtSeedVersionSkips() {
        // installed == "1.0" (the seed) means no progress past last-seen.
        let coord = WhatsNewCoordinator(
            catalogue: [release("1.1")],
            installed: installed("1.0"),
            lastSeen: nil,
            hasAnyFluxPref: true
        )
        #expect(coord.autoDecision() == .skip)
    }

    @Test
    func normalUpgradeWithStackedEntriesPresentsNewestFirst() {
        let r12 = release("1.2")
        let r13 = release("1.3")
        let coord = WhatsNewCoordinator(
            catalogue: [r12, r13],
            installed: installed("1.3"),
            lastSeen: "1.1",
            hasAnyFluxPref: true
        )
        #expect(coord.autoDecision() == .present(releases: [r13, r12]))
    }

    @Test
    func sameVersionSkipsAndDoesNotWrite() {
        let coord = WhatsNewCoordinator(
            catalogue: [release("1.1")],
            installed: installed("1.1"),
            lastSeen: "1.1",
            hasAnyFluxPref: true
        )
        #expect(coord.autoDecision() == .skip)
    }

    @Test
    func downgradeWithInRangeEntryStillSkips() {
        // installed=1.1 < lastSeen=1.2. The 1.1 entry would otherwise satisfy
        // version > effective(1.0) and version <= installed, but the
        // installed > effective guard exits early before the range filter runs.
        let r10 = release("1.0")
        let r11 = release("1.1")
        let coord = WhatsNewCoordinator(
            catalogue: [r10, r11],
            installed: installed("1.1"),
            lastSeen: "1.2",
            hasAnyFluxPref: true
        )
        #expect(coord.autoDecision() == .skip)
    }

    @Test
    func downgradeReturnsSkip() {
        let coord = WhatsNewCoordinator(
            catalogue: [release("1.1")],
            installed: installed("1.0"),
            lastSeen: "1.1",
            hasAnyFluxPref: true
        )
        #expect(coord.autoDecision() == .skip)
    }

    @Test
    func emptyHighlightRangeSilentSetsToInstalled() {
        let coord = WhatsNewCoordinator(
            catalogue: [release("1.2", highlights: [])],
            installed: installed("1.2"),
            lastSeen: "1.1",
            hasAnyFluxPref: true
        )
        #expect(coord.autoDecision() == .silentSet(version: "1.2"))
    }

    @Test
    func futureVersionEntryFiltered() {
        let r12 = release("1.2")
        let r20 = release("2.0")
        let coord = WhatsNewCoordinator(
            catalogue: [r12, r20],
            installed: installed("1.2"),
            lastSeen: "1.1",
            hasAnyFluxPref: true
        )
        #expect(coord.autoDecision() == .present(releases: [r12]))
    }

    @Test
    func presentDoesNotWriteLastSeen() {
        // The coordinator is a pure value type — a regression guard that
        // .present does not touch persistence is implicitly enforced by the
        // public API: there is no UserDefaults dependency at all.
        let r11 = release("1.1")
        let coord = WhatsNewCoordinator(
            catalogue: [r11],
            installed: installed("1.1"),
            lastSeen: nil,
            hasAnyFluxPref: true
        )
        let decision = coord.autoDecision()
        // Calling autoDecision a second time yields the same result
        // (no internal state mutated).
        #expect(decision == coord.autoDecision())
        #expect(decision == .present(releases: [r11]))
    }

    // MARK: - manualLatest()

    @Test
    func manualLatestReturnsNewestEntryWithHighlights() {
        let r11 = release("1.1")
        let r12 = release("1.2")
        let coord = WhatsNewCoordinator(
            catalogue: [r11, r12],
            installed: installed("1.2"),
            lastSeen: "1.2",
            hasAnyFluxPref: true
        )
        #expect(coord.manualLatest() == r12)
    }

    @Test
    func manualLatestSkipsEntryWithNoHighlights() {
        let r11 = release("1.1")
        let r12Empty = release("1.2", highlights: [])
        let coord = WhatsNewCoordinator(
            catalogue: [r11, r12Empty],
            installed: installed("1.2"),
            lastSeen: "1.2",
            hasAnyFluxPref: true
        )
        #expect(coord.manualLatest() == r11)
    }

    @Test
    func manualLatestNilForEmptyCatalogue() {
        let coord = WhatsNewCoordinator(
            catalogue: [],
            installed: installed("1.1"),
            lastSeen: nil,
            hasAnyFluxPref: false
        )
        #expect(coord.manualLatest() == nil)
    }

    @Test
    func manualLatestIndependentOfLastSeen() {
        // Fresh install: lastSeen=nil, hasAnyFluxPref=false. manualLatest still
        // returns the latest-eligible release.
        let r11 = release("1.1")
        let coord = WhatsNewCoordinator(
            catalogue: [r11],
            installed: installed("1.1"),
            lastSeen: nil,
            hasAnyFluxPref: false
        )
        #expect(coord.manualLatest() == r11)
    }

    @Test
    func manualLatestFiltersFutureVersions() {
        let r11 = release("1.1")
        let r20 = release("2.0")
        let coord = WhatsNewCoordinator(
            catalogue: [r11, r20],
            installed: installed("1.1"),
            lastSeen: nil,
            hasAnyFluxPref: false
        )
        #expect(coord.manualLatest() == r11)
    }

    @Test
    func unparseableCatalogueVersionFiltered() {
        let r11 = release("1.1")
        let bad = release("1.x")
        let coord = WhatsNewCoordinator(
            catalogue: [bad, r11],
            installed: installed("1.1"),
            lastSeen: "1.0",
            hasAnyFluxPref: true
        )
        #expect(coord.autoDecision() == .present(releases: [r11]))
    }
}
