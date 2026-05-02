#if os(macOS)
import SwiftUI

struct FluxKeyboardCommands: Commands {
    let coordinator: FluxRefreshCoordinator

    var body: some Commands {
        CommandGroup(after: .toolbar) {
            Button("Refresh") {
                coordinator.refresh?()
            }
            .keyboardShortcut("r", modifiers: .command)
        }
    }
}
#endif
