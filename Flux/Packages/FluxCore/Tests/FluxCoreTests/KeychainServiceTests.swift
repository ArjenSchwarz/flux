import Foundation
import Security
import Testing
@testable import FluxCore

@MainActor @Suite(.serialized)
struct KeychainServiceTests {
    @Test
    func saveAndLoadTokenRoundTrip() throws {
        let service = makeService()
        defer { try? service.deleteToken() }

        try service.saveToken("secret-token")

        #expect(service.loadToken() == "secret-token")
    }

    @Test
    func deleteTokenRemovesStoredToken() throws {
        let service = makeService()
        defer { try? service.deleteToken() }
        try service.saveToken("secret-token")

        try service.deleteToken()

        #expect(service.loadToken() == nil)
    }

    @Test
    func loadTokenReturnsNilWhenMissing() {
        let service = makeService()
        try? service.deleteToken()

        #expect(service.loadToken() == nil)
    }

    @Test
    func accessibilityEquatability() {
        #expect(KeychainAccessibility.afterFirstUnlockThisDeviceOnly
                == KeychainAccessibility.afterFirstUnlockThisDeviceOnly)
        #expect(KeychainAccessibility.afterFirstUnlockThisDeviceOnly
                != KeychainAccessibility.other("foo"))
        #expect(KeychainAccessibility.other("a") != KeychainAccessibility.other("b"))
        #expect(KeychainAccessibility.other("a") == KeychainAccessibility.other("a"))
    }

    @Test
    func readAccessibilityReturnsNilWhenMissing() {
        let service = makeService()
        try? service.deleteToken()

        #expect(service.readAccessibility() == nil)
    }

    @Test
    func readAccessibilityReflectsSavedAccessibility() throws {
        let service = makeService(accessibility: .afterFirstUnlockThisDeviceOnly)
        defer { try? service.deleteToken() }

        try service.saveToken("secret-token")

        #expect(service.readAccessibility() == .afterFirstUnlockThisDeviceOnly)
    }

    @Test
    func updateAccessibilityChangesClassWithoutLosingToken() throws {
        let service = makeService(accessibility: .other(kSecAttrAccessibleWhenUnlocked as String))
        defer { try? service.deleteToken() }
        try service.saveToken("secret-token")

        let updated = try service.updateAccessibility(.afterFirstUnlockThisDeviceOnly)

        #expect(updated == true)
        #expect(service.loadToken() == "secret-token")
        #expect(service.readAccessibility() == .afterFirstUnlockThisDeviceOnly)
    }

    @Test
    func updateAccessibilityReturnsFalseWhenNoItemExists() throws {
        let service = makeService()
        try? service.deleteToken()

        let updated = try service.updateAccessibility(.afterFirstUnlockThisDeviceOnly)

        #expect(updated == false)
    }

    private func makeService(
        accessibility: KeychainAccessibility = .afterFirstUnlockThisDeviceOnly
    ) -> KeychainService {
        KeychainService(
            service: "me.nore.ig.flux.tests.\(UUID().uuidString)",
            account: "api-token.\(UUID().uuidString)",
            accessGroup: nil,
            accessibility: accessibility
        )
    }
}
