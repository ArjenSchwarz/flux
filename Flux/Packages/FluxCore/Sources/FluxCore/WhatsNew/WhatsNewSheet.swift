import Foundation
import SwiftUI

public struct WhatsNewSheet: View {
    @Environment(\.dismiss) private var dismiss

    private let releases: [WhatsNewRelease]

    public init(releases: [WhatsNewRelease]) {
        self.releases = releases
    }

    public var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: 28) {
                    ForEach(releases) { release in
                        ReleaseSection(release: release)
                    }
                }
                .padding(20)
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            .navigationTitle("What's New")
            #if os(iOS)
            .navigationBarTitleDisplayMode(.inline)
            #endif
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("Done") { dismiss() }
                        .accessibilityLabel("Dismiss What's New")
                }
            }
        }
        #if os(macOS)
        .frame(minWidth: 480, minHeight: 420)
        #endif
    }
}

private struct ReleaseSection: View {
    let release: WhatsNewRelease

    private static let dateFormat: Date.FormatStyle = .init().month(.wide).year()

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            VStack(alignment: .leading, spacing: 4) {
                Text("Version \(release.version)")
                    .font(.title3.weight(.semibold))
                Text(release.date.formatted(Self.dateFormat))
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
            }

            ForEach(Highlight.Category.allCases, id: \.self) { category in
                let group = release.highlights.filter { $0.category == category }
                if !group.isEmpty {
                    CategoryGroup(category: category, highlights: group)
                }
            }
        }
    }
}

private struct CategoryGroup: View {
    let category: Highlight.Category
    let highlights: [Highlight]

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            ForEach(highlights, id: \.self) { highlight in
                HighlightRow(highlight: highlight)
            }
        }
    }
}

private struct HighlightRow: View {
    let highlight: Highlight

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 14) {
            Image(systemName: highlight.symbol ?? highlight.category.defaultSymbol)
                .font(.title3)
                .foregroundStyle(.tint)
                .frame(width: 28, alignment: .leading)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 4) {
                Text(highlight.title)
                    .font(.body.weight(.semibold))
                if let detail = highlight.detail {
                    Text(detail)
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
            }
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(accessibilityLabel)
    }

    private var accessibilityLabel: String {
        var parts = "\(highlight.category.label). \(highlight.title)"
        if let detail = highlight.detail {
            parts += ". \(detail)"
        }
        return parts
    }
}

private extension Highlight.Category {
    var defaultSymbol: String {
        switch self {
        case .new: "sparkles"
        case .improved: "wand.and.stars"
        case .fixed: "checkmark.circle"
        }
    }
}

public struct PendingAutoPresentation: Identifiable, Sendable {
    public let id = UUID()
    public let releases: [WhatsNewRelease]

    public init(releases: [WhatsNewRelease]) {
        self.releases = releases
    }
}
