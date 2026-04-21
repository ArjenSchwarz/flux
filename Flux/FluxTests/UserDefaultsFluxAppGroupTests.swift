import FluxCore
import Foundation
import Testing

@MainActor @Suite(.serialized)
struct UserDefaultsFluxAppGroupTests {
    @Test
    func apiURLWritesAndReadsFromSuite() {
        let suite = makeSuite()
        defer { cleanUp(suite) }

        suite.apiURL = "https://api.example.com"

        #expect(suite.string(forKey: "apiURL") == "https://api.example.com")
        #expect(suite.apiURL == "https://api.example.com")
    }

    @Test
    func apiURLReturnsNilWhenSuiteEmptyAndStandardEmpty() {
        let suite = makeSuite()
        defer { cleanUp(suite) }
        UserDefaults.standard.removeObject(forKey: "apiURL")

        #expect(suite.apiURL == nil)
    }

    @Test
    func apiURLFallsBackToStandardWhenSuiteEmpty() {
        let suite = makeSuite()
        defer {
            cleanUp(suite)
            UserDefaults.standard.removeObject(forKey: "apiURL")
        }
        UserDefaults.standard.set("https://legacy.example.com", forKey: "apiURL")

        #expect(suite.apiURL == "https://legacy.example.com")
    }

    @Test
    func loadAlertThresholdDefaultIs3000WhenSuiteEmpty() {
        let suite = makeSuite()
        defer {
            cleanUp(suite)
            UserDefaults.standard.removeObject(forKey: "loadAlertThreshold")
        }
        UserDefaults.standard.removeObject(forKey: "loadAlertThreshold")

        #expect(suite.loadAlertThreshold == 3000)
    }

    @Test
    func loadAlertThresholdReturnsStoredSuiteValue() {
        let suite = makeSuite()
        defer { cleanUp(suite) }

        suite.loadAlertThreshold = 4200

        #expect(suite.double(forKey: "loadAlertThreshold") == 4200)
        #expect(suite.loadAlertThreshold == 4200)
    }

    @Test
    func loadAlertThresholdFallsBackToStandardWhenSuiteEmpty() {
        let suite = makeSuite()
        defer {
            cleanUp(suite)
            UserDefaults.standard.removeObject(forKey: "loadAlertThreshold")
        }
        UserDefaults.standard.set(5100.0, forKey: "loadAlertThreshold")

        #expect(suite.loadAlertThreshold == 5100)
    }

    private let suiteName = "UserDefaultsFluxAppGroupTests.\(UUID().uuidString)"

    private func makeSuite() -> UserDefaults {
        let defaults = UserDefaults(suiteName: suiteName)!
        defaults.removePersistentDomain(forName: suiteName)
        return defaults
    }

    private func cleanUp(_ suite: UserDefaults) {
        suite.removePersistentDomain(forName: suiteName)
    }
}
