import FluxCore
import SwiftUI

struct NoteEditorSheet: View {
    @Environment(\.dismiss) private var dismiss
    @State private var viewModel: NoteEditorViewModel

    init(viewModel: NoteEditorViewModel) {
        _viewModel = State(initialValue: viewModel)
    }

    var body: some View {
        NavigationStack {
            VStack(alignment: .leading, spacing: 12) {
                @Bindable var binding = viewModel
                TextEditor(text: $binding.draft)
                    .frame(minHeight: 160)
                    .padding(8)
                    .background(.thinMaterial, in: RoundedRectangle(cornerRadius: 12, style: .continuous))

                HStack {
                    if let error = viewModel.error {
                        Text(error.message)
                            .font(.caption)
                            .foregroundStyle(.red)
                    }
                    Spacer()
                    Text("\(remainingCharacters) left")
                        .font(.caption)
                        .foregroundStyle(remainingCharacters < 0 ? .red : .secondary)
                        .monospacedDigit()
                }
            }
            .padding()
            .navigationTitle("Note")
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Save") {
                        Task {
                            if await viewModel.save() {
                                dismiss()
                            }
                        }
                    }
                    .disabled(!viewModel.canSave)
                }
            }
        }
    }

    private var remainingCharacters: Int {
        NoteText.maxGraphemes - viewModel.characterCount
    }
}

#if DEBUG
#Preview {
    NoteEditorSheet(
        viewModel: NoteEditorViewModel(
            initial: "Away in Bali",
            parent: DayDetailViewModel(date: "2026-04-15", apiClient: MockFluxAPIClient.preview)
        )
    )
}
#endif
