import FluxCore
import SwiftUI

extension DailyUsageBlock.Kind {
    /// Single source of truth for AC 1.3 — the bottom-to-top stacking order
    /// in `HistoryDailyUsageCard` and the legend ordering both read this.
    static let chronologicalOrder: [DailyUsageBlock.Kind] =
        [.night, .morningPeak, .offPeak, .afternoonPeak, .evening]

    /// Position in `chronologicalOrder`. Drives sort + tie-break on
    /// largest-kind comparisons (AC 1.8). Force-unwrap is intentional —
    /// a future `Kind` case missing from `chronologicalOrder` should crash
    /// loudly rather than silently sort as 0.
    var chronologicalIndex: Int {
        Self.chronologicalOrder.firstIndex(of: self)!
    }

    /// Pinned palette per Decision 5. Lives in the iOS app target rather
    /// than FluxCore because `Color` is a SwiftUI symbol.
    var chartColor: Color {
        switch self {
        case .night: return .indigo
        case .morningPeak: return .orange
        case .offPeak: return .teal
        case .afternoonPeak: return .red
        case .evening: return .purple
        }
    }

    /// Shared with the Day Detail Daily Usage card so labels stay aligned
    /// across screens (AC 1.5).
    var displayLabel: String {
        switch self {
        case .night: return "Night"
        case .morningPeak: return "Morning Peak"
        case .offPeak: return "Off-Peak"
        case .afternoonPeak: return "Afternoon Peak"
        case .evening: return "Evening"
        }
    }
}
