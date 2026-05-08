import Foundation
import Testing
@testable import FluxCore

@Suite
struct UserDefaultsWhatsNewTests {
    private func makeSuiteName() -> String {
        "test.whatsnew.\(UUID().uuidString)"
    }

    private func clearSuite(_ name: String) {
        UserDefaults(suiteName: name)?.removePersistentDomain(forName: name)
    }

    @Test
    func lastSeenWhatsNewVersionIsNilByDefault() throws {
        let name = makeSuiteName()
        defer { clearSuite(name) }
        let defaults = try #require(UserDefaults(suiteName: name))

        #expect(defaults.lastSeenWhatsNewVersion == nil)
    }

    @Test
    func lastSeenWhatsNewVersionRoundTrips() throws {
        let name = makeSuiteName()
        defer { clearSuite(name) }
        let defaults = try #require(UserDefaults(suiteName: name))

        defaults.lastSeenWhatsNewVersion = "1.2"
        #expect(defaults.lastSeenWhatsNewVersion == "1.2")
    }

    @Test
    func hasAnyFluxPreferenceWrittenIsFalseOnCleanInstance() throws {
        let name = makeSuiteName()
        defer { clearSuite(name) }
        let defaults = try #require(UserDefaults(suiteName: name))

        #expect(defaults.hasAnyFluxPreferenceWritten == false)
    }

    @Test(arguments: [
        UserDefaults.apiURLKey,
        UserDefaults.themeIdentifierKey,
        UserDefaults.appFontFamilyKey,
        UserDefaults.loadAlertThresholdKey,
        UserDefaults.widgetUsesSymbolsKey
    ])
    func hasAnyFluxPreferenceWrittenIsTrueAfterAnyKnownKey(key: String) throws {
        let name = makeSuiteName()
        defer { clearSuite(name) }
        let defaults = try #require(UserDefaults(suiteName: name))

        defaults.set("value", forKey: key)
        #expect(defaults.hasAnyFluxPreferenceWritten == true)
    }
}
