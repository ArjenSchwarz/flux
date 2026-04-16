import Foundation
import Testing
@testable import Flux

@MainActor @Suite(.serialized)
struct ColoringTests {
    @Test
    func batteryColorSOCBoundaries() {
        #expect(BatteryColor.forSOC(0) == .red)
        #expect(BatteryColor.forSOC(14.9) == .red)
        #expect(BatteryColor.forSOC(15) == .orange)
        #expect(BatteryColor.forSOC(29.9) == .orange)
        #expect(BatteryColor.forSOC(30) == .normal)
        #expect(BatteryColor.forSOC(60) == .normal)
        #expect(BatteryColor.forSOC(60.1) == .green)
        #expect(BatteryColor.forSOC(100) == .green)
    }

    @Test
    func gridColorRedOnlyForHighSustainedImportOutsideOffpeak() {
        let cases: [(pgrid: Double, sustained: Bool, inOffpeak: Bool, expected: ColorTier)] = [
            (400, false, false, .normal),
            (400, false, true, .normal),
            (400, true, false, .normal),
            (400, true, true, .normal),
            (600, false, false, .normal),
            (600, false, true, .normal),
            (600, true, true, .normal),
            (600, true, false, .red)
        ]

        for testCase in cases {
            let now = testCase.inOffpeak
                ? makeSydneyDate(year: 2026, month: 4, day: 15, hour: 12, minute: 0)
                : makeSydneyDate(year: 2026, month: 4, day: 15, hour: 15, minute: 0)

            let tier = GridColor.forGrid(
                pgrid: testCase.pgrid,
                pgridSustained: testCase.sustained,
                offpeakWindowStart: "11:00",
                offpeakWindowEnd: "14:00",
                now: now
            )

            #expect(tier == testCase.expected)
        }
    }

    @Test
    func gridColorIsGreenWhenExporting() {
        let now = makeSydneyDate(year: 2026, month: 4, day: 15, hour: 16, minute: 0)
        let tier = GridColor.forGrid(
            pgrid: -250,
            pgridSustained: true,
            offpeakWindowStart: "11:00",
            offpeakWindowEnd: "14:00",
            now: now
        )

        #expect(tier == .green)
    }

    @Test
    func cutoffTimeColorUsesRedAmberDefaultRules() {
        let now = makeSydneyDate(year: 2026, month: 4, day: 15, hour: 8, minute: 0)
        let redCutoff = makeSydneyDate(year: 2026, month: 4, day: 15, hour: 9, minute: 30)
        let amberCutoff = makeSydneyDate(year: 2026, month: 4, day: 15, hour: 10, minute: 30)
        let defaultCutoff = makeSydneyDate(year: 2026, month: 4, day: 15, hour: 12, minute: 0)

        #expect(CutoffTimeColor.forCutoff(redCutoff, offpeakWindowStart: "11:00", now: now) == .red)
        #expect(CutoffTimeColor.forCutoff(amberCutoff, offpeakWindowStart: "11:00", now: now) == .orange)
        #expect(CutoffTimeColor.forCutoff(defaultCutoff, offpeakWindowStart: "11:00", now: now) == .normal)
    }

    private var sydneyCalendar: Calendar {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = DateFormatting.sydneyTimeZone
        return calendar
    }

    private func makeSydneyDate(year: Int, month: Int, day: Int, hour: Int, minute: Int) -> Date {
        sydneyCalendar.date(from: DateComponents(
            timeZone: DateFormatting.sydneyTimeZone,
            year: year,
            month: month,
            day: day,
            hour: hour,
            minute: minute
        ))!
    }
}
