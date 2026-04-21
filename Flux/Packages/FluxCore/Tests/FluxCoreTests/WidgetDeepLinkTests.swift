import Foundation
import Testing
@testable import FluxCore

@Suite
struct WidgetDeepLinkTests {
    @Test
    func parseDashboardURLReturnsDashboard() throws {
        let url = try #require(URL(string: "flux://dashboard"))

        #expect(WidgetDeepLink.parse(url) == .dashboard)
    }

    @Test
    func parseDashboardURLWithExtraPathIgnoresExtra() throws {
        let url = try #require(URL(string: "flux://dashboard/extra"))

        #expect(WidgetDeepLink.parse(url) == .dashboard)
    }

    @Test
    func parseUnknownHostReturnsNil() throws {
        let url = try #require(URL(string: "flux://unknown"))

        #expect(WidgetDeepLink.parse(url) == nil)
    }

    @Test
    func parseWrongSchemeReturnsNil() throws {
        let url = try #require(URL(string: "other://dashboard"))

        #expect(WidgetDeepLink.parse(url) == nil)
    }

    @Test
    func dashboardURLIsExpected() throws {
        let expected = try #require(URL(string: "flux://dashboard"))

        #expect(WidgetDeepLink.dashboardURL == expected)
    }
}
