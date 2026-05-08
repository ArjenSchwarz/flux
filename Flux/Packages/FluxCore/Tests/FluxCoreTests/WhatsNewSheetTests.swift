import Foundation
import SwiftUI
import Testing
@testable import FluxCore

#if canImport(UIKit)
import UIKit
#elseif canImport(AppKit)
import AppKit
#endif

@MainActor
@Suite
struct WhatsNewSheetTests {
    private static let date = Date(timeIntervalSince1970: 0)

    private static let multiRelease: [WhatsNewRelease] = [
        WhatsNewRelease(version: "1.2", date: date, highlights: [
            Highlight(
                category: .new,
                title: "Battery cutoff predictions",
                detail: "Shows when the battery will run out."
            ),
            Highlight(
                category: .improved,
                title: "Faster dashboard refresh",
                detail: "Cuts wait time in half."
            )
        ]),
        WhatsNewRelease(version: "1.1", date: date, highlights: [
            Highlight(category: .fixed, title: "Off-peak window edge case", detail: nil)
        ])
    ]

    private static let singleRelease: [WhatsNewRelease] = [
        WhatsNewRelease(version: "1.1", date: date, highlights: [
            Highlight(category: .new, title: "What's New sheet", detail: "Discover changes after each update.")
        ])
    ]

    private static let detailFreeRelease: [WhatsNewRelease] = [
        WhatsNewRelease(version: "1.1", date: date, highlights: [
            Highlight(category: .new, title: "Title only"),
            Highlight(category: .improved, title: "Another title only"),
            Highlight(category: .fixed, title: "Yet another title")
        ])
    ]

    @Test
    func multiReleaseRendersWithoutCrashing() {
        evaluate(WhatsNewSheet(releases: Self.multiRelease))
    }

    @Test
    func singleReleaseRendersWithoutCrashing() {
        evaluate(WhatsNewSheet(releases: Self.singleRelease))
    }

    @Test
    func releaseWithNoDetailStringsRendersWithoutCrashing() {
        evaluate(WhatsNewSheet(releases: Self.detailFreeRelease))
    }

    private func evaluate<V: View>(_ view: V) {
        #if canImport(UIKit)
        let host = UIHostingController(rootView: view)
        host.loadViewIfNeeded()
        #expect(host.view != nil)
        #elseif canImport(AppKit)
        let host = NSHostingController(rootView: view)
        _ = host.view
        #expect(host.view.frame.size.width >= 0)
        #endif
    }
}
