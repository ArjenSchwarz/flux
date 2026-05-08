import Foundation
import Testing
@testable import FluxCore

@Suite
struct WhatsNewVersionTests {
    @Test
    func tenIsGreaterThanNine() throws {
        let v110 = try #require(WhatsNewVersion("1.10"))
        let v19 = try #require(WhatsNewVersion("1.9"))
        #expect(v110 > v19)
    }

    @Test
    func twoComponentEqualsThreeComponentWhenTrailingZero() throws {
        let v12 = try #require(WhatsNewVersion("1.2"))
        let v120 = try #require(WhatsNewVersion("1.2.0"))
        #expect(v12 == v120)
        #expect(v120 == v12)
    }

    @Test
    func singleComponentEqualsTwoComponentWhenTrailingZero() throws {
        let v1 = try #require(WhatsNewVersion("1"))
        let v10 = try #require(WhatsNewVersion("1.0"))
        #expect(v1 == v10)
        #expect(v10 == v1)
    }

    @Test
    func majorBumpBeatsLargeMinor() throws {
        let v20 = try #require(WhatsNewVersion("2.0"))
        let v199 = try #require(WhatsNewVersion("1.99"))
        #expect(v20 > v199)
    }

    @Test
    func nonNumericComponentReturnsNil() {
        #expect(WhatsNewVersion("1.x") == nil)
    }

    @Test
    func emptyStringReturnsNil() {
        #expect(WhatsNewVersion("") == nil)
    }

    @Test
    func sortDescendingMatchesNumericOrdering() throws {
        let inputs = ["1.0", "1.10", "1.2", "2.0", "1.9.1"]
        let parsed = inputs.compactMap(WhatsNewVersion.init)
        #expect(parsed.count == inputs.count)

        let sorted = parsed.sorted(by: >)
        let expected = ["2.0", "1.10", "1.9.1", "1.2", "1.0"].compactMap(WhatsNewVersion.init)
        #expect(sorted == expected)
    }
}
