import Foundation

/// The editable shape used by the pricing editor. The backend assigns id /
/// createdAt / updatedAt on POST, so the draft only carries the writable
/// fields. Server-side validation is authoritative; local validation is for
/// early editor feedback and mirrors the server rules so a plan accepted
/// locally is not rejected for a rule the client could have checked (AC 6.4).
public struct PricingPlanDraft: Codable, Sendable, Equatable {
    public var startDate: String
    /// Exclusive switch date (Decision 5); `nil` means open-ended.
    public var endDate: String?
    public var defaultRate: Double
    public var windows: [PlanWindow]
    public var feedInRate: Double
    public var savingsReferenceRate: Double?

    public init(
        startDate: String = "",
        endDate: String? = nil,
        defaultRate: Double = 0,
        windows: [PlanWindow] = [],
        feedInRate: Double = 0,
        savingsReferenceRate: Double? = nil
    ) {
        self.startDate = startDate
        self.endDate = endDate
        self.defaultRate = defaultRate
        self.windows = windows
        self.feedInRate = feedInRate
        self.savingsReferenceRate = savingsReferenceRate
    }

    public init(plan: PricingPlan) {
        self.startDate = plan.startDate
        self.endDate = plan.endDate
        self.defaultRate = plan.defaultRate
        self.windows = plan.windows
        self.feedInRate = plan.feedInRate
        self.savingsReferenceRate = plan.savingsReferenceRate
    }

    /// Reasons a draft can fail validation. Mirrors the backend error codes in
    /// `internal/plan`; `invalidStartDate` / `invalidEndDate` are the local
    /// split of the server's single `inverted_dates` code. Cross-plan rules
    /// (date-range overlap, the single-open-ended rule) run server-side only.
    public enum ValidationError: Error, Equatable, Sendable {
        case invalidStartDate
        case invalidEndDate
        case invertedDates
        case bandWindowInvalid
        case bandOverlap
        case multipleFreeBands
        case noRatedBand
        case savingsRateMissing
        case rateOutOfRange
        case ratePrecision
    }

    /// The per-rate upper bound carried over from the flat-rate model — 10× the
    /// highest plausible AU retail tariff, which catches order-of-magnitude
    /// typos without constraining real use.
    public static let rateCap = 10.0

    /// Local pre-flight validation. The server re-validates — this is purely
    /// for early editor feedback. Returns `nil` when valid. The rule order
    /// matches the server's so the first message the editor shows is the one
    /// the server would report first.
    public func validate() -> ValidationError? {
        if let dateError = validateDates() { return dateError }
        if let bandError = validateBands() { return bandError }
        return validateRates()
    }

    /// Returns the rate rounded to exactly four decimal places. The backend
    /// stores rates at 4dp; this helper keeps the wire payload consistent.
    public static func roundedToFourDP(_ rate: Double) -> Double {
        (rate * 10_000).rounded() / 10_000
    }

    /// The draft with every rate normalised to the backend's 4dp storage
    /// precision. A free window's stale rate is dropped rather than rounded —
    /// it carries no rate by contract.
    public func normalised() -> PricingPlanDraft {
        PricingPlanDraft(
            startDate: startDate,
            endDate: endDate,
            defaultRate: Self.roundedToFourDP(defaultRate),
            windows: windows.map { window in
                PlanWindow(
                    start: window.start,
                    end: window.end,
                    free: window.free,
                    rate: window.free ? nil : Self.roundedToFourDP(window.rate ?? 0)
                )
            },
            feedInRate: Self.roundedToFourDP(feedInRate),
            savingsReferenceRate: savingsReferenceRate.map(Self.roundedToFourDP)
        )
    }

    // MARK: - Rule groups

    private func validateDates() -> ValidationError? {
        guard Self.isValidDate(startDate) else { return .invalidStartDate }
        guard let endDate else { return nil }
        guard Self.isValidDate(endDate) else { return .invalidEndDate }
        // Exclusive ends make endDate == startDate a plan that prices no days.
        guard endDate > startDate else { return .invertedDates }
        return nil
    }

    private func validateBands() -> ValidationError? {
        var parsed: [ParsedBand] = []
        for window in windows {
            guard let band = ParsedBand(window) else { return .bandWindowInvalid }
            parsed.append(band)
        }

        let sorted = parsed.sorted { $0.start < $1.start }
        for index in 1 ..< max(sorted.count, 1) where sorted[index].start < sorted[index - 1].end {
            return .bandOverlap
        }

        let free = parsed.filter(\.free)
        if free.count > 1 { return .multipleFreeBands }
        // AC 1.3: at least one rated band. The only way to have none is a free
        // window covering the whole day — a zero-width default remainder left
        // by rated windows tiling the rest is fine.
        let freeMinutes = free.reduce(0) { $0 + ($1.end - $1.start) }
        if !free.isEmpty, freeMinutes >= PlanWindow.minutesPerDay { return .noRatedBand }
        if !free.isEmpty, savingsReferenceRate == nil { return .savingsRateMissing }
        return nil
    }

    private func validateRates() -> ValidationError? {
        var rates = [defaultRate, feedInRate]
        if let savingsReferenceRate { rates.append(savingsReferenceRate) }
        // A free window's rate is skipped — it carries no rate by contract.
        rates.append(contentsOf: windows.filter { !$0.free }.map { $0.rate ?? 0 })

        for rate in rates {
            if !Self.fitsFourDecimalPlaces(rate) { return .ratePrecision }
            if rate < 0 || rate > Self.rateCap { return .rateOutOfRange }
        }
        return nil
    }

    // MARK: - Primitives

    private static func isValidDate(_ value: String) -> Bool {
        // YYYY-MM-DD — strictly 10 characters with hyphen positions at 5 and 8.
        guard value.count == 10 else { return false }
        let chars = Array(value)
        guard chars[4] == "-", chars[7] == "-" else { return false }
        guard let year = Int(String(chars[0 ..< 4])),
              let month = Int(String(chars[5 ..< 7])),
              let day = Int(String(chars[8 ..< 10])) else { return false }
        guard year >= 1970, year <= 9999 else { return false }
        guard (1 ... 12).contains(month) else { return false }
        guard (1 ... 31).contains(day) else { return false }
        // Calendar-day check so 2026-02-30 fails client-side instead of
        // round-tripping to the server and surfacing as the misleading
        // "endDate must not precede startDate" message. Go's time.Parse on the
        // wire already enforces this; this keeps the pre-flight validator in
        // agreement with the authoritative server check.
        var calendar = Calendar(identifier: .iso8601)
        calendar.timeZone = TimeZone(identifier: "Australia/Melbourne") ?? .gmt
        let components = DateComponents(year: year, month: month, day: day)
        return calendar.date(from: components).map {
            let resolved = calendar.dateComponents([.year, .month, .day], from: $0)
            return resolved.year == year && resolved.month == month && resolved.day == day
        } ?? false
    }

    private static func fitsFourDecimalPlaces(_ rate: Double) -> Bool {
        // A rate fits 4dp if rate * 10_000 is (numerically) very close to an
        // integer. Float64 noise at 4dp is well below 1e-6, so this is safe.
        let scaled = rate * 10_000
        return abs(scaled - scaled.rounded()) < 1e-6
    }
}
