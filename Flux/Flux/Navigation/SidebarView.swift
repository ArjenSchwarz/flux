import SwiftUI

struct SidebarView: View {
    @Binding var selection: Screen?

    var body: some View {
        List(Screen.allCases, selection: $selection) { screen in
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
