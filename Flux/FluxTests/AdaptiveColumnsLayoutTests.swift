import SwiftUI
import Testing
@testable import Flux

@MainActor
struct AdaptiveColumnsLayoutTests {
    @Test
    func widthBelow700ReturnsSingleColumn() {
        let layout = AdaptiveColumnsLayout { EmptyView() }
        #expect(layout.columnCount(width: 699, typeSize: .large) == 1)
        #expect(layout.columnCount(width: 0, typeSize: .large) == 1)
    }

    @Test
    func widthAt700ReturnsTwoColumns() {
        let layout = AdaptiveColumnsLayout { EmptyView() }
        #expect(layout.columnCount(width: 700, typeSize: .large) == 2)
    }

    @Test
    func widthBelow1000ReturnsTwoColumns() {
        let layout = AdaptiveColumnsLayout { EmptyView() }
        #expect(layout.columnCount(width: 999, typeSize: .large) == 2)
    }

    @Test
    func widthAt1000ReturnsThreeColumns() {
        let layout = AdaptiveColumnsLayout { EmptyView() }
        #expect(layout.columnCount(width: 1000, typeSize: .large) == 3)
    }

    @Test
    func wideWidthStaysAtThreeColumns() {
        let layout = AdaptiveColumnsLayout { EmptyView() }
        #expect(layout.columnCount(width: 1600, typeSize: .large) == 3)
    }

    @Test
    func accessibility3KeepsBaseColumns() {
        let layout = AdaptiveColumnsLayout { EmptyView() }
        #expect(layout.columnCount(width: 700, typeSize: .accessibility3) == 2)
        #expect(layout.columnCount(width: 1000, typeSize: .accessibility3) == 3)
    }

    @Test
    func accessibility4DropsOneColumn() {
        let layout = AdaptiveColumnsLayout { EmptyView() }
        #expect(layout.columnCount(width: 700, typeSize: .accessibility4) == 1)
        #expect(layout.columnCount(width: 1000, typeSize: .accessibility4) == 2)
    }

    @Test
    func accessibility5DropsOneColumnAndNeverGoesBelowOne() {
        let layout = AdaptiveColumnsLayout { EmptyView() }
        #expect(layout.columnCount(width: 500, typeSize: .accessibility5) == 1)
        #expect(layout.columnCount(width: 700, typeSize: .accessibility5) == 1)
        #expect(layout.columnCount(width: 1000, typeSize: .accessibility5) == 2)
    }
}
