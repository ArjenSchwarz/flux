import FluxCore
import SwiftData
import SwiftUI

// Previews live in their own file to keep `HistoryView.swift` under the
// SwiftLint file-length cap.

#if DEBUG
#Preview("Compact") {
    let configuration = ModelConfiguration(isStoredInMemoryOnly: true)
    // swiftlint:disable:next force_try
    let container = try! ModelContainer(for: CachedDayEnergy.self, configurations: configuration)
    NavigationStack {
        HistoryView(apiClient: MockFluxAPIClient.preview, modelContext: ModelContext(container))
    }
}

#Preview("Regular 770") {
    let configuration = ModelConfiguration(isStoredInMemoryOnly: true)
    // swiftlint:disable:next force_try
    let container = try! ModelContainer(for: CachedDayEnergy.self, configurations: configuration)
    NavigationStack {
        HistoryView(apiClient: MockFluxAPIClient.preview, modelContext: ModelContext(container))
    }
    .frame(width: 770)
    .environment(\.horizontalSizeClass, .regular)
}
#endif
