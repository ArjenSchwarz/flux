import SwiftUI

struct PillRow: View {
    let title: String
    let value: String
    let color: Color
    var font: Font = .subheadline
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
                .font(font)
                .monospacedDigit()
                .foregroundStyle(color)
                .lineLimit(1)
                .redacted(reason: redacted ? .placeholder : [])
        }
    }
}
