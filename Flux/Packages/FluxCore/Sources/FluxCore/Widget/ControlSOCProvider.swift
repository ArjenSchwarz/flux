#if os(macOS)
import Foundation
import WidgetKit

public struct SOCValue: Sendable, Equatable {
    public let percent: Double
    public let stale: Bool

    public init(percent: Double, stale: Bool) {
        self.percent = percent
        self.stale = stale
    }
}

public struct ControlSOCProvider: ControlValueProvider {
    public typealias Value = SOCValue

    // Representative value for the widget gallery; an empty/stale battery
    // preview looks alarming and isn't a useful illustration of the widget.
    public let previewValue = SOCValue(percent: 80, stale: false)

    private let cache: WidgetSnapshotCache
    private let logic: StatusTimelineLogic
    private let nowProvider: @Sendable () -> Date
    private let cacheStalenessThreshold: TimeInterval

    public init(
        cache: WidgetSnapshotCache,
        logic: StatusTimelineLogic,
        nowProvider: @escaping @Sendable () -> Date = { Date() },
        cacheStalenessThreshold: TimeInterval = 600
    ) {
        self.cache = cache
        self.logic = logic
        self.nowProvider = nowProvider
        self.cacheStalenessThreshold = cacheStalenessThreshold
    }

    public func currentValue() async throws -> SOCValue {
        if let cached = cache.read(),
           nowProvider().timeIntervalSince(cached.fetchedAt) < cacheStalenessThreshold {
            return SOCValue(percent: cached.status.live?.soc ?? 0, stale: false)
        }
        let entry = await logic.snapshot(isPreview: false)
        let percent = entry.envelope?.status.live?.soc ?? 0
        return SOCValue(percent: percent, stale: entry.source != .live)
    }
}
#endif
