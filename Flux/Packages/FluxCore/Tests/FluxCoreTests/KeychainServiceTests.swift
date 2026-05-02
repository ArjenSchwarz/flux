import Foundation
import Security
import Testing
@testable import FluxCore

/// True when the test process can write `kSecAttrSynchronizable=true` items
/// (i.e. a `keychain-access-groups` entitlement is present). `swift test`
/// from the CLI returns `errSecMissingEntitlement` (-34018); Xcode's test
/// runners under the Flux scheme do have the entitlement.
private nonisolated let synchronizableKeychainSupported: Bool = {
    let probe = "flux.synchronizable-probe.\(UUID().uuidString)"
    var addQuery: [CFString: Any] = [
        kSecClass: kSecClassGenericPassword,
        kSecAttrService: probe,
        kSecAttrAccount: probe,
        kSecValueData: Data("probe".utf8),
        kSecAttrAccessible: kSecAttrAccessibleAfterFirstUnlock,
        kSecAttrSynchronizable: kCFBooleanTrue as Any
    ]
    let status = SecItemAdd(addQuery as CFDictionary, nil)
    if status == errSecSuccess {
        var deleteQuery: [CFString: Any] = [
            kSecClass: kSecClassGenericPassword,
            kSecAttrService: probe,
            kSecAttrAccount: probe,
            kSecAttrSynchronizable: kSecAttrSynchronizableAny
        ]
        SecItemDelete(deleteQuery as CFDictionary)
        return true
    }
    return false
}()

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

    #if !os(macOS)
    // macOS keychain does not preserve kSecAttrAccessible for items without an
    // access group, so these assertions only hold on iOS — which is fine
    // because both helpers exist solely to support the iOS-only
    // KeychainAccessibilityMigrator.
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
    #endif

    @Test
    func updateAccessibilityReturnsFalseWhenNoItemExists() throws {
        let service = makeService()

        let updated = try service.updateAccessibility(.afterFirstUnlockThisDeviceOnly)

        #expect(updated == false)
    }

    @Test(.enabled(if: synchronizableKeychainSupported, "requires keychain-access-groups entitlement"))
    func loadTokenFindsSynchronizableItemWrittenDirectly() throws {
        let ids = makeUniqueIds()
        defer { cleanupAllVariants(service: ids.service, account: ids.account) }
        let keychain = KeychainService(
            service: ids.service,
            account: ids.account,
            accessGroup: nil
        )

        writeRawItem(
            service: ids.service,
            account: ids.account,
            value: "sync-token",
            synchronizable: true,
            accessibility: kSecAttrAccessibleAfterFirstUnlock
        )

        #expect(keychain.loadToken() == "sync-token")
    }

    @Test
    func loadTokenFindsLegacyNonSynchronizableItem() throws {
        let ids = makeUniqueIds()
        defer { cleanupAllVariants(service: ids.service, account: ids.account) }
        let keychain = KeychainService(
            service: ids.service,
            account: ids.account,
            accessGroup: nil
        )

        writeRawItem(
            service: ids.service,
            account: ids.account,
            value: "legacy-token",
            synchronizable: false,
            accessibility: kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly
        )

        #expect(keychain.loadToken() == "legacy-token")
    }

    @Test(.enabled(if: synchronizableKeychainSupported, "requires keychain-access-groups entitlement"))
    func deleteTokenRemovesBothLegacyAndSynchronizableVariants() throws {
        let ids = makeUniqueIds()
        defer { cleanupAllVariants(service: ids.service, account: ids.account) }
        let keychain = KeychainService(
            service: ids.service,
            account: ids.account,
            accessGroup: nil
        )

        writeRawItem(
            service: ids.service,
            account: ids.account,
            value: "legacy",
            synchronizable: false,
            accessibility: kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly
        )
        writeRawItem(
            service: ids.service,
            account: ids.account,
            value: "sync",
            synchronizable: true,
            accessibility: kSecAttrAccessibleAfterFirstUnlock
        )

        try keychain.deleteToken()

        #expect(countItemsAcrossVariants(service: ids.service, account: ids.account) == 0)
    }

    @Test(.enabled(if: synchronizableKeychainSupported, "requires keychain-access-groups entitlement"))
    func saveTokenConvergesToSingleSynchronizableItem() throws {
        let ids = makeUniqueIds()
        defer { cleanupAllVariants(service: ids.service, account: ids.account) }
        let keychain = KeychainService(
            service: ids.service,
            account: ids.account,
            accessGroup: nil
        )

        writeRawItem(
            service: ids.service,
            account: ids.account,
            value: "old-legacy",
            synchronizable: false,
            accessibility: kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly
        )
        writeRawItem(
            service: ids.service,
            account: ids.account,
            value: "old-sync",
            synchronizable: true,
            accessibility: kSecAttrAccessibleAfterFirstUnlock
        )

        try keychain.saveToken("new")

        #expect(countItemsAcrossVariants(service: ids.service, account: ids.account) == 1)
        #expect(keychain.loadToken() == "new")
        #expect(readSynchronizableFlag(service: ids.service, account: ids.account) == true)
    }

    private func makeService(
        accessibility: KeychainAccessibility = .afterFirstUnlockThisDeviceOnly,
        synchronizable: Bool = false
    ) -> KeychainService {
        let ids = makeUniqueIds()
        return KeychainService(
            service: ids.service,
            account: ids.account,
            accessGroup: nil,
            accessibility: accessibility,
            synchronizable: synchronizable
        )
    }

    private func makeUniqueIds() -> (service: String, account: String) {
        (
            "me.nore.ig.flux.tests.\(UUID().uuidString)",
            "api-token.\(UUID().uuidString)"
        )
    }

    private func baseQuery(service: String, account: String) -> [CFString: Any] {
        [
            kSecClass: kSecClassGenericPassword,
            kSecAttrService: service,
            kSecAttrAccount: account
        ]
    }

    private func writeRawItem(
        service: String,
        account: String,
        value: String,
        synchronizable: Bool,
        accessibility: CFString
    ) {
        var query = baseQuery(service: service, account: account)
        query[kSecValueData] = Data(value.utf8)
        query[kSecAttrAccessible] = accessibility
        query[kSecAttrSynchronizable] = synchronizable ? kCFBooleanTrue : kCFBooleanFalse
        SecItemAdd(query as CFDictionary, nil)
    }

    private func countItemsAcrossVariants(service: String, account: String) -> Int {
        var query = baseQuery(service: service, account: account)
        query[kSecAttrSynchronizable] = kSecAttrSynchronizableAny
        query[kSecMatchLimit] = kSecMatchLimitAll
        query[kSecReturnAttributes] = kCFBooleanTrue

        var item: CFTypeRef?
        let status = SecItemCopyMatching(query as CFDictionary, &item)
        if status == errSecItemNotFound { return 0 }
        guard status == errSecSuccess else { return -1 }
        return (item as? [[String: Any]])?.count ?? 0
    }

    private func readSynchronizableFlag(service: String, account: String) -> Bool? {
        var query = baseQuery(service: service, account: account)
        query[kSecAttrSynchronizable] = kSecAttrSynchronizableAny
        query[kSecMatchLimit] = kSecMatchLimitOne
        query[kSecReturnAttributes] = kCFBooleanTrue

        var item: CFTypeRef?
        let status = SecItemCopyMatching(query as CFDictionary, &item)
        guard status == errSecSuccess, let attributes = item as? [String: Any] else {
            return nil
        }
        return (attributes[kSecAttrSynchronizable as String] as? Bool) ?? false
    }

    private func cleanupAllVariants(service: String, account: String) {
        var query = baseQuery(service: service, account: account)
        query[kSecAttrSynchronizable] = kSecAttrSynchronizableAny
        SecItemDelete(query as CFDictionary)
    }
}
