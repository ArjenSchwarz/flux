import FluxCore
import SwiftUI

extension ColorTier {
    var color: Color {
        switch self {
        case .green: .green
        case .red: .red
        case .orange: .orange
        case .amber: .yellow
        case .normal: .primary
        }
    }
}
