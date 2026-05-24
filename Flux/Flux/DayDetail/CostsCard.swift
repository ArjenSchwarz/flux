import FluxCore
import SwiftUI

/// Day Detail costs card. Four rows in fixed order (AC 4.1): peak imports
/// cost, solar feed-in income, net, off-peak savings. Monetary values
/// rendered as AUD with two decimal places; negatives prefix a leading minus.
@MainActor
struct CostsCard: View {
    let costs: DayCosts

    enum Row: CaseIterable {
        case peakImportsCost
        case solarFeedInIncome
        case net
        case offPeakSavings
    }

    var body: some View {
        FluxPanel {
            VStack(alignment: .leading, spacing: 8) {
                Text("Costs")
                    .appFont { FluxTheme.Typography.statRowLabel(family: $0).weight(.semibold) }
                    .foregroundStyle(FluxTheme.Palette.primaryText)
                ForEach(Array(Row.allCases.enumerated()), id: \.offset) { _, row in
                    HStack {
                        Text(CostsCard.label(for: row))
                            .appFont { FluxTheme.Typography.statRowLabel(family: $0) }
                            .foregroundStyle(FluxTheme.Palette.secondaryText)
                        Spacer()
                        Text(CostsCard.valueText(for: row, costs: costs))
                            .appFont { FluxTheme.Typography.statRowValue(family: $0) }
                            .foregroundStyle(FluxTheme.Palette.primaryText)
                    }
                }
            }
        }
    }
}

// MARK: - Static helpers (exposed for tests)

extension CostsCard {
    static func label(for row: Row) -> String {
        switch row {
        case .peakImportsCost: return "Peak imports cost"
        case .solarFeedInIncome: return "Solar feed-in income"
        case .net: return "Net"
        case .offPeakSavings: return "Off-peak savings"
        }
    }

    static func valueText(for row: Row, costs: DayCosts) -> String {
        switch row {
        case .peakImportsCost: return formatAUD(costs.peakImportsCost)
        case .solarFeedInIncome: return formatAUD(costs.solarFeedInIncome)
        case .net: return formatAUD(costs.net)
        case .offPeakSavings: return formatAUD(costs.offPeakSavings)
        }
    }

    /// AUD format with two decimal places. A negative value gets a single
    /// leading minus before the dollar sign (per AC 4.7 — "$3.42" / "−$3.42").
    /// Display rounds half-away-from-zero so totals never look like
    /// floating-point garbage.
    static func formatAUD(_ value: Double) -> String {
        let rounded = (value * 100).rounded() / 100
        // Treat -0.00 as 0.00 — a negligible negative due to rounding still
        // reads as zero on the card, not "−$0.00".
        if abs(rounded) < 0.005 {
            return "$0.00"
        }
        let prefix = rounded < 0 ? "−" : ""
        return "\(prefix)$\(String(format: "%.2f", abs(rounded)))"
    }
}
