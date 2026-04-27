import FluxCore
import SwiftUI

struct DailyUsageCard: View {
    let dailyUsage: DailyUsage

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Daily Usage")
                .font(.headline)

            ForEach(dailyUsage.blocks) { block in
                row(block)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding()
        .background(.thinMaterial, in: RoundedRectangle(cornerRadius: 16, style: .continuous))
    }

    private func row(_ block: DailyUsageBlock) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            HStack {
                Text(label(for: block.kind))
                    .font(.subheadline)
                Spacer()
                timeRangeView(block)
            }
            HStack {
                Spacer()
                Text(totals(block))
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }
            HStack {
                if block.status == .inProgress {
                    Text("(so far)")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
                Spacer()
                Text("\(block.percentOfDay)%")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        }
    }

    @ViewBuilder
    private func timeRangeView(_ block: DailyUsageBlock) -> some View {
        let times = timeRange(block)
        let cap = caption(for: block.kind)
        let showCaption = block.boundarySource == .estimated && !cap.isEmpty
        let leads = captionLeads(block.kind)
        HStack(spacing: 4) {
            if showCaption && leads {
                Text(cap)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            Text(times)
                .font(.subheadline)
            if showCaption && !leads {
                Text(cap)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        }
    }

    private func label(for kind: DailyUsageBlock.Kind) -> String {
        switch kind {
        case .night: return "Night"
        case .morningPeak: return "Morning Peak"
        case .offPeak: return "Off-Peak"
        case .afternoonPeak: return "Afternoon Peak"
        case .evening: return "Evening"
        }
    }

    private func caption(for kind: DailyUsageBlock.Kind) -> String {
        switch kind {
        case .night: return "≈ sunrise"
        case .morningPeak: return "≈ sunrise"
        case .offPeak: return ""
        case .afternoonPeak: return "≈ sunset"
        case .evening: return "≈ sunset"
        }
    }

    // True when the caption renders before the time range (estimated edge is the start).
    private func captionLeads(_ kind: DailyUsageBlock.Kind) -> Bool {
        switch kind {
        case .morningPeak, .evening: return true
        case .night, .offPeak, .afternoonPeak: return false
        }
    }

    private func timeRange(_ block: DailyUsageBlock) -> String {
        guard let startDate = DateFormatting.parseTimestamp(block.start),
              let endDate = DateFormatting.parseTimestamp(block.end)
        else {
            return "—"
        }
        return "\(DateFormatting.clockTime24h(from: startDate)) – \(DateFormatting.clockTime24h(from: endDate))"
    }

    private func totals(_ block: DailyUsageBlock) -> String {
        if let avg = block.averageKwhPerHour {
            return String(format: "%.1f kWh · %.2f kWh/h", block.totalKwh, avg)
        }
        return String(format: "%.1f kWh", block.totalKwh)
    }
}

#if DEBUG
#Preview {
    DailyUsageCard(dailyUsage: DailyUsage(blocks: [
        DailyUsageBlock(
            kind: .night,
            start: "2026-04-14T14:00:00Z",
            end: "2026-04-14T20:30:00Z",
            totalKwh: 3.1,
            averageKwhPerHour: 0.48,
            percentOfDay: 18,
            status: .complete,
            boundarySource: .readings
        ),
        DailyUsageBlock(
            kind: .morningPeak,
            start: "2026-04-14T20:30:00Z",
            end: "2026-04-15T01:00:00Z",
            totalKwh: 2.1,
            averageKwhPerHour: 0.47,
            percentOfDay: 12,
            status: .complete,
            boundarySource: .estimated
        ),
        DailyUsageBlock(
            kind: .offPeak,
            start: "2026-04-15T01:00:00Z",
            end: "2026-04-15T04:00:00Z",
            totalKwh: 5.0,
            averageKwhPerHour: 1.67,
            percentOfDay: 30,
            status: .complete,
            boundarySource: .readings
        ),
        DailyUsageBlock(
            kind: .afternoonPeak,
            start: "2026-04-15T04:00:00Z",
            end: "2026-04-15T08:42:00Z",
            totalKwh: 4.5,
            averageKwhPerHour: 0.96,
            percentOfDay: 27,
            status: .complete,
            boundarySource: .estimated
        ),
        DailyUsageBlock(
            kind: .evening,
            start: "2026-04-15T08:42:00Z",
            end: "2026-04-15T14:00:00Z",
            totalKwh: 2.2,
            averageKwhPerHour: 0.41,
            percentOfDay: 13,
            status: .inProgress,
            boundarySource: .estimated
        )
    ]))
    .padding()
}
#endif
