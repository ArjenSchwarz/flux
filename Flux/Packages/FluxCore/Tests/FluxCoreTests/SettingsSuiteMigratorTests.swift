import Foundation
import Testing
@testable import FluxCore

@Suite
struct SettingsSuiteMigratorTests {
    private func makeSuiteName() -> String {
        "test.settings.\(UUID().uuidString)"
    }

    private func clearSuite(_ name: String) {
        UserDefaults(suiteName: name)?.removePersistentDomain(forName: name)
    }

    @Test
    func copiesApiURLAndThresholdFromStandardToEmptySuite() throws {
        let standardName = makeSuiteName()
        let suiteName = makeSuiteName()
        defer {
            clearSuite(standardName)
            clearSuite(suiteName)
        }
        let standard = try #require(UserDefaults(suiteName: standardName))
        let suite = try #require(UserDefaults(suiteName: suiteName))
        standard.set("https://api.example.com", forKey: "apiURL")
        standard.set(4500.0, forKey: "loadAlertThreshold")

        let copied = SettingsSuiteMigrator.run(standard: standard, suite: suite)

        #expect(copied == true)
        #expect(suite.string(forKey: "apiURL") == "https://api.example.com")
        #expect(suite.double(forKey: "loadAlertThreshold") == 4500.0)
        #expect(suite.integer(forKey: "settingsMigrationVersion") == 1)
    }

    @Test
    func secondRunIsIdempotent() throws {
        let standardName = makeSuiteName()
        let suiteName = makeSuiteName()
        defer {
            clearSuite(standardName)
            clearSuite(suiteName)
        }
        let standard = try #require(UserDefaults(suiteName: standardName))
        let suite = try #require(UserDefaults(suiteName: suiteName))
        standard.set("https://api.example.com", forKey: "apiURL")
        standard.set(4500.0, forKey: "loadAlertThreshold")

        let firstRun = SettingsSuiteMigrator.run(standard: standard, suite: suite)
        // Mutate standard afterwards — migrator should NOT re-copy.
        standard.set("https://other.example.com", forKey: "apiURL")
        let secondRun = SettingsSuiteMigrator.run(standard: standard, suite: suite)

        #expect(firstRun == true)
        #expect(secondRun == false)
        #expect(suite.string(forKey: "apiURL") == "https://api.example.com")
    }

    @Test
    func freshInstallWritesVersionWithoutCopy() throws {
        let standardName = makeSuiteName()
        let suiteName = makeSuiteName()
        defer {
            clearSuite(standardName)
            clearSuite(suiteName)
        }
        let standard = try #require(UserDefaults(suiteName: standardName))
        let suite = try #require(UserDefaults(suiteName: suiteName))

        let copied = SettingsSuiteMigrator.run(standard: standard, suite: suite)

        #expect(copied == false)
        #expect(suite.integer(forKey: "settingsMigrationVersion") == 1)
        #expect(suite.string(forKey: "apiURL") == nil)
        #expect(suite.object(forKey: "loadAlertThreshold") == nil)
    }

    @Test
    func doesNotOverwriteSuiteWhenItAlreadyHasApiURL() throws {
        let standardName = makeSuiteName()
        let suiteName = makeSuiteName()
        defer {
            clearSuite(standardName)
            clearSuite(suiteName)
        }
        let standard = try #require(UserDefaults(suiteName: standardName))
        let suite = try #require(UserDefaults(suiteName: suiteName))
        standard.set("https://api.example.com", forKey: "apiURL")
        suite.set("https://suite.example.com", forKey: "apiURL")

        _ = SettingsSuiteMigrator.run(standard: standard, suite: suite)

        #expect(suite.string(forKey: "apiURL") == "https://suite.example.com")
    }
}
