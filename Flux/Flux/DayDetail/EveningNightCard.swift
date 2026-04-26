import FluxCore
import SwiftUI

struct EveningNightCard: View {
    let eveningNight: EveningNight

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Evening / Night")
                .font(.headline)

            // Chronological order for the calendar date being viewed: 00:00→sunrise (Night),
            // then sunset→24:00 (Evening). Title stays "Evening / Night" regardless.
            if let night = eveningNight.night {
                row(label: "Night", block: night, kind: .night)
            }
            if let evening = eveningNight.evening {
                row(label: "Evening", block: evening, kind: .evening)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding()
        .background(.thinMaterial, in: RoundedRectangle(cornerRadius: 16, style: .continuous))
    }

    private enum BlockKind {
        case evening
        case night
    }

    private func row(label: String, block: EveningNightBlock, kind: BlockKind) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            HStack {
                Text(label)
                    .font(.subheadline)
                Spacer()
                Text(timeRange(block))
                    .font(.subheadline)
            }
            HStack {
                Text(secondaryCaption(block, kind: kind))
                    .font(.caption)
                    .foregroundStyle(.secondary)
                Spacer()
                Text(totals(block))
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }
        }
    }

    // For an in-progress block the displayed `end` is `now`, not the nominal sunrise/sunset,
    // so we suppress the boundary caption and show "(so far)" instead.
    private func secondaryCaption(_ block: EveningNightBlock, kind: BlockKind) -> String {
        if block.status == .inProgress {
            return "(so far)"
        }
        if block.boundarySource == .estimated {
            switch kind {
            case .evening: return "≈ sunset"
            case .night:   return "≈ sunrise"
            }
        }
        return ""
    }

    private func timeRange(_ block: EveningNightBlock) -> String {
        guard let startDate = DateFormatting.parseTimestamp(block.start),
              let endDate = DateFormatting.parseTimestamp(block.end)
        else {
            return "—"
        }
        return "\(DateFormatting.clockTime24h(from: startDate)) – \(DateFormatting.clockTime24h(from: endDate))"
    }

    private func totals(_ block: EveningNightBlock) -> String {
        if let avg = block.averageKwhPerHour {
            return String(format: "%.1f kWh · %.2f kWh/h", block.totalKwh, avg)
        }
        return String(format: "%.1f kWh", block.totalKwh)
    }
}

#if DEBUG
#Preview {
    EveningNightCard(eveningNight: EveningNight(
        evening: EveningNightBlock(
            start: "2026-04-15T08:30:00Z",
            end: "2026-04-15T11:00:00Z",
            totalKwh: 2.7,
            averageKwhPerHour: 1.08,
            status: .inProgress,
            boundarySource: .readings
        ),
        night: EveningNightBlock(
            start: "2026-04-15T14:00:00Z",
            end: "2026-04-15T20:30:00Z",
            totalKwh: 4.2,
            averageKwhPerHour: 0.65,
            status: .complete,
            boundarySource: .readings
        )
    ))
    .padding()
}
#endif
