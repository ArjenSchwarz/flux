import SwiftUI

/// Compare toggle + period chip + failure caption. Hosted directly above
/// the SummaryBlock card on Day Detail. The caption is rendered inside
/// this view (not as a sibling) so it stays leading-aligned to the chip
/// per AC 5.5; the period text is sourced from the live `period`
/// binding so the caption updates instantly on chip taps even before
/// the in-flight comparison fetch resolves.
struct CompareControl: View {
    @Binding var enabled: Bool
    @Binding var period: ComparePeriod
    let unavailable: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: 12) {
                Toggle("Compare", isOn: $enabled)
                    .labelsHidden()
                Text("Compare")
                    .appFont(FluxTheme.Typography.statRowLabel)
                    .foregroundStyle(FluxTheme.Palette.secondaryText)
                if enabled {
                    periodChip
                }
                Spacer()
            }
            if enabled && unavailable {
                Text("No comparison data available for \(period.displayName)")
                    .appFont(FluxTheme.Typography.touTime)
                    .foregroundStyle(FluxTheme.Palette.tertiaryText)
                    .accessibilityLabel("No comparison data available for \(period.displayName)")
            }
        }
    }

    private var periodChip: some View {
        Picker("Compare period", selection: $period) {
            ForEach(ComparePeriod.allCases) { option in
                Text(option.displayName).tag(option)
            }
        }
        .pickerStyle(.menu)
        .accessibilityLabel("Compare period, \(period.displayName)")
    }
}

#if DEBUG
#Preview("Off") {
    @Previewable @State var enabled = false
    @Previewable @State var period: ComparePeriod = .yesterday
    return CompareControl(enabled: $enabled, period: $period, unavailable: false)
        .padding()
        .background(FluxTheme.Palette.background)
}

#Preview("On, available") {
    @Previewable @State var enabled = true
    @Previewable @State var period: ComparePeriod = .yesterday
    return CompareControl(enabled: $enabled, period: $period, unavailable: false)
        .padding()
        .background(FluxTheme.Palette.background)
}

#Preview("On, unavailable") {
    @Previewable @State var enabled = true
    @Previewable @State var period: ComparePeriod = .sevenDaysAgo
    return CompareControl(enabled: $enabled, period: $period, unavailable: true)
        .padding()
        .background(FluxTheme.Palette.background)
}
#endif
