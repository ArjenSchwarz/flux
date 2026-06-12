/// The one value that travels from view model to API client to chart-expansion
/// scope, so every consumer fetches the same history window (the data-consistency
/// rule). `Codable` is required, not optional: `ChartScope` carries this type
/// and is encoded for scene/expansion restoration.
public enum HistoryQuery: Hashable, Sendable, Codable {
    /// Existing anchored-to-today form: an inclusive day-count window ending on
    /// the server's Sydney today (may include a live today row).
    case days(Int)
    /// Explicit past-only window with inclusive `YYYY-MM-DD` bounds. The server
    /// rejects ranges that are not strictly before Sydney today (Decision 15).
    case dateRange(start: String, end: String)
}
