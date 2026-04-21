import Foundation
import Testing
import WidgetKit
@testable import FluxCore

@Suite
struct WidgetAccessibilityTests {
    private func status(soc: Double, pbat: Double, ppv: Double = 1800, pload: Double = 412, pgrid: Double = 210) -> StatusResponse {
        StatusResponse(
            live: LiveData(
                ppv: ppv,
                pload: pload,
                pbat: pbat,
                pgrid: pgrid,
                pgridSustained: false,
                soc: soc,
                timestamp: "2026-04-20T10:00:00Z"
            ),
            battery: BatteryInfo(
                capacityKwh: 10,
                cutoffPercent: 10,
                estimatedCutoffTime: "2026-04-20T17:12:00Z",
                low24h: nil
            ),
            rolling15min: RollingAvg(
                avgLoad: 400,
                avgPbat: 500,
                estimatedCutoffTime: "2026-04-20T17:12:00Z"
            ),
            offpeak: nil,
            todayEnergy: nil
        )
    }

    private func entry(
        staleness: Staleness = .fresh,
        source: StatusEntry.Source = .live,
        envelope: StatusSnapshotEnvelope? = nil
    ) -> StatusEntry {
        StatusEntry(
            date: Date(timeIntervalSince1970: 0),
            envelope: envelope,
            staleness: staleness,
            source: source
        )
    }

    private func freshDischargeEnvelope(soc: Double = 73.4) -> StatusSnapshotEnvelope {
        StatusSnapshotEnvelope(
            fetchedAt: Date(timeIntervalSince1970: 0),
            status: status(soc: soc, pbat: 2100)
        )
    }

    @Test
    func systemMediumContainsSOCDischargeVerbAndPowerTrio() {
        let env = freshDischargeEnvelope()
        let label = WidgetAccessibility.label(for: entry(envelope: env), family: .systemMedium)

        #expect(label.contains("73"))
        #expect(label.lowercased().contains("discharging"))
        #expect(label.lowercased().contains("solar"))
        #expect(label.lowercased().contains("load"))
        #expect(label.lowercased().contains("grid"))
    }

    @Test
    func systemLargeIncludesPowerTrio() {
        let env = freshDischargeEnvelope()
        let label = WidgetAccessibility.label(for: entry(envelope: env), family: .systemLarge)

        #expect(label.lowercased().contains("solar"))
        #expect(label.lowercased().contains("load"))
        #expect(label.lowercased().contains("grid"))
    }

    @Test
    func systemSmallDoesNotListPowerTrioColumns() {
        let env = freshDischargeEnvelope()
        let label = WidgetAccessibility.label(for: entry(envelope: env), family: .systemSmall)

        #expect(label.contains("73"))
        #expect(label.lowercased().contains("discharging"))
        #expect(!label.lowercased().contains("solar"))
    }

    @Test
    func accessoryInlineIsSingleSentenceWithSOCAndStatusWord() {
        let env = freshDischargeEnvelope()
        let label = WidgetAccessibility.label(for: entry(envelope: env), family: .accessoryInline)

        #expect(label.contains("73"))
        #expect(label.lowercased().contains("discharging"))
        let sentenceCount = label.split { $0 == "." }.count
        #expect(sentenceCount == 1)
    }

    @Test
    func offlineLabelBeginsWithOfflineAcrossFamilies() {
        let env = freshDischargeEnvelope()
        let families: [WidgetFamily] = [
            .systemSmall,
            .systemMedium,
            .systemLarge,
            .accessoryCircular,
            .accessoryRectangular,
            .accessoryInline
        ]

        for family in families {
            let label = WidgetAccessibility.label(
                for: entry(staleness: .offline, envelope: env),
                family: family
            )
            #expect(label.hasPrefix("Offline."), "family=\(family) label=\(label)")
        }
    }

    @Test
    func placeholderEntryProducesGenericUnavailableLabel() {
        let label = WidgetAccessibility.label(
            for: entry(source: .placeholder, envelope: nil),
            family: .systemMedium
        )

        #expect(label == "Flux battery data unavailable")
    }

    @Test
    func placeholderLabelDoesNotIncludeSOC() {
        let label = WidgetAccessibility.label(
            for: entry(source: .placeholder, envelope: nil),
            family: .systemMedium
        )

        #expect(!label.contains("%"))
        #expect(!label.contains("0"))
    }

    @Test
    func labelBeginsWithBatteryPercentWhenEnvelopePresent() {
        let env = freshDischargeEnvelope(soc: 42.0)
        let families: [WidgetFamily] = [
            .systemSmall,
            .systemMedium,
            .systemLarge,
            .accessoryCircular,
            .accessoryRectangular,
            .accessoryInline
        ]

        for family in families {
            let label = WidgetAccessibility.label(
                for: entry(envelope: env),
                family: family
            )
            let firstNumber = label.first { $0.isNumber }
            #expect(firstNumber == "4", "family=\(family) label=\(label)")
        }
    }

    @Test
    func chargingVerbAppearsWhenPbatNegative() {
        let env = StatusSnapshotEnvelope(
            fetchedAt: Date(timeIntervalSince1970: 0),
            status: status(soc: 50, pbat: -500)
        )
        let label = WidgetAccessibility.label(for: entry(envelope: env), family: .accessoryInline)

        #expect(label.lowercased().contains("charging"))
        #expect(!label.lowercased().contains("discharging"))
    }

    @Test
    func idleVerbAppearsWhenPbatZero() {
        let env = StatusSnapshotEnvelope(
            fetchedAt: Date(timeIntervalSince1970: 0),
            status: status(soc: 50, pbat: 0)
        )
        let label = WidgetAccessibility.label(for: entry(envelope: env), family: .accessoryInline)

        #expect(label.lowercased().contains("idle"))
    }

    @Test
    func fullVerbAppearsAtHundredPercentCharging() {
        let env = StatusSnapshotEnvelope(
            fetchedAt: Date(timeIntervalSince1970: 0),
            status: status(soc: 100, pbat: -10)
        )
        let label = WidgetAccessibility.label(for: entry(envelope: env), family: .accessoryInline)

        #expect(label.lowercased().contains("full"))
    }
}
