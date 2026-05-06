import SwiftUI

/// Glassy card chrome used for every grouped section in the V5 redesign.
struct FluxPanel<Content: View>: View {
    var padding: CGFloat = FluxTheme.Metrics.panelPadding
    @ViewBuilder var content: () -> Content

    var body: some View {
        content()
            .padding(padding)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(
                RoundedRectangle(cornerRadius: FluxTheme.Metrics.panelCornerRadius, style: .continuous)
                    .fill(.ultraThinMaterial)
                    .opacity(0.7)
            )
            .background(
                RoundedRectangle(cornerRadius: FluxTheme.Metrics.panelCornerRadius, style: .continuous)
                    .fill(FluxTheme.Palette.panel)
            )
            .overlay(
                RoundedRectangle(cornerRadius: FluxTheme.Metrics.panelCornerRadius, style: .continuous)
                    .strokeBorder(FluxTheme.Palette.border, lineWidth: FluxTheme.Metrics.hairline)
            )
    }
}

/// Top-of-panel header with an uppercase label on the left and an optional
/// monospaced unit/caption on the right.
struct FluxPanelHeader: View {
    let label: String
    var right: String?

    var body: some View {
        HStack(alignment: .firstTextBaseline) {
            Text(label.uppercased())
                .appFont(FluxTheme.Typography.panelHeader)
                .tracking(1.2)
                .foregroundStyle(FluxTheme.Palette.tertiaryText)
            Spacer()
            if let right {
                Text(right)
                    .appFont(FluxTheme.Typography.panelHeaderRight)
                    .foregroundStyle(FluxTheme.Palette.tertiaryText)
            }
        }
        .padding(.bottom, 6)
    }
}

/// Single label/value row used inside grouped panels. Optionally renders a
/// small monospaced sub-caption next to the label and a hairline divider
/// underneath unless `last` is true.
struct FluxStatRow: View {
    let label: String
    let value: String
    var sub: String?
    var accent: Color?
    var last: Bool = false

    var body: some View {
        VStack(spacing: 0) {
            HStack(alignment: .firstTextBaseline) {
                HStack(alignment: .firstTextBaseline, spacing: 6) {
                    Text(label)
                        .appFont(FluxTheme.Typography.statRowLabel)
                        .foregroundStyle(FluxTheme.Palette.secondaryText)
                    if let sub {
                        Text(sub)
                            .appFont(FluxTheme.Typography.statRowSub)
                            .foregroundStyle(FluxTheme.Palette.tertiaryText)
                    }
                }
                Spacer()
                Text(value)
                    .appFont(FluxTheme.Typography.statRowValue)
                    .foregroundStyle(accent ?? FluxTheme.Palette.primaryText)
            }
            .padding(.vertical, FluxTheme.Metrics.statRowVerticalPadding)

            if !last {
                Rectangle()
                    .fill(FluxTheme.Palette.border)
                    .frame(height: FluxTheme.Metrics.hairline)
            }
        }
    }
}

/// Universal three-segment tab bar used across Dashboard / Today / History.
struct FluxTabBar: View {
    @Binding var selection: FluxTab
    /// Fires on every tap (even when the tapped tab is already selected) so
    /// the host can clear that tab's NavigationStack path. The selection
    /// binding is then updated for cross-tab switches.
    var onActivate: ((FluxTab) -> Void)?

    var body: some View {
        HStack(spacing: 2) {
            ForEach(FluxTab.allCases) { tab in
                Button {
                    onActivate?(tab)
                    if selection != tab {
                        withAnimation(.easeOut(duration: 0.18)) {
                            selection = tab
                        }
                    }
                } label: {
                    let tabWeight: Font.Weight = selection == tab ? .semibold : .medium
                    Text(tab.title)
                        .appFont { FluxTheme.Typography.tabItem(family: $0).weight(tabWeight) }
                        .tracking(0.1)
                        .foregroundStyle(
                            selection == tab ? FluxTheme.Palette.primaryText : FluxTheme.Palette.secondaryText
                        )
                        .frame(maxWidth: .infinity)
                        .padding(.vertical, 7)
                        .background(
                            RoundedRectangle(cornerRadius: FluxTheme.Metrics.tabBarItemCornerRadius, style: .continuous)
                                .fill(selection == tab ? FluxTheme.Palette.tabBarItemActiveFill : Color.clear)
                        )
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityAddTraits(.isButton)
                .accessibilityLabel(selection == tab ? "\(tab.title), selected" : tab.title)
            }
        }
        .padding(FluxTheme.Metrics.tabBarPadding)
        .background(
            RoundedRectangle(cornerRadius: FluxTheme.Metrics.tabBarCornerRadius, style: .continuous)
                .fill(FluxTheme.Palette.tabBarFill)
        )
        .overlay(
            RoundedRectangle(cornerRadius: FluxTheme.Metrics.tabBarCornerRadius, style: .continuous)
                .strokeBorder(FluxTheme.Palette.border, lineWidth: FluxTheme.Metrics.hairline)
        )
    }
}

/// Small gear pill rendered next to the tab bar. Matches the tab bar's
/// height, fill, and border so the two read as a single navigation row.
struct FluxTabBarSettingsButton: View {
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            Image(systemName: "gearshape")
                .appFontSystem(size: 13, weight: .medium)
                .foregroundStyle(FluxTheme.Palette.secondaryText)
                .frame(width: 36)
                .padding(.vertical, 7)
                .padding(FluxTheme.Metrics.tabBarPadding)
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .background(
            RoundedRectangle(cornerRadius: FluxTheme.Metrics.tabBarCornerRadius, style: .continuous)
                .fill(FluxTheme.Palette.tabBarFill)
        )
        .overlay(
            RoundedRectangle(cornerRadius: FluxTheme.Metrics.tabBarCornerRadius, style: .continuous)
                .strokeBorder(FluxTheme.Palette.border, lineWidth: FluxTheme.Metrics.hairline)
        )
        .accessibilityLabel("Settings")
    }
}

/// Header block for every screen: tab bar (and optional settings pill) →
/// optional uppercase eyebrow → optional page title. When neither eyebrow
/// nor title is provided, only the tab bar row renders.
struct FluxScreenHeader: View {
    var eyebrow: String?
    var title: String?
    @Binding var selection: FluxTab
    var onSettingsTap: (() -> Void)?
    var onTabActivate: ((FluxTab) -> Void)?

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            HStack(spacing: 8) {
                FluxTabBar(selection: $selection, onActivate: onTabActivate)
                if let onSettingsTap {
                    FluxTabBarSettingsButton(action: onSettingsTap)
                }
            }

            if eyebrow != nil || title != nil {
                VStack(alignment: .leading, spacing: 2) {
                    if let eyebrow {
                        Text(eyebrow.uppercased())
                            .appFont(FluxTheme.Typography.eyebrow)
                            .tracking(1.6)
                            .foregroundStyle(FluxTheme.Palette.tertiaryText)
                    }
                    if let title {
                        Text(title)
                            .appFont(FluxTheme.Typography.pageTitle)
                            .tracking(-0.6)
                            .foregroundStyle(FluxTheme.Palette.primaryText)
                    }
                }
            }
        }
        .padding(.top, 6)
        .padding(.bottom, 4)
    }
}

#if DEBUG
#Preview("Stat row") {
    ZStack {
        FluxTheme.Palette.background.ignoresSafeArea()
        FluxPanel {
            FluxStatRow(label: "Free grid in", value: "3.42 kWh", accent: FluxTheme.Palette.offpeak)
            FluxStatRow(label: "Battery charged", value: "+24%")
            FluxStatRow(label: "Lowest", value: "38%", sub: "SOC at 11:14")
            FluxStatRow(label: "15m avg load", value: "1.68 kW", last: true)
        }
        .padding()
    }
}

#Preview("Tab bar") {
    @Previewable @State var tab: FluxTab = .dashboard
    return ZStack {
        FluxTheme.Palette.background.ignoresSafeArea()
        FluxTabBar(selection: $tab)
            .padding()
    }
}
#endif
