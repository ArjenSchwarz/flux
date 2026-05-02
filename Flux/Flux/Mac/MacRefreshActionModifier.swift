#if os(macOS)
import SwiftUI

private struct MacRefreshActionModifier: ViewModifier {
    @Environment(FluxRefreshCoordinator.self) private var coordinator
    let action: @MainActor () async -> Void

    func body(content: Content) -> some View {
        content
            .onAppear {
                coordinator.refresh = {
                    Task { await action() }
                }
            }
            .onDisappear {
                coordinator.refresh = nil
            }
    }
}

extension View {
    func macRefreshAction(_ action: @escaping @MainActor () async -> Void) -> some View {
        modifier(MacRefreshActionModifier(action: action))
    }
}
#endif
