import FluxCore
import SwiftUI

/// One exception window in the pricing editor: its boundaries, whether it is
/// free, and — when it isn't — the rate it carries. Extracted from
/// `PricingEditor` so the sheet stays readable as the number of sections grew.
@MainActor
struct PricingWindowRow: View {
    @Bindable var viewModel: PricingViewModel
    let index: Int

    var body: some View {
        VStack(spacing: 8) {
            HStack {
                bandTimePicker(label: "From", isStart: true)
                Spacer(minLength: 12)
                bandTimePicker(label: "To", isStart: false)
            }
            Toggle("Free", isOn: Binding(
                get: { window?.free ?? false },
                set: { viewModel.setWindowFree($0, at: index) }
            ))
            if window?.free == false {
                rateField
            }
            Button(role: .destructive) {
                viewModel.removeWindow(at: index)
            } label: {
                Label("Remove window", systemImage: "minus.circle")
                    .font(.footnote)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .padding(.vertical, 4)
    }

    /// Nil while SwiftUI is still rendering a row whose window has just been
    /// removed — reading the array directly would trap on the stale index.
    private var window: PlanWindow? {
        viewModel.draft.windows.indices.contains(index) ? viewModel.draft.windows[index] : nil
    }

    private var rateField: some View {
        HStack {
            Text("Rate")
            Spacer()
            TextField(
                "0.0000",
                value: Binding(
                    get: { window?.rate ?? 0 },
                    set: { newValue in
                        guard viewModel.draft.windows.indices.contains(index) else { return }
                        viewModel.draft.windows[index].rate = newValue
                    }
                ),
                format: .number.precision(.fractionLength(4))
            )
            #if os(iOS)
            .keyboardType(.decimalPad)
            #endif
            .multilineTextAlignment(.trailing)
            .frame(maxWidth: 120)
            Text("/ kWh")
                .foregroundStyle(.secondary)
        }
    }

    private func bandTimePicker(label: String, isStart: Bool) -> some View {
        DatePicker(
            label,
            selection: Binding(
                get: {
                    let value = isStart ? window?.start : window?.end
                    return value.flatMap(PricingEditor.parseBandTime) ?? PricingEditor.bandTimeReference
                },
                set: { newValue in
                    guard viewModel.draft.windows.indices.contains(index) else { return }
                    let formatted = PricingEditor.formatBandTime(newValue)
                    if isStart {
                        viewModel.draft.windows[index].start = formatted
                    } else {
                        viewModel.draft.windows[index].end = formatted
                    }
                }
            ),
            displayedComponents: .hourAndMinute
        )
        .labelsHidden()
        .accessibilityLabel("\(label) time")
    }
}
