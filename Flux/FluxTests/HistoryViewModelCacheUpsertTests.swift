import FluxCore
import Foundation
import SwiftData
import Testing
@testable import Flux

@MainActor @Suite(.serialized)
struct HistoryViewModelCacheUpsertTests {
    @Test
    func cacheBackfillsDerivedFieldsWithoutWarnings() async throws {
        let modelContext = try makeModelContext()
        modelContext.insert(CachedDayEnergy(from: DayEnergy(
            date: "2026-04-14", epv: 5.2, eInput: 0.9, eOutput: 0.3, eCharge: 1.8, eDischarge: 2.7
        )))
        try modelContext.save()

        let apiClient = MockCacheUpsertAPIClient()
        let blocks = makeChronologicalBlocks(
            night: 1.0, morning: 1.0, offPeak: 2.0, afternoon: 2.0, evening: 1.0
        )
        let peakPeriods = [
            PeakPeriod(start: "2026-04-14T17:30:00Z", end: "2026-04-14T17:45:00Z", avgLoadW: 3000, energyWh: 750)
        ]
        apiClient.historyResult = .success(HistoryResponse(days: [
            DayEnergy(
                date: "2026-04-14", epv: 5.2, eInput: 0.9, eOutput: 0.3, eCharge: 1.8, eDischarge: 2.7,
                dailyUsage: DailyUsage(blocks: blocks),
                socLow: 22.5, socLowTime: "2026-04-14T03:30:00Z",
                peakPeriods: peakPeriods
            )
        ]))

        let sink = WarnSink()
        let now = makeUTCDate(year: 2026, month: 4, day: 15, hour: 14, minute: 30)
        let viewModel = HistoryViewModel(
            apiClient: apiClient,
            modelContext: modelContext,
            nowProvider: { now },
            warn: { line in Task { await sink.record(line) } }
        )
        await viewModel.loadHistory(days: 7)

        let cached = try modelContext.fetch(FetchDescriptor<CachedDayEnergy>())
        let row = try #require(cached.first)
        #expect(row.dailyUsage?.blocks.count == 5)
        #expect(row.socLow == 22.5)
        #expect(row.socLowTime == "2026-04-14T03:30:00Z")
        #expect(row.peakPeriods?.count == 1)

        try? await Task.sleep(for: .milliseconds(50))
        let lines = await sink.snapshot()
        #expect(lines.isEmpty, "no warning when backfilling previously-nil fields")
    }

    @Test
    func cacheClearsDerivedFieldsAndEmitsWarnings() async throws {
        let modelContext = try makeModelContext()
        let preload = CachedDayEnergy(from: DayEnergy(
            date: "2026-04-14", epv: 5.2, eInput: 0.9, eOutput: 0.3, eCharge: 1.8, eDischarge: 2.7,
            dailyUsage: DailyUsage(blocks: makeChronologicalBlocks(
                night: 1.0, morning: 1.0, offPeak: 1.0, afternoon: 1.0, evening: 1.0
            )),
            socLow: 18.0,
            socLowTime: "2026-04-14T04:00:00Z",
            peakPeriods: [
                PeakPeriod(start: "2026-04-14T17:30:00Z", end: "2026-04-14T17:45:00Z", avgLoadW: 3000, energyWh: 750)
            ]
        ))
        modelContext.insert(preload)
        try modelContext.save()

        let apiClient = MockCacheUpsertAPIClient()
        apiClient.historyResult = .success(HistoryResponse(days: [
            DayEnergy(date: "2026-04-14", epv: 5.2, eInput: 0.9, eOutput: 0.3, eCharge: 1.8, eDischarge: 2.7)
        ]))

        let sink = WarnSink()
        let now = makeUTCDate(year: 2026, month: 4, day: 15, hour: 14, minute: 30)
        let viewModel = HistoryViewModel(
            apiClient: apiClient,
            modelContext: modelContext,
            nowProvider: { now },
            warn: { line in Task { await sink.record(line) } }
        )
        await viewModel.loadHistory(days: 7)

        let cached = try modelContext.fetch(FetchDescriptor<CachedDayEnergy>())
        let row = try #require(cached.first)
        #expect(row.dailyUsage == nil)
        #expect(row.socLow == nil)
        #expect(row.socLowTime == nil)
        #expect(row.peakPeriods == nil)

        let lines = await sink.waitForLines(count: 4)
        #expect(lines.count == 4, "one warning per cleared field")
        for field in ["dailyUsage", "socLow", "socLowTime", "peakPeriods"] {
            #expect(lines.contains(where: { $0.contains("2026-04-14") && $0.contains(field) }),
                    "missing warning for \(field)")
        }
    }

    @Test
    func cacheRoundTripPreservesDailyUsageBlocks() async throws {
        let modelContext = try makeModelContext()
        let blocks = makeChronologicalBlocks(
            night: 1.5, morning: 0.5, offPeak: 2.5, afternoon: 1.0, evening: 1.5
        )
        let original = DayEnergy(
            date: "2026-04-14", epv: 5.0, eInput: 1.0, eOutput: 0.4, eCharge: 1.5, eDischarge: 2.1,
            dailyUsage: DailyUsage(blocks: blocks)
        )
        modelContext.insert(CachedDayEnergy(from: original))
        try modelContext.save()

        let cached = try modelContext.fetch(FetchDescriptor<CachedDayEnergy>())
        let restoredBlocks = try #require(cached.first?.asDayEnergy.dailyUsage?.blocks)
        #expect(restoredBlocks.count == blocks.count)
        for (lhs, rhs) in zip(restoredBlocks, blocks) {
            #expect(lhs.kind == rhs.kind)
            #expect(lhs.totalKwh == rhs.totalKwh)
            #expect(lhs.start == rhs.start)
            #expect(lhs.end == rhs.end)
            #expect(lhs.status == rhs.status)
            #expect(lhs.boundarySource == rhs.boundarySource)
        }
    }

    private func makeModelContext() throws -> ModelContext {
        let configuration = ModelConfiguration(isStoredInMemoryOnly: true)
        let container = try ModelContainer(for: CachedDayEnergy.self, configurations: configuration)
        return ModelContext(container)
    }

    private func makeUTCDate(year: Int, month: Int, day: Int, hour: Int, minute: Int) -> Date {
        let calendar = Calendar(identifier: .gregorian)
        return calendar.date(from: DateComponents(
            timeZone: TimeZone(secondsFromGMT: 0),
            year: year, month: month, day: day, hour: hour, minute: minute
        ))!
    }

    private func makeBlock(kind: DailyUsageBlock.Kind, totalKwh: Double) -> DailyUsageBlock {
        DailyUsageBlock(
            kind: kind,
            start: "2026-04-14T00:00:00Z",
            end: "2026-04-14T01:00:00Z",
            totalKwh: totalKwh,
            averageKwhPerHour: nil,
            percentOfDay: 0,
            status: .complete,
            boundarySource: .readings
        )
    }

    private func makeChronologicalBlocks(
        night: Double, morning: Double, offPeak: Double, afternoon: Double, evening: Double
    ) -> [DailyUsageBlock] {
        [
            makeBlock(kind: .night, totalKwh: night),
            makeBlock(kind: .morningPeak, totalKwh: morning),
            makeBlock(kind: .offPeak, totalKwh: offPeak),
            makeBlock(kind: .afternoonPeak, totalKwh: afternoon),
            makeBlock(kind: .evening, totalKwh: evening)
        ]
    }
}

private actor WarnSink {
    private var lines: [String] = []

    func record(_ line: String) {
        lines.append(line)
    }

    func snapshot() -> [String] {
        lines
    }

    /// Poll-with-timeout for closure-spawned `Task { await record(line) }`s
    /// to land. Avoids racing with unstructured task scheduling.
    func waitForLines(count: Int, timeoutMillis: Int = 500) async -> [String] {
        let deadline = Date.now.addingTimeInterval(Double(timeoutMillis) / 1000.0)
        while lines.count < count, Date.now < deadline {
            try? await Task.sleep(for: .milliseconds(10))
        }
        return lines
    }
}

private final class MockCacheUpsertAPIClient: FluxAPIClient, @unchecked Sendable {
    var historyResult: Result<HistoryResponse, Error> = .failure(FluxAPIError.notConfigured)

    func fetchStatus() async throws -> StatusResponse {
        throw FluxAPIError.notConfigured
    }

    func fetchHistory(days _: Int) async throws -> HistoryResponse {
        try historyResult.get()
    }

    func fetchDay(date _: String) async throws -> DayDetailResponse {
        throw FluxAPIError.notConfigured
    }

    func saveNote(date _: String, text _: String) async throws -> NoteResponse {
        throw FluxAPIError.notConfigured
    }
}
