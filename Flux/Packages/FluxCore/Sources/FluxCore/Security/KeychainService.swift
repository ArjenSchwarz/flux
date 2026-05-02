import Foundation
import Security

public enum KeychainServiceError: Error, Sendable, Equatable {
    case unexpectedStatus(OSStatus)
}

public enum KeychainAccessibility: Sendable, Equatable {
    case afterFirstUnlockThisDeviceOnly
    case afterFirstUnlock
    case other(String)
    case missing

    init(cfString: CFString) {
        let raw = cfString as String
        if raw == (kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly as String) {
            self = .afterFirstUnlockThisDeviceOnly
        } else if raw == (kSecAttrAccessibleAfterFirstUnlock as String) {
            self = .afterFirstUnlock
        } else {
            self = .other(raw)
        }
    }

    var cfString: CFString {
        switch self {
        case .afterFirstUnlockThisDeviceOnly:
            return kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly
        case .afterFirstUnlock:
            return kSecAttrAccessibleAfterFirstUnlock
        case .other(let raw):
            return raw as CFString
        case .missing:
            return kSecAttrAccessibleAfterFirstUnlock
        }
    }
}

public final class KeychainService: Sendable {
    private let service: String
    private let account: String
    private let accessGroup: String?
    private let accessibility: KeychainAccessibility
    private let synchronizable: Bool

    public init(
        service: String = "me.nore.ig.flux",
        account: String = "api-token",
        accessGroup: String? = "group.me.nore.ig.flux",
        accessibility: KeychainAccessibility = .afterFirstUnlock,
        synchronizable: Bool = true
    ) {
        self.service = service
        self.account = account
        self.accessGroup = accessGroup
        self.accessibility = accessibility
        self.synchronizable = synchronizable
    }

    public func saveToken(_ token: String) throws {
        try deleteToken()

        var query = keychainQuery()
        query[kSecValueData] = Data(token.utf8)
        query[kSecAttrAccessible] = accessibility.cfString
        query[kSecAttrSynchronizable] = synchronizable ? kCFBooleanTrue : kCFBooleanFalse

        let status = SecItemAdd(query as CFDictionary, nil)
        guard status == errSecSuccess else {
            throw KeychainServiceError.unexpectedStatus(status)
        }
    }

    public func loadToken() -> String? {
        var query = keychainQuery()
        query[kSecReturnData] = kCFBooleanTrue
        query[kSecMatchLimit] = kSecMatchLimitOne
        query[kSecAttrSynchronizable] = kSecAttrSynchronizableAny

        var item: CFTypeRef?
        let status = SecItemCopyMatching(query as CFDictionary, &item)

        guard status == errSecSuccess else {
            return nil
        }

        guard let data = item as? Data else {
            return nil
        }

        return String(data: data, encoding: .utf8)
    }

    public func deleteToken() throws {
        var query = keychainQuery()
        query[kSecAttrSynchronizable] = kSecAttrSynchronizableAny
        let status = SecItemDelete(query as CFDictionary)
        guard status == errSecSuccess || status == errSecItemNotFound else {
            throw KeychainServiceError.unexpectedStatus(status)
        }
    }

    public func readAccessibility() -> KeychainAccessibility? {
        var query = keychainQuery()
        query[kSecReturnAttributes] = kCFBooleanTrue
        query[kSecMatchLimit] = kSecMatchLimitOne
        query[kSecAttrSynchronizable] = kSecAttrSynchronizableAny

        var item: CFTypeRef?
        let status = SecItemCopyMatching(query as CFDictionary, &item)

        if status == errSecItemNotFound {
            return nil
        }
        guard status == errSecSuccess else {
            return nil
        }

        guard let attributes = item as? [String: Any] else {
            return nil
        }

        guard let raw = attributes[kSecAttrAccessible as String] as? String else {
            return .missing
        }

        return KeychainAccessibility(cfString: raw as CFString)
    }

    @discardableResult
    public func updateAccessibility(_ accessibility: KeychainAccessibility) throws -> Bool {
        var query = keychainQuery()
        query[kSecAttrSynchronizable] = kSecAttrSynchronizableAny
        let attributes: [CFString: Any] = [
            kSecAttrAccessible: accessibility.cfString
        ]

        let status = SecItemUpdate(query as CFDictionary, attributes as CFDictionary)

        if status == errSecItemNotFound {
            return false
        }
        guard status == errSecSuccess else {
            throw KeychainServiceError.unexpectedStatus(status)
        }
        return true
    }

    private func keychainQuery() -> [CFString: Any] {
        var query: [CFString: Any] = [
            kSecClass: kSecClassGenericPassword,
            kSecAttrService: service,
            kSecAttrAccount: account
        ]

        if let accessGroup {
            query[kSecAttrAccessGroup] = accessGroup
        }

        return query
    }
}
