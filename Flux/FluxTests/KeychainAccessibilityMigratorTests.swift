import FluxCore
import Foundation
import Security
import Testing
@testable import Flux

@MainActor @Suite(.serialized)
struct KeychainAccessibilityMigratorTests {
    @Test
    func runWithNoTokenReturnsFalseAndDoesNotMutate() {
        let keychain = makeKeychain()
        try? keychain.deleteToken()

        let result = KeychainAccessibilityMigrator.run(keychain: keychain)

        #expect(result == false)
        #expect(keychain.loadToken() == nil)
        #expect(keychain.readAccessibility() == nil)
    }

    @Test
    func runWithCorrectClassReturnsFalseAndDoesNotMutate() throws {
        let keychain = makeKeychain(accessibility: .afterFirstUnlockThisDeviceOnly)
        defer { try? keychain.deleteToken() }
        try keychain.saveToken("correct-token")

        let result = KeychainAccessibilityMigrator.run(keychain: keychain)

        #expect(result == false)
        #expect(keychain.loadToken() == "correct-token")
        #expect(keychain.readAccessibility() == .afterFirstUnlockThisDeviceOnly)
    }

    @Test
    func runWithOtherClassMigratesInPlaceAndPreservesToken() throws {
        let context = makeContext(initialAccessibility: .other(kSecAttrAccessibleWhenUnlocked as String))
        defer { try? context.migrator.deleteToken() }
        try context.seed.saveToken("migrate-me")

        let result = KeychainAccessibilityMigrator.run(keychain: context.migrator)

        #expect(result == true)
        #expect(context.migrator.loadToken() == "migrate-me")
        #expect(context.migrator.readAccessibility() == .afterFirstUnlockThisDeviceOnly)
    }

    @Test
    func runFallsBackToSaveWhenUpdateAccessibilityFails() throws {
        let context = makeContext(initialAccessibility: .other(kSecAttrAccessibleWhenUnlocked as String))
        defer { try? context.migrator.deleteToken() }
        try context.seed.saveToken("preserve-me")

        let result = KeychainAccessibilityMigrator.run(
            keychain: context.migrator,
            updateOverride: { _ in
                throw KeychainServiceError.unexpectedStatus(errSecIO)
            }
        )

        #expect(result == true)
        #expect(context.migrator.loadToken() == "preserve-me")
        #expect(context.migrator.readAccessibility() == .afterFirstUnlockThisDeviceOnly)
    }

    private func makeKeychain(
        accessibility: KeychainAccessibility = .afterFirstUnlockThisDeviceOnly,
        service: String = "me.nore.ig.flux.tests.\(UUID().uuidString)",
        account: String = "api-token.\(UUID().uuidString)"
    ) -> KeychainService {
        KeychainService(
            service: service,
            account: account,
            accessGroup: nil,
            accessibility: accessibility
        )
    }

    private struct Context {
        let seed: KeychainService
        let migrator: KeychainService
    }

    private func makeContext(initialAccessibility: KeychainAccessibility) -> Context {
        let service = "me.nore.ig.flux.tests.\(UUID().uuidString)"
        let account = "api-token.\(UUID().uuidString)"
        let seed = KeychainService(
            service: service,
            account: account,
            accessGroup: nil,
            accessibility: initialAccessibility
        )
        let migrator = KeychainService(
            service: service,
            account: account,
            accessGroup: nil,
            accessibility: .afterFirstUnlockThisDeviceOnly
        )
        return Context(seed: seed, migrator: migrator)
    }
}
