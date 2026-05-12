import Foundation
import Testing
@testable import Flux

@Suite
struct ChartKindTests {
    @Test("ChartKind round-trips through JSON for every case")
    func chartKindCodableRoundTripCoversAllCases() throws {
        let encoder = JSONEncoder()
        let decoder = JSONDecoder()

        for kind in ChartKind.allCases {
            let data = try encoder.encode(kind)
            let decoded = try decoder.decode(ChartKind.self, from: data)
            #expect(decoded == kind)
        }
    }

    @Test("ChartKind encodes its raw string value")
    func chartKindEncodesRawString() throws {
        let data = try JSONEncoder().encode(ChartKind.historySolar)
        let json = String(data: data, encoding: .utf8)
        #expect(json == "\"historySolar\"")
    }

    @Test("Distinct ChartKinds are distinct under Hashable")
    func chartKindHashableDistinguishesCases() {
        let set = Set(ChartKind.allCases)
        #expect(set.count == ChartKind.allCases.count)
    }

    @Test("ChartScope.historyRange round-trips through JSON")
    func chartScopeHistoryRangeRoundTrips() throws {
        let scope = ChartScope.historyRange(days: 14)
        let data = try JSONEncoder().encode(scope)
        let decoded = try JSONDecoder().decode(ChartScope.self, from: data)
        #expect(decoded == scope)
    }

    @Test("ChartScope.daySpecific round-trips through JSON")
    func chartScopeDaySpecificRoundTrips() throws {
        let date = Date(timeIntervalSince1970: 1_700_000_000)
        let scope = ChartScope.daySpecific(date: date)
        let data = try JSONEncoder().encode(scope)
        let decoded = try JSONDecoder().decode(ChartScope.self, from: data)
        #expect(decoded == scope)
    }

    @Test("ChartScope distinguishes cases under Hashable")
    func chartScopeHashableDistinguishesCases() {
        let date = Date(timeIntervalSince1970: 1_700_000_000)
        let scopes: Set<ChartScope> = [
            .historyRange(days: 7),
            .historyRange(days: 14),
            .historyRange(days: 30),
            .daySpecific(date: date)
        ]
        #expect(scopes.count == 4)
    }

    @Test("ChartScope.historyRange differing only in days are not equal")
    func chartScopeHistoryRangeDiffersByDays() {
        #expect(ChartScope.historyRange(days: 7) != ChartScope.historyRange(days: 14))
    }
}
