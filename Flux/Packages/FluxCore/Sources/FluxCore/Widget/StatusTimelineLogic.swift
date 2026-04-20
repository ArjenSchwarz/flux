import Foundation
import WidgetKit

public struct StatusTimelineLogic: Sendable {
    public static let refreshInterval: TimeInterval = 30 * 60

    private let apiClient: (any FluxAPIClient)?
    private let cache: WidgetSnapshotCache
    private let tokenProvider: @Sendable () -> String?
    private let nowProvider: @Sendable () -> Date
    private let fetchTimeout: Duration
    private let migrator: @Sendable () async -> Void

    public init(
        apiClient: (any FluxAPIClient)?,
        cache: WidgetSnapshotCache,
        tokenProvider: @escaping @Sendable () -> String? = { KeychainService().loadToken() },
        nowProvider: @escaping @Sendable () -> Date = { Date() },
        fetchTimeout: Duration = .seconds(5),
        migrator: @escaping @Sendable () async -> Void = { _ = SettingsSuiteMigrator.run() }
    ) {
        self.apiClient = apiClient
        self.cache = cache
        self.tokenProvider = tokenProvider
        self.nowProvider = nowProvider
        self.fetchTimeout = fetchTimeout
        self.migrator = migrator
    }

    public func placeholder() -> StatusEntry {
        let now = nowProvider()
        let envelope = Self.placeholderEnvelope(now: now)
        return StatusEntry(
            date: now,
            envelope: envelope,
            staleness: .fresh,
            source: .placeholder,
            relevance: RelevanceScoring.score(
                staleness: .fresh,
                live: envelope.status.live,
                battery: envelope.status.battery
            )
        )
    }

    public func snapshot(isPreview: Bool) async -> StatusEntry {
        if isPreview { return placeholder() }
        let timeline = await timeline()
        return timeline.entries.first ?? placeholder()
    }

    public func timeline() async -> Timeline<StatusEntry> {
        await migrator()
        let now = nowProvider()
        let cached = cache.read()

        var liveEnvelope: StatusSnapshotEnvelope?
        if let token = tokenProvider(), !token.isEmpty, let client = apiClient {
            if let status = await fetchWithTimeout(client: client) {
                let envelope = StatusSnapshotEnvelope(fetchedAt: now, status: status)
                cache.writeIfNewer(envelope)
                liveEnvelope = envelope
            }
        }

        let envelope: StatusSnapshotEnvelope?
        let source: StatusEntry.Source
        if let live = liveEnvelope {
            envelope = live
            source = .live
        } else if let cached {
            envelope = cached
            source = .cache
        } else {
            envelope = nil
            source = .placeholder
        }

        let entries = makeEntries(envelope: envelope, source: source, now: now)
        return Timeline(
            entries: entries,
            policy: .after(now.addingTimeInterval(Self.refreshInterval))
        )
    }

    private func fetchWithTimeout(client: any FluxAPIClient) async -> StatusResponse? {
        do {
            return try await withThrowingTaskGroup(of: StatusResponse.self) { group in
                group.addTask {
                    try await client.fetchStatus()
                }
                group.addTask { [fetchTimeout] in
                    try await Task.sleep(for: fetchTimeout)
                    throw FluxAPIError.networkError("timeout")
                }
                defer { group.cancelAll() }
                guard let first = try await group.next() else {
                    throw FluxAPIError.networkError("no result")
                }
                return first
            }
        } catch {
            return nil
        }
    }

    private func makeEntries(
        envelope: StatusSnapshotEnvelope?,
        source: StatusEntry.Source,
        now: Date
    ) -> [StatusEntry] {
        guard let env = envelope else {
            return [
                StatusEntry(
                    date: now,
                    envelope: nil,
                    staleness: .offline,
                    source: .placeholder,
                    relevance: RelevanceScoring.score(
                        staleness: .offline,
                        live: nil,
                        battery: nil
                    )
                )
            ]
        }

        let freshBoundary = env.fetchedAt.addingTimeInterval(StalenessClassifier.freshThreshold)
        let offlineBoundary = env.fetchedAt.addingTimeInterval(StalenessClassifier.offlineThreshold)

        var dates: [Date] = [now]
        if now < freshBoundary { dates.append(freshBoundary) }
        if now < offlineBoundary { dates.append(offlineBoundary) }

        return dates.map { date in
            let staleness = StalenessClassifier.classify(fetchedAt: env.fetchedAt, now: date)
            return StatusEntry(
                date: date,
                envelope: env,
                staleness: staleness,
                source: source,
                relevance: RelevanceScoring.score(
                    staleness: staleness,
                    live: env.status.live,
                    battery: env.status.battery
                )
            )
        }
    }

    public static func validateWidgetSessionTimeouts(_ config: URLSessionConfiguration) -> Bool {
        config.timeoutIntervalForRequest == 5 && config.timeoutIntervalForResource == 5
    }

    public static func placeholderEnvelope(now: Date) -> StatusSnapshotEnvelope {
        let formatter = ISO8601DateFormatter()
        let live = LiveData(
            ppv: 1800,
            pload: 412,
            pbat: 500,
            pgrid: 210,
            pgridSustained: false,
            soc: 68,
            timestamp: formatter.string(from: now)
        )
        let battery = BatteryInfo(
            capacityKwh: 10,
            cutoffPercent: 10,
            estimatedCutoffTime: nil,
            low24h: nil
        )
        let rolling = RollingAvg(
            avgLoad: 400,
            avgPbat: 500,
            estimatedCutoffTime: nil
        )
        return StatusSnapshotEnvelope(
            fetchedAt: now,
            status: StatusResponse(
                live: live,
                battery: battery,
                rolling15min: rolling,
                offpeak: nil,
                todayEnergy: nil
            )
        )
    }
}
