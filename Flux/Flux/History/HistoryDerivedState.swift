import FluxCore
import Foundation

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
            guard let largest = blocks.max(by: { $0.totalKwh < $1.totalKwh }) else {
                return "\(dateText): \(total)"
            }
            return "\(dateText): \(total), \(largest.kind.displayLabel) largest"
        }
    }

    struct PeriodSummary: Equatable {
        let solarTotalKwh: Double
        /// Excludes today (today is partial; including it would skew daily
        /// averages). `batteryDayCount` follows the same rule.
        let solarDayCount: Int
        let peakImportTotalKwh: Double
        let offpeakImportTotalKwh: Double
        let exportTotalKwh: Double
        /// Includes today when an off-peak record exists. Off-peak imports
        /// are the actionable headline so showing today's in-progress
        /// number matters more than a clean daily average.
        let gridDayCount: Int
        let chargeTotalKwh: Double
        let dischargeTotalKwh: Double
        let batteryDayCount: Int
        /// Sum of clamped stacked totals across complete days-with-blocks.
        let dailyUsageTotalKwh: Double
        /// Number of complete days-with-blocks contributing to the total.
        let dailyUsageDayCount: Int
        /// Largest contributing block kind across complete days-with-blocks,
        /// with ties broken by chronological order (AC 1.8). `nil` when no
        /// complete day-with-blocks exists in the range.
        let dailyUsageLargestKind: DailyUsageBlock.Kind?
        /// Sum of clamped `totalKwh` across complete days for
        /// `dailyUsageLargestKind`. Used by the card's subtitle to express
        /// the per-day average of the leading block kind.
        let dailyUsageLargestKindTotalKwh: Double

        static let empty = PeriodSummary(
            solarTotalKwh: 0,
            solarDayCount: 0,
            peakImportTotalKwh: 0,
            offpeakImportTotalKwh: 0,
            exportTotalKwh: 0,
            gridDayCount: 0,
            chargeTotalKwh: 0,
            dischargeTotalKwh: 0,
            batteryDayCount: 0,
            dailyUsageTotalKwh: 0,
            dailyUsageDayCount: 0,
            dailyUsageLargestKind: nil,
            dailyUsageLargestKindTotalKwh: 0
        )

        var solarPerDayKwh: Double? {
            solarDayCount > 0 ? solarTotalKwh / Double(solarDayCount) : nil
        }

        var dischargePerDayKwh: Double? {
            batteryDayCount > 0 ? dischargeTotalKwh / Double(batteryDayCount) : nil
        }

        var dailyUsageAvgKwh: Double? {
            dailyUsageDayCount > 0 ? dailyUsageTotalKwh / Double(dailyUsageDayCount) : nil
        }

        var dailyUsageLargestKindAvgKwh: Double? {
            dailyUsageDayCount > 0 ? dailyUsageLargestKindTotalKwh / Double(dailyUsageDayCount) : nil
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
                if !isToday {
                    totals.addCompleteDay(day)
                }
                if let entry = Self.dailyUsageEntry(day: day, parsedDate: parsedDate, isToday: isToday) {
                    dailyUsage.append(entry)
                    if !isToday {
                        totals.addDailyUsage(entry)
                    }
                }
            }

            self.solar = solar
            self.grid = grid
            self.battery = battery
            self.dailyUsage = dailyUsage
            self.summary = totals.snapshot
        }

        private static func gridEntry(day: DayEnergy, parsedDate: Date, isToday: Bool) -> GridEntry? {
            guard let offpeakImport = day.offpeakGridImportKwh else { return nil }
            let peak = max(0, day.eInput - offpeakImport)
            return GridEntry(
                date: parsedDate,
                dayID: day.date,
                peakImportKwh: peak,
                offpeakImportKwh: offpeakImport,
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

extension HistoryViewModel {
    struct Totals {
        var solarTotal = 0.0
        var peakImportTotal = 0.0
        var offpeakImportTotal = 0.0
        var exportTotal = 0.0
        var chargeTotal = 0.0
        var dischargeTotal = 0.0
        var completeDayCount = 0
        var gridDayCount = 0
        var dailyUsageStackTotal = 0.0
        var dailyUsageDayCount = 0
        var dailyUsageKindSums: [DailyUsageBlock.Kind: Double] = [:]

        mutating func addGrid(_ entry: GridEntry) {
            peakImportTotal += entry.peakImportKwh
            offpeakImportTotal += entry.offpeakImportKwh
            exportTotal += entry.exportKwh
            gridDayCount += 1
        }

        mutating func addCompleteDay(_ day: DayEnergy) {
            solarTotal += day.epv
            chargeTotal += day.eCharge
            dischargeTotal += day.eDischarge
            completeDayCount += 1
        }

        mutating func addDailyUsage(_ entry: DailyUsageEntry) {
            dailyUsageStackTotal += entry.stackedTotalKwh
            dailyUsageDayCount += 1
            for block in entry.blocks {
                dailyUsageKindSums[block.kind, default: 0] += block.totalKwh
            }
        }

        var snapshot: PeriodSummary {
            let largest = largestDailyUsageKind
            return PeriodSummary(
                solarTotalKwh: solarTotal,
                solarDayCount: completeDayCount,
                peakImportTotalKwh: peakImportTotal,
                offpeakImportTotalKwh: offpeakImportTotal,
                exportTotalKwh: exportTotal,
                gridDayCount: gridDayCount,
                chargeTotalKwh: chargeTotal,
                dischargeTotalKwh: dischargeTotal,
                batteryDayCount: completeDayCount,
                dailyUsageTotalKwh: dailyUsageStackTotal,
                dailyUsageDayCount: dailyUsageDayCount,
                dailyUsageLargestKind: largest,
                dailyUsageLargestKindTotalKwh: largest.flatMap { dailyUsageKindSums[$0] } ?? 0
            )
        }

        // Tolerance-band tie-break per AC 1.8: kinds whose sums differ by
        // less than 0.01 kWh are treated as tied, with chronological order
        // breaking the tie. Iteration in chronological order gives the
        // earliest-kind-wins behaviour without an explicit secondary sort.
        private var largestDailyUsageKind: DailyUsageBlock.Kind? {
            guard dailyUsageDayCount > 0 else { return nil }
            var best: (kind: DailyUsageBlock.Kind, sum: Double)?
            for kind in DailyUsageBlock.Kind.chronologicalOrder {
                let sum = dailyUsageKindSums[kind] ?? 0
                guard let current = best else {
                    best = (kind, sum)
                    continue
                }
                if (sum - current.sum).magnitude < 0.01 {
                    continue
                }
                if sum > current.sum {
                    best = (kind, sum)
                }
            }
            return best?.kind
        }
    }
}
