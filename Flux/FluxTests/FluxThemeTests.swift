#if canImport(UIKit)
import SwiftUI
import Testing
import UIKit
@testable import Flux

@Suite
struct FluxThemeTests {
    @Test("Adaptive text colour resolves off the main actor in light and dark mode")
    @MainActor
    func adaptiveColourResolvesOnBackgroundRenderer() async {
        let colour = UIColor(FluxTheme.Palette.primaryText)

        // SwiftUI's async renderer resolves dynamic colours away from the main
        // actor. The provider must not inherit the actor that created it.
        await Task.detached {
            Self.checkBackgroundResolution(colour)
        }.value
    }

    nonisolated private static func checkBackgroundResolution(_ colour: UIColor) {
        #expect(!Thread.isMainThread)
        for (style, expectedWhite) in [(UIUserInterfaceStyle.light, CGFloat(0)), (.dark, CGFloat(1))] {
            let resolved = colour.resolvedColor(with: UITraitCollection(userInterfaceStyle: style))
            var white: CGFloat = -1
            var alpha: CGFloat = -1
            #expect(resolved.getWhite(&white, alpha: &alpha))
            #expect(abs(white - expectedWhite) < 0.001)
            #expect(abs(alpha - 1) < 0.001)
        }
    }
}
#endif
