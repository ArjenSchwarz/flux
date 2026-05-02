#if os(macOS)
import SwiftUI

struct AppearsActiveMonitor: ViewModifier {
    @Environment(\.appearsActive) private var appearsActive
    let viewModel: DashboardViewModel

    func body(content: Content) -> some View {
        content.onChange(of: appearsActive, initial: true) { _, isActive in
            viewModel.updateActivityTier(isActive ? .active : .inactive)
        }
    }
}
#endif
