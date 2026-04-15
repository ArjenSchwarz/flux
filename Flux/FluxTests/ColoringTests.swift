import SwiftUI
import Testing
@testable import Flux

@MainActor @Suite(.serialized)
struct ColoringTests {
    @Test
    func batteryColorSOCBoundaries() {
        expectColor(BatteryColor.forSOC(0), equals: .red)
        expectColor(BatteryColor.forSOC(14.9), equals: .red)
        expectColor(BatteryColor.forSOC(15), equals: .orange)
        expectColor(BatteryColor.forSOC(29.9), equals: .orange)
        expectColor(BatteryColor.forSOC(30), equals: .primary)
        expectColor(BatteryColor.forSOC(60), equals: .primary)
        expectColor(BatteryColor.forSOC(60.1), equals: .green)
        expectColor(BatteryColor.forSOC(100), equals: .green)
    }

    @Test
    func gridColorRedOnlyForHighSustainedImportOutsideOffpeak() {
        let cases: [(pgrid: Double, sustained: Bool, inOffpeak: Bool, expected: Color)] = [
            (400, false, false, .primary),
            (400, false, true, .primary),
            (400, true, false, .primary),
            (400, true, true, .primary),
            (600, false, false, .primary),
            (600, false, true, .primary),
            (600, true, true, .primary),
            (600, true, false, .red)
        ]

        for testCase in cases {
            let now = testCase.inOffpeak
                ? makeSydneyDate(year: 2026, month: 4, day: 15, hour: 12, minute: 0)
                : makeSydneyDate(year: 2026, month: 4, day: 15, hour: 15, minute: 0)

            let color = GridColor.forGrid(
                pgrid: testCase.pgrid,
                pgridSustained: testCase.sustained,
                offpeakWindowStart: "11:00",
                offpeakWindowEnd: "14:00",
                now: now
            )

            expectColor(color, equals: testCase.expected)
        }
    }

    @Test
    func gridColorIsGreenWhenExporting() {
        let now = makeSydneyDate(year: 2026, month: 4, day: 15, hour: 16, minute: 0)
        let color = GridColor.forGrid(
            pgrid: -250,
            pgridSustained: true,
            offpeakWindowStart: "11:00",
            offpeakWindowEnd: "14:00",
            now: now
        )

        expectColor(color, equals: .green)
    }

    @Test
    func cutoffTimeColorUsesRedAmberDefaultRules() {
        let now = makeSydneyDate(year: 2026, month: 4, day: 15, hour: 8, minute: 0)
        let redCutoff = makeSydneyDate(year: 2026, month: 4, day: 15, hour: 9, minute: 30)
        let amberCutoff = makeSydneyDate(year: 2026, month: 4, day: 15, hour: 10, minute: 30)
        let defaultCutoff = makeSydneyDate(year: 2026, month: 4, day: 15, hour: 12, minute: 0)

        expectColor(CutoffTimeColor.forCutoff(redCutoff, offpeakWindowStart: "11:00", now: now), equals: .red)
        expectColor(CutoffTimeColor.forCutoff(amberCutoff, offpeakWindowStart: "11:00", now: now), equals: .orange)
        expectColor(CutoffTimeColor.forCutoff(defaultCutoff, offpeakWindowStart: "11:00", now: now), equals: .primary)
    }

    private func expectColor(_ color: Color, equals expected: Color) {
        #expect(String(reflecting: color) == String(reflecting: expected))
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
