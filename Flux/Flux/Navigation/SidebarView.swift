import SwiftUI

struct SidebarView: View {
    @Binding var selection: Screen?
    let items: [Screen]

    init(selection: Binding<Screen?>, items: [Screen] = Screen.sidebarVisible) {
        self._selection = selection
        self.items = items
    }

    var body: some View {
        List(items, selection: $selection) { screen in
            Label(screen.title, systemImage: screen.systemImage)
                .tag(screen)
        }
        .navigationTitle("Flux")
    }
}

#Preview {
    NavigationStack {
        SidebarView(selection: .constant(.dashboard))
    }
}
