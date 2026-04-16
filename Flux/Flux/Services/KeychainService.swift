import Foundation
import Security

enum KeychainServiceError: Error, Sendable, Equatable {
    case unexpectedStatus(OSStatus)
}

final class KeychainService: Sendable {
    private let service: String
    private let account: String
    private let accessGroup: String?

    init(
        service: String = "me.nore.ig.flux",
        account: String = "api-token",
        accessGroup: String? = "group.me.nore.ig.flux"
    ) {
        self.service = service
        self.account = account
        self.accessGroup = accessGroup
    }

    func saveToken(_ token: String) throws {
        try deleteToken()

        var query = keychainQuery()
        query[kSecValueData] = Data(token.utf8)

        let status = SecItemAdd(query as CFDictionary, nil)
        guard status == errSecSuccess else {
            throw KeychainServiceError.unexpectedStatus(status)
        }
    }

    func loadToken() -> String? {
        var query = keychainQuery()
        query[kSecReturnData] = kCFBooleanTrue
        query[kSecMatchLimit] = kSecMatchLimitOne

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

    func deleteToken() throws {
        let status = SecItemDelete(keychainQuery() as CFDictionary)
        guard status == errSecSuccess || status == errSecItemNotFound else {
            throw KeychainServiceError.unexpectedStatus(status)
        }
    }

    private func keychainQuery() -> [CFString: Any] {
        var query: [CFString: Any] = [
            kSecClass: kSecClassGenericPassword,
            kSecAttrService: service,
            kSecAttrAccount: account,
        ]

        if let accessGroup {
            query[kSecAttrAccessGroup] = accessGroup
        }

        return query
    }
}
