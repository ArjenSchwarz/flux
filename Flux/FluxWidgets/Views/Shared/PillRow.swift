import SwiftUI

struct PillRow: View {
    let title: String
    let value: String
    let color: Color
    var font: Font = .subheadline
    var redacted: Bool = false

    var body: some View {
        HStack(spacing: 8) {
            Text(title)
                .foregroundStyle(.secondary)
                .lineLimit(1)
            Spacer(minLength: 4)
            Text(value)
                .monospacedDigit()
                .foregroundStyle(color)
                .lineLimit(1)
                .padding(.horizontal, 8)
                .padding(.vertical, 3)
                .background(color.opacity(0.15), in: Capsule())
                .redacted(reason: redacted ? .placeholder : [])
        }
        .font(font)
    }
}
