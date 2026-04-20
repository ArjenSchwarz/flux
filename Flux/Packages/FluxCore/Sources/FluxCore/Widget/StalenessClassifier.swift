import Foundation

public enum Staleness: Sendable, Equatable {
    case fresh
    case stale
    case offline
}

public enum StalenessClassifier {
    public static let freshThreshold: TimeInterval = 45 * 60
    public static let offlineThreshold: TimeInterval = 3 * 3600

    public static func classify(fetchedAt: Date, now: Date) -> Staleness {
        let age = now.timeIntervalSince(fetchedAt)
        if age >= offlineThreshold {
            return .offline
        }
        if age >= freshThreshold {
            return .stale
        }
        return .fresh
    }

    public static func nextTransition(fetchedAt: Date, now: Date) -> Date? {
        let freshBoundary = fetchedAt.addingTimeInterval(freshThreshold)
        let offlineBoundary = fetchedAt.addingTimeInterval(offlineThreshold)

        if now < freshBoundary {
            return freshBoundary
        }
        if now < offlineBoundary {
            return offlineBoundary
        }
        return nil
    }
}
