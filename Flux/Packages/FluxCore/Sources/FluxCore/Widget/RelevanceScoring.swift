import Foundation
import WidgetKit

public enum RelevanceScoring {
    public static func score(
        staleness: Staleness,
        live: LiveData?,
        battery: BatteryInfo?
    ) -> TimelineEntryRelevance {
        guard let live else {
            return TimelineEntryRelevance(score: 0.1)
        }

        switch staleness {
        case .offline:
            return TimelineEntryRelevance(score: 0.1)
        case .stale:
            return TimelineEntryRelevance(score: 0.3)
        case .fresh:
            guard live.pbat > 0, let battery else {
                return TimelineEntryRelevance(score: 0.5)
            }
            let cutoff = Double(battery.cutoffPercent)
            if live.soc <= cutoff + 5 {
                return TimelineEntryRelevance(score: 0.9)
            }
            if live.soc <= cutoff + 20 {
                return TimelineEntryRelevance(score: 0.7)
            }
            return TimelineEntryRelevance(score: 0.5)
        }
    }
}
