import FluxCore
import Foundation

enum KeychainAccessibilityMigrator {
    static let required: KeychainAccessibility = .afterFirstUnlockThisDeviceOnly

    @discardableResult
    static func run(
        keychain: KeychainService = KeychainService(),
        updateOverride: ((KeychainAccessibility) throws -> Bool)? = nil
    ) -> Bool {
        guard let currentClass = keychain.readAccessibility() else {
            return false
        }
        if currentClass == required {
            return false
        }

        let update = updateOverride ?? { try keychain.updateAccessibility($0) }

        do {
            let updated = try update(required)
            if updated { return true }
        } catch {
            // Fall through to read+save fallback for rare update failures.
        }

        guard let token = keychain.loadToken() else { return false }
        do {
            try keychain.saveToken(token)
            return true
        } catch {
            return false
        }
    }
}
