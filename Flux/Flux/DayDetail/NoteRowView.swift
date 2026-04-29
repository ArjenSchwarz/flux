import SwiftUI

/// Read-only note row used by Dashboard, History, and Day Detail (the
/// view-only state on Day Detail). Returns an `EmptyView` when `text` is
/// nil so callers can place it unconditionally and have it collapse.
struct NoteRowView: View {
    let text: String?

    var body: some View {
        if let text, !text.isEmpty {
            VStack(alignment: .leading, spacing: 6) {
                Label("Note", systemImage: "note.text")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                Text(text)
                    .font(.body)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .fixedSize(horizontal: false, vertical: true)
            }
            .padding()
            .background(.thinMaterial, in: RoundedRectangle(cornerRadius: 16, style: .continuous))
        } else {
            EmptyView()
        }
    }
}

#if DEBUG
#Preview("With note") {
    NoteRowView(text: "Away in Bali — minimal load expected.")
        .padding()
}

#Preview("No note") {
    NoteRowView(text: nil)
        .padding()
}
#endif
