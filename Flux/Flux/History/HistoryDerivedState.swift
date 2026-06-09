import FluxCore
import Foundation

// The period-accumulator logic (DayRecordValue + Totals) is kept in one file
// alongside the derivations it serves; splitting it only to fit would fragment
// tightly-coupled code with intentionally file-private types.
// swiftlint:disable file_length

extension HistoryViewModel {
    struct SolarEntry: Identifiable, Equatable {
        let date: Date
        let dayID: String
        let kwh: Double
        let isToday: Bool

        var id: String { dayID }
    }

    struct GridEntry: Identifiable, Equatable {
        let date: Date
        let dayID: String
        let peakImportKwh: Double
        let offpeakImportKwh: Double
        let exportKwh: Double
        let isToday: Bool

        var id: String { dayID }
    }

    struct BatteryEntry: Identifiable, Equatable {
        let date: Date
        let dayID: String
        let chargeKwh: Double
        let dischargeKwh: Double
        let isToday: Bool

        var id: String { dayID }
    }

    struct DailyUsageEntryBlock: Equatable {
        let kind: DailyUsageBlock.Kind
        /// Clamped to ≥ 0 per AC 1.13.
        let totalKwh: Double
    }

    struct DailyUsageEntry: Identifiable, Equatable {
        let date: Date
        let dayID: String
        /// Sorted into `DailyUsageBlock.Kind.chronologicalOrder`.
        let blocks: [DailyUsageEntryBlock]
        /// Sum of clamped block kWh.
        let stackedTotalKwh: Double
        let isToday: Bool

        var id: String { dayID }

        /// `{date}: {stackedTotal} kWh, {largestKindForThisDay} largest`. Used
        /// for the chart's per-day VoiceOver element (AC 1.12).
        var accessibilitySummary: String {
            let total = HistoryFormatters.kwh(stackedTotalKwh)
            let dateText = DateFormatting.dayDateString(from: date)
            let largest = DailyUsageBlock.Kind.largest(
                among: blocks.lazy.map { (kind: $0.kind, value: $0.totalKwh) }
            )
            guard let largest else {
                return "\(dateText): \(total)"
            }
            return "\(dateText): \(total), \(largest.displayLabel) largest"
        }
    }

    struct DayKwhRecord: Equatable {
        let dayID: String
        let date: Date
        let kwh: Double
    }

    struct LowestSocRecord: Equatable {
        let dayID: String
        let date: Date
        /// Raw `Double` so display rounding can't perturb the tie-break (Decision 10).
        let soc: Double
        /// `"HH:mm:ss"` Sydney; nil when the payload had no time.
        let socLowTime: String?
    }

    struct PeriodSummary: Equatable {
        // Period totals and day-records are hard numbers, so they include
        // today's partial value — the rule the grid aggregates and Lowest SoC
        // already followed (Decision 15 supersedes the "exclude today" half of
        // Decision 5). Per-day *averages* still exclude today (a partial day
        // would skew them): each divides a complete-days-only total
        // (`…CompleteTotalKwh`) by a complete-days count.

        /// Sum of `epv` across every day in range, today included.
        let solarTotalKwh: Double
        /// Sum of `epv` across complete days only. Numerator for
        /// `solarPerDayKwh` so the average is not skewed by today's partial day.
        let solarCompleteTotalKwh: Double
        /// Complete days only (today excluded). Denominator for
        /// `solarPerDayKwh`; `batteryDayCount` follows the same rule.
        let solarDayCount: Int
        /// All days in range, today included. Gates the Total solar tile so a
        /// today-only range still shows a number rather than an em-dash.
        let dayCount: Int
        let peakImportTotalKwh: Double
        let offpeakImportTotalKwh: Double
        let exportTotalKwh: Double
        /// Includes today when an off-peak record exists. Off-peak imports
        /// are the actionable headline so showing today's in-progress
        /// number matters more than a clean daily average.
        let gridDayCount: Int
        /// Sum of `eCharge` across every day in range, today included. There is
        /// deliberately no `chargeCompleteTotalKwh` counterpart: no per-day
        /// charge average is derived, so don't add one without a consumer.
        let chargeTotalKwh: Double
        /// Sum of `eDischarge` across every day in range, today included.
        let dischargeTotalKwh: Double
        /// Sum of `eDischarge` across complete days only. Numerator for
        /// `dischargePerDayKwh`.
        let dischargeCompleteTotalKwh: Double
        let batteryDayCount: Int
        /// Sum of clamped stacked totals across every day-with-blocks, today
        /// included.
        let dailyUsageTotalKwh: Double
        /// Sum of clamped stacked totals across complete days-with-blocks only.
        /// Numerator for `dailyUsageAvgKwh`.
        let dailyUsageCompleteTotalKwh: Double
        /// Number of complete days-with-blocks. Denominator for the daily-usage
        /// averages (`dailyUsageAvgKwh`, `dailyUsageLargestKindAvgKwh`).
        let dailyUsageDayCount: Int
        /// Number of days-with-blocks including today. Gates the Total usage
        /// tile and the Daily usage chart placeholder so today's breakdown is
        /// shown when it is the only day with data.
        let dailyUsageDisplayDayCount: Int
        /// Largest contributing block kind across complete days-with-blocks,
        /// with ties broken by chronological order (AC 1.8). `nil` when no
        /// complete day-with-blocks exists in the range.
        let dailyUsageLargestKind: DailyUsageBlock.Kind?
        /// Sum of clamped `totalKwh` across complete days for
        /// `dailyUsageLargestKind`. Used by the card's subtitle to express
        /// the per-day average of the leading block kind.
        let dailyUsageLargestKindTotalKwh: Double
        /// Sum of post-clamp `night`-block kWh across complete days that
        /// contributed a night block. Divisor for `nightAvgKwh` is
        /// `nightBlockDayCount`.
        let nightTotalKwh: Double
        let nightBlockDayCount: Int
        /// Best day-with-blocks (max stacked kWh, ties broken by most recent).
        /// Today included — a record is a hard number.
        let mostUsageDay: DayKwhRecord?
        /// Best day by `epv` (max, ties broken by most recent). Today included.
        let mostSolarDay: DayKwhRecord?
        /// Day-with-low whose raw `socLow` is the minimum; ties broken by
        /// most recent. Today included.
        let lowestSocDay: LowestSocRecord?

        static let empty = PeriodSummary(
            solarTotalKwh: 0,
            solarCompleteTotalKwh: 0,
            solarDayCount: 0,
            dayCount: 0,
            peakImportTotalKwh: 0,
            offpeakImportTotalKwh: 0,
            exportTotalKwh: 0,
            gridDayCount: 0,
            chargeTotalKwh: 0,
            dischargeTotalKwh: 0,
            dischargeCompleteTotalKwh: 0,
            batteryDayCount: 0,
            dailyUsageTotalKwh: 0,
            dailyUsageCompleteTotalKwh: 0,
            dailyUsageDayCount: 0,
            dailyUsageDisplayDayCount: 0,
            dailyUsageLargestKind: nil,
            dailyUsageLargestKindTotalKwh: 0,
            nightTotalKwh: 0,
            nightBlockDayCount: 0,
            mostUsageDay: nil,
            mostSolarDay: nil,
            lowestSocDay: nil
        )

        var solarPerDayKwh: Double? {
            solarDayCount > 0 ? solarCompleteTotalKwh / Double(solarDayCount) : nil
        }

        var dischargePerDayKwh: Double? {
            batteryDayCount > 0 ? dischargeCompleteTotalKwh / Double(batteryDayCount) : nil
        }

        var dailyUsageAvgKwh: Double? {
            dailyUsageDayCount > 0 ? dailyUsageCompleteTotalKwh / Double(dailyUsageDayCount) : nil
        }

        /// Denominator is `dailyUsageDayCount` (all complete days), not the
        /// subset that contained the largest kind. Keeping it consistent with
        /// `dailyUsageAvgKwh` lets the subtitle compare like-for-like with the
        /// KPI: both averages are over the same cohort.
        var dailyUsageLargestKindAvgKwh: Double? {
            dailyUsageDayCount > 0 ? dailyUsageLargestKindTotalKwh / Double(dailyUsageDayCount) : nil
        }

        var nightAvgKwh: Double? {
            nightBlockDayCount > 0 ? nightTotalKwh / Double(nightBlockDayCount) : nil
        }
    }

    struct DerivedState {
        let solar: [SolarEntry]
        let grid: [GridEntry]
        let battery: [BatteryEntry]
        let dailyUsage: [DailyUsageEntry]
        let summary: PeriodSummary

        init(days: [DayEnergy], now: Date) {
            guard !days.isEmpty else {
                solar = []
                grid = []
                battery = []
                dailyUsage = []
                summary = .empty
                return
            }

            var solar: [SolarEntry] = []
            var grid: [GridEntry] = []
            var battery: [BatteryEntry] = []
            var dailyUsage: [DailyUsageEntry] = []
            var totals = Totals()
            solar.reserveCapacity(days.count)
            grid.reserveCapacity(days.count)
            battery.reserveCapacity(days.count)
            dailyUsage.reserveCapacity(days.count)

            for day in days {
                guard let parsedDate = DateFormatting.parseDayDate(day.date) else { continue }
                let isToday = DateFormatting.isToday(day.date, now: now)
                solar.append(SolarEntry(date: parsedDate, dayID: day.date, kwh: day.epv, isToday: isToday))
                battery.append(BatteryEntry(
                    date: parsedDate, dayID: day.date,
                    chargeKwh: day.eCharge, dischargeKwh: day.eDischarge, isToday: isToday
                ))
                if let entry = Self.gridEntry(day: day, parsedDate: parsedDate, isToday: isToday) {
                    grid.append(entry)
                    totals.addGrid(entry)
                }
                let usageEntry = Self.dailyUsageEntry(day: day, parsedDate: parsedDate, isToday: isToday)
                if let usageEntry {
                    dailyUsage.append(usageEntry)
                }
                totals.addDay(
                    day,
                    parsedDate: parsedDate,
                    isToday: isToday,
                    dailyUsageEntry: usageEntry
                )
            }

            self.solar = solar
            self.grid = grid
            self.battery = battery
            self.dailyUsage = dailyUsage
            self.summary = totals.snapshot
        }

        private static func gridEntry(day: DayEnergy, parsedDate: Date, isToday: Bool) -> GridEntry? {
            // Prefer the server peak (today's live value or a stored past day) so
            // the split renders even before the off-peak window opens, where
            // off-peak is genuinely 0, not missing (T-1420). Else use the
            // eInput − off-peak residual; else omit (no split info — never imply
            // a misleading off-peak 0).
            let peak: Double
            let offpeak: Double
            if let serverPeak = day.peakGridImportKwh {
                peak = serverPeak
                offpeak = day.offpeakGridImportKwh ?? 0
            } else if let offpeakImport = day.offpeakGridImportKwh {
                peak = max(0, day.eInput - offpeakImport)
                offpeak = offpeakImport
            } else {
                return nil
            }
            return GridEntry(
                date: parsedDate,
                dayID: day.date,
                peakImportKwh: peak,
                offpeakImportKwh: offpeak,
                exportKwh: day.eOutput,
                isToday: isToday
            )
        }

        private static func dailyUsageEntry(
            day: DayEnergy,
            parsedDate: Date,
            isToday: Bool
        ) -> DailyUsageEntry? {
            guard let payloadBlocks = day.dailyUsage?.blocks, !payloadBlocks.isEmpty else { return nil }
            let blocks = payloadBlocks
                .map { DailyUsageEntryBlock(kind: $0.kind, totalKwh: max(0, $0.totalKwh)) }
                .sorted { $0.kind.chronologicalIndex < $1.kind.chronologicalIndex }
            let stackedTotal = blocks.reduce(0.0) { $0 + $1.totalKwh }
            return DailyUsageEntry(
                date: parsedDate,
                dayID: day.date,
                blocks: blocks,
                stackedTotalKwh: stackedTotal,
                isToday: isToday
            )
        }
    }
}

private protocol DayRecordValue {
    var comparableValue: Double { get }
    var date: Date { get }
}

extension HistoryViewModel.DayKwhRecord: DayRecordValue {
    fileprivate var comparableValue: Double { kwh }
}

extension HistoryViewModel.LowestSocRecord: DayRecordValue {
    fileprivate var comparableValue: Double { soc }
}

extension HistoryViewModel {
    fileprivate struct Totals {
        // Display aggregates — every day in range, today included.
        var dayCount = 0
        var solarTotal = 0.0
        var chargeTotal = 0.0
        var dischargeTotal = 0.0
        var dailyUsageStackTotal = 0.0
        var dailyUsageDisplayDayCount = 0
        var mostUsage: DayKwhRecord?
        var mostSolar: DayKwhRecord?
        var lowestSoc: LowestSocRecord?
        // Grid aggregates — every day with an off-peak record (today included).
        var peakImportTotal = 0.0
        var offpeakImportTotal = 0.0
        var exportTotal = 0.0
        var gridDayCount = 0
        // Average bases — complete days only (today is partial → would skew).
        var completeDayCount = 0
        var solarCompleteTotal = 0.0
        var dischargeCompleteTotal = 0.0
        var dailyUsageCompleteStackTotal = 0.0
        var dailyUsageCompleteDayCount = 0
        var dailyUsageKindSums: [DailyUsageBlock.Kind: Double] = [:]
        var nightTotal = 0.0
        var nightBlockDayCount = 0

        mutating func addGrid(_ entry: GridEntry) {
            peakImportTotal += entry.peakImportKwh
            offpeakImportTotal += entry.offpeakImportKwh
            exportTotal += entry.exportKwh
            gridDayCount += 1
        }

        /// Folds one day into both the display aggregates (always) and the
        /// average bases (complete days only). Totals and day-records are hard
        /// numbers so they include today; the averages exclude it.
        mutating func addDay(
            _ day: DayEnergy,
            parsedDate: Date,
            isToday: Bool,
            dailyUsageEntry: DailyUsageEntry?
        ) {
            // Display aggregates (today included).
            dayCount += 1
            solarTotal += day.epv
            chargeTotal += day.eCharge
            dischargeTotal += day.eDischarge

            Self.consider(
                &mostSolar,
                candidate: DayKwhRecord(dayID: day.date, date: parsedDate, kwh: day.epv),
                prefersLarger: true
            )

            if let entry = dailyUsageEntry {
                dailyUsageStackTotal += entry.stackedTotalKwh
                dailyUsageDisplayDayCount += 1
                Self.consider(
                    &mostUsage,
                    candidate: DayKwhRecord(dayID: day.date, date: parsedDate, kwh: entry.stackedTotalKwh),
                    prefersLarger: true
                )
            }

            considerSocLow(day: day, parsedDate: parsedDate)

            // Average bases (today excluded).
            guard !isToday else { return }
            completeDayCount += 1
            solarCompleteTotal += day.epv
            dischargeCompleteTotal += day.eDischarge

            guard let entry = dailyUsageEntry else { return }

            dailyUsageCompleteStackTotal += entry.stackedTotalKwh
            dailyUsageCompleteDayCount += 1
            for block in entry.blocks {
                dailyUsageKindSums[block.kind, default: 0] += block.totalKwh
            }

            if let night = entry.blocks.first(where: { $0.kind == .night }) {
                nightTotal += night.totalKwh
                nightBlockDayCount += 1
            }
        }

        /// Lowest SoC includes today; `socLow` is a final-or-running floor, so
        /// today's value is meaningful even mid-day.
        private mutating func considerSocLow(day: DayEnergy, parsedDate: Date) {
            guard let soc = day.socLow, soc.isFinite else { return }
            Self.consider(
                &lowestSoc,
                candidate: LowestSocRecord(
                    dayID: day.date, date: parsedDate, soc: soc, socLowTime: day.socLowTime
                ),
                prefersLarger: false
            )
        }

        private static func consider<T: DayRecordValue>(
            _ current: inout T?,
            candidate: T,
            prefersLarger: Bool
        ) {
            guard let existing = current else { current = candidate; return }
            let strictlyBeats = prefersLarger
                ? candidate.comparableValue > existing.comparableValue
                : candidate.comparableValue < existing.comparableValue
            // Raw `Double` `==` is intentional here: `comparableValue` reflects
            // wire values with no arithmetic applied between storage and tie-break,
            // so an epsilon tolerance would mask real ties (Decision 10).
            let equal = candidate.comparableValue == existing.comparableValue
            if strictlyBeats || (equal && candidate.date > existing.date) {
                current = candidate
            }
        }

        var snapshot: PeriodSummary {
            let largest = largestDailyUsageKind
            return PeriodSummary(
                solarTotalKwh: solarTotal,
                solarCompleteTotalKwh: solarCompleteTotal,
                solarDayCount: completeDayCount,
                dayCount: dayCount,
                peakImportTotalKwh: peakImportTotal,
                offpeakImportTotalKwh: offpeakImportTotal,
                exportTotalKwh: exportTotal,
                gridDayCount: gridDayCount,
                chargeTotalKwh: chargeTotal,
                dischargeTotalKwh: dischargeTotal,
                dischargeCompleteTotalKwh: dischargeCompleteTotal,
                batteryDayCount: completeDayCount,
                dailyUsageTotalKwh: dailyUsageStackTotal,
                dailyUsageCompleteTotalKwh: dailyUsageCompleteStackTotal,
                dailyUsageDayCount: dailyUsageCompleteDayCount,
                dailyUsageDisplayDayCount: dailyUsageDisplayDayCount,
                dailyUsageLargestKind: largest,
                dailyUsageLargestKindTotalKwh: largest.flatMap { dailyUsageKindSums[$0] } ?? 0,
                nightTotalKwh: nightTotal,
                nightBlockDayCount: nightBlockDayCount,
                mostUsageDay: mostUsage,
                mostSolarDay: mostSolar,
                lowestSocDay: lowestSoc
            )
        }

        private var largestDailyUsageKind: DailyUsageBlock.Kind? {
            guard dailyUsageCompleteDayCount > 0 else { return nil }
            return DailyUsageBlock.Kind.largest(
                among: DailyUsageBlock.Kind.chronologicalOrder.lazy.map {
                    (kind: $0, value: dailyUsageKindSums[$0] ?? 0)
                }
            )
        }
    }
}
