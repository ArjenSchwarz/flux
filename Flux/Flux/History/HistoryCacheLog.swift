import Foundation
import os

/// Pinned subsystem/category for cache-upsert observability per
/// `history-daily-usage` AC 4.2 — tests intercept via the injected `warn`
/// closure on `HistoryViewModel.init`; operators filter on these strings in
/// Console.app.
enum HistoryCacheLog {
    static let logger = Logger(subsystem: "eu.arjen.flux", category: "history-cache")

    static let defaultWarn: (String) -> Void = { message in
        logger.warning("\(message, privacy: .public)")
    }
}
