import Foundation
import Testing
@testable import FluxCore

@Suite
struct APNsEnvironmentTests {
    @Test
    func currentReturnsKnownValue() {
        // The test host runs without an aps-environment entitlement, so the
        // fallback path applies. Either way the value must be one of the
        // two known strings — anything else would be a programming error.
        let env = APNsEnvironment.current()
        #expect(env == APNsEnvironment.development || env == APNsEnvironment.production,
                "current() must return one of the two known APNs environment strings; got \(env)")
    }

    @Test
    func constantsMatchBackendWireValues() {
        // The backend's validation rejects anything other than these two
        // strings, so the constants must match exactly.
        #expect(APNsEnvironment.development == "development")
        #expect(APNsEnvironment.production == "production")
    }
}
