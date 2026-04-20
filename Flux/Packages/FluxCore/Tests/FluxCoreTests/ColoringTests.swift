import Foundation
import SwiftUI
import Testing
@testable import FluxCore

private extension ColorTier {
    var color: Color {
        switch self {
        case .green: .green
        case .red: .red
        case .orange: .orange
        case .amber: .yellow
        case .normal: .primary
        }
    }
}

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

    private struct GridColorCase {
        let pgrid: Double
        let sustained: Bool
        let inOffpeak: Bool
        let expected: ColorTier
    }

    @Test
    func gridColorRedOnlyForHighSustainedImportOutsideOffpeak() {
        let cases: [GridColorCase] = [
            GridColorCase(pgrid: 400, sustained: false, inOffpeak: false, expected: .normal),
            GridColorCase(pgrid: 400, sustained: false, inOffpeak: true, expected: .normal),
            GridColorCase(pgrid: 400, sustained: true, inOffpeak: false, expected: .normal),
            GridColorCase(pgrid: 400, sustained: true, inOffpeak: true, expected: .normal),
            GridColorCase(pgrid: 600, sustained: false, inOffpeak: false, expected: .normal),
            GridColorCase(pgrid: 600, sustained: false, inOffpeak: true, expected: .normal),
            GridColorCase(pgrid: 600, sustained: true, inOffpeak: true, expected: .normal),
            GridColorCase(pgrid: 600, sustained: true, inOffpeak: false, expected: .red)
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

    @Test
    func batteryColorTierProducesSwiftUIColor() {
        // Confirms the SwiftUI extension at the top of the file still works for
        // renderable tiers.
        _ = BatteryColor.forSOC(50).color
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
