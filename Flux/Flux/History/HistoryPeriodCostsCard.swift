import FluxCore
import SwiftUI

/// History "Period costs" card. Four tiles in fixed order — peak imports
/// cost, solar feed-in income, net, off-peak savings — plus a partial-
/// coverage caption when fewer than 100% of the days in the active range
/// are priced (AC 5.3).
@MainActor
struct HistoryPeriodCostsCard: View {
    let costs: PeriodCosts

    enum Tile: CaseIterable {
        case peakImportsCost
        case solarFeedInIncome
        case net
        case offPeakSavings
    }

    @Environment(\.horizontalSizeClass) private var hSizeClass

    private var columnCount: Int {
        #if os(macOS)
        return 4
        #else
        return hSizeClass == .regular ? 4 : 2
        #endif
    }

    var body: some View {
        HistoryCardChrome(title: "Period costs") {
            VStack(alignment: .leading, spacing: 8) {
                grid
                if let caption = HistoryPeriodCostsCard.captionText(costs: costs) {
                    Text(caption)
                        .appFont(.caption)
                        .foregroundStyle(FluxTheme.Palette.tertiaryText)
                }
            }
        }
    }

    private var grid: some View {
        LazyVGrid(
            columns: Array(
                repeating: GridItem(.flexible(), spacing: 12, alignment: .topLeading),
                count: columnCount
            ),
            alignment: .leading,
            spacing: 12
        ) {
            ForEach(Tile.allCases, id: \.self) { tile in
                tileView(for: tile)
            }
        }
        .animation(.default, value: columnCount)
    }

    private func tileView(for tile: Tile) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(HistoryPeriodCostsCard.label(for: tile))
                .appFont { FluxTheme.Typography.statRowLabel(family: $0) }
                .foregroundStyle(FluxTheme.Palette.secondaryText)
            Text(HistoryPeriodCostsCard.valueText(for: tile, costs: costs))
                .appFont { FluxTheme.Typography.statRowValue(family: $0) }
                .foregroundStyle(FluxTheme.Palette.primaryText)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
    }
}

// MARK: - Static helpers (exposed for tests)

extension HistoryPeriodCostsCard {
    static func label(for tile: Tile) -> String {
        switch tile {
        case .peakImportsCost: return "Peak imports"
        case .solarFeedInIncome: return "Solar feed-in"
        case .net: return "Net"
        case .offPeakSavings: return "Off-peak savings"
        }
    }

    static func valueText(for tile: Tile, costs: PeriodCosts) -> String {
        switch tile {
        case .peakImportsCost: return CostsCard.formatAUD(costs.peakImportsCost)
        case .solarFeedInIncome: return CostsCard.formatAUD(costs.solarFeedInIncome)
        case .net: return CostsCard.formatAUD(costs.net)
        case .offPeakSavings: return CostsCard.formatAUD(costs.offPeakSavings)
        }
    }

    /// "N of M days priced" caption per AC 5.3 — surfaces only when coverage
    /// is partial.
    static func captionText(costs: PeriodCosts) -> String? {
        guard costs.hasPartialCoverage else { return nil }
        return "\(costs.pricedDayCount) of \(costs.totalDayCount) days priced"
    }
}
