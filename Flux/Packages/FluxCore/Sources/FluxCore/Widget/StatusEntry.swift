import Foundation
import WidgetKit

public struct StatusEntry: TimelineEntry, Sendable {
    public enum Source: Sendable, Equatable {
        case live
        case cache
        case placeholder
    }

    public let date: Date
    public let envelope: StatusSnapshotEnvelope?
    public let staleness: Staleness
    public let source: Source
    public let relevance: TimelineEntryRelevance?

    public init(
        date: Date,
        envelope: StatusSnapshotEnvelope?,
        staleness: Staleness,
        source: Source,
        relevance: TimelineEntryRelevance? = nil
    ) {
        self.date = date
        self.envelope = envelope
        self.staleness = staleness
        self.source = source
        self.relevance = relevance
    }
}
