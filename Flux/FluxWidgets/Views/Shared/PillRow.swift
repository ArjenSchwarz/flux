import SwiftUI

struct PillRow: View {
    let title: String
    let value: String
    let color: Color
    /// Font applied to the value text. The title is always rendered at
    /// `.subheadline` so the row has a smaller-label / larger-value rhythm when
    /// `valueFont` is something bigger like `.body`.
    var valueFont: Font = .subheadline
    var redacted: Bool = false
    var tight: Bool = false

    var body: some View {
        HStack(spacing: 8) {
            Text(title)
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .lineLimit(1)
            if !tight {
                Spacer(minLength: 4)
            }
            Text(value)
                .font(valueFont)
                .monospacedDigit()
                .foregroundStyle(color)
                .lineLimit(1)
                .redacted(reason: redacted ? .placeholder : [])
        }
    }
}
