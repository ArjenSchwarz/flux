import Foundation
import Testing
@testable import FluxCore

@MainActor
@Suite(.serialized)
struct DeviceIdentifierTests {
    @Test
    func generateOnFirstReadAndPersist() {
        let defaults = makeDefaults()
        let id = DeviceIdentifier(userDefaults: defaults)
        let first = id.currentOrGenerate()
        #expect(!first.isEmpty)
        let second = id.currentOrGenerate()
        #expect(first == second, "subsequent reads must return the same value")
    }

    @Test
    func surviveAcrossInstances() {
        // Two DeviceIdentifier values backed by the same UserDefaults must
        // see the same id — simulates the app being relaunched after the
        // process exits.
        let defaults = makeDefaults()
        let id1 = DeviceIdentifier(userDefaults: defaults).currentOrGenerate()
        let id2 = DeviceIdentifier(userDefaults: defaults).currentOrGenerate()
        #expect(id1 == id2)
    }

    @Test
    func resetWhenSuiteCleared() {
        // Clearing the UserDefaults suite is the closest hermetic analogue
        // to an app uninstall (Decision 8). After clearing, a fresh UUID
        // must be issued.
        let suite = "flux.test.\(UUID().uuidString)"
        let defaults = UserDefaults(suiteName: suite) ?? .standard
        defaults.removePersistentDomain(forName: suite)

        let initial = DeviceIdentifier(userDefaults: defaults).currentOrGenerate()
        defaults.removePersistentDomain(forName: suite)
        let reset = DeviceIdentifier(userDefaults: defaults).currentOrGenerate()

        #expect(initial != reset, "uninstall analogue must produce a new id")
    }

    @Test
    func generatedValueIsAUUID() {
        let defaults = makeDefaults()
        let value = DeviceIdentifier(userDefaults: defaults).currentOrGenerate()
        #expect(UUID(uuidString: value) != nil,
                "generated identifier must be parseable as a UUID")
    }

    private func makeDefaults() -> UserDefaults {
        let suite = "flux.test.\(UUID().uuidString)"
        let defaults = UserDefaults(suiteName: suite) ?? .standard
        defaults.removePersistentDomain(forName: suite)
        return defaults
    }
}
