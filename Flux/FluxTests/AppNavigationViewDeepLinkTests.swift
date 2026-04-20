import Foundation
import Testing
@testable import Flux

@MainActor @Suite
struct AppNavigationViewDeepLinkTests {
    @Test
    func fluxDashboardSelectsDashboardAndResetsPath() throws {
        let url = try #require(URL(string: "flux://dashboard"))

        let action = DeepLinkHandler.handle(url)

        #expect(action == .navigate(.dashboard))
    }

    @Test
    func fluxUnknownHostLeavesStateUnchanged() throws {
        let url = try #require(URL(string: "flux://unknown"))

        let action = DeepLinkHandler.handle(url)

        #expect(action == .none)
    }

    @Test
    func differentSchemeLeavesStateUnchanged() throws {
        let url = try #require(URL(string: "other://dashboard"))

        let action = DeepLinkHandler.handle(url)

        #expect(action == .none)
    }
}
